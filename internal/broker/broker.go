// Package broker is the sole SSH authority for both desktop and AI clients.
package broker

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"sync"
	"sync/atomic"
	"time"

	"sshclient/internal/accesspolicy"
	"sshclient/internal/approval"
	"sshclient/internal/connectcfg"
	"sshclient/internal/fileattrs"
	"sshclient/internal/localipc"
	"sshclient/internal/ownercap"
	"sshclient/internal/protocol"
	"sshclient/internal/sshconn"
	"sshclient/internal/vault"
)

// Message preserves the existing broker-facing name for the shared contract.
type Message = protocol.Message

type Server struct {
	Vault           *vault.Session
	Approvals       *approval.Manager
	listener        net.Listener
	ctx             context.Context
	cancel          context.CancelFunc
	release         func()
	once            sync.Once
	wg              sync.WaitGroup
	errMu           sync.Mutex
	err             error
	slots           chan struct{}
	ownerMu         sync.Mutex
	challenges      map[string]time.Time
	ownerRegistry   *ownercap.Registry
	ownerLeases     map[string]*OwnerLease
	dir             string
	writes          sync.Mutex
	monitorBudgets  sync.Map
	monitorMu       sync.Mutex
	monitors        map[monitorTarget]*monitorCollector
	monitorClosed   bool
	monitorWG       sync.WaitGroup
	audit           *audit
	auditFault      atomic.Bool
	roots           protectedRootCache
	conns           connPool
	sealOnce        sync.Once
	sealErr         error
	channels        map[string]*callerChannel
	requestChannels map[string]*callerChannel
	requestOwners   map[string]string
	tlsConfig       *tls.Config
	publicKeyPin    string
}

// Open takes exclusive ownership of the vault until Close. The caller reads the
// password from its human-facing UI/TTY, never an AI RPC or command argument.
func Open(dir string, pass []byte) (*Server, error) {
	v, e := vault.Read(filepath.Join(dir, "vault.json"), pass)
	if e != nil {
		return nil, e
	}
	return OpenSession(dir, v)
}

// OpenSession takes ownership, including on failure. Only local owner frontends
// obtain this decrypted session through a master password or system verification.
func OpenSession(dir string, v *vault.Session) (*Server, error) {
	return openSession(dir, v, time.Now)
}

func openSession(dir string, v *vault.Session, now func() time.Time) (*Server, error) {
	return openSessionWithOwnerRegistry(dir, v, now, "", false)
}

// OpenManagedSession is the authenticated host entry point. It has the same
// vault ownership and failure cleanup as OpenSession and additionally creates
// the in-memory owner-session registry for this Server. The epoch comes from
// the trusted local host; the legacy constructor never enables management.
func OpenManagedSession(dir string, owner *vault.Session, epoch string) (*Server, error) {
	return openSessionWithOwnerRegistry(dir, owner, time.Now, epoch, true)
}

func openSessionWithOwnerRegistry(dir string, v *vault.Session, now func() time.Time, epoch string, managed bool) (*Server, error) {
	if v == nil {
		return nil, errors.New("保险库会话为空")
	}
	release, e := localipc.Acquire(dir)
	if e != nil {
		v.Close()
		return nil, e
	}
	return openSessionWithRelease(dir, v, now, epoch, managed, release)
}

func openSessionWithRelease(dir string, v *vault.Session, now func() time.Time, epoch string, managed bool, release func()) (*Server, error) {
	l, e := localipc.Listen(dir)
	if e != nil {
		v.Close()
		release()
		return nil, e
	}
	log, e := openAudit(dir)
	if e != nil {
		l.Close()
		v.Close()
		release()
		return nil, e
	}
	// A log rewritten as a whole is internally consistent. The head the vault
	// stored is what makes that visible: the retained history must still contain
	// that record. Everything after it is a tail nobody committed — a crash, or a
	// lock that could not seal the vault — so it is not a rewrite. Losing the
	// anchored record is, and that stops the app here.
	if want := v.AuditHead(); want != "" {
		if got := log.head(); got != want {
			found, err := log.contains(want)
			if err != nil || !found {
				log.close()
				l.Close()
				v.Close()
				release()
				if err != nil {
					return nil, err
				}
				return nil, errors.New("审计历史的锚点已不存在：记录可能被整体重写或截断，请从备份恢复审计历史后再解锁")
			}
		}
	}
	if e = log.write("unlocked", nil); e != nil {
		log.close()
		l.Close()
		v.Close()
		release()
		return nil, e
	}
	ctx, cancel := context.WithCancel(context.Background())
	identity, err := v.BrokerIdentity()
	var tlsConfig *tls.Config
	var publicKeyPin string
	if err == nil {
		tlsConfig, publicKeyPin, err = localipc.ServerTLS(identity)
		clear(identity)
	}
	if err != nil {
		cancel()
		l.Close()
		log.close()
		v.Close()
		release()
		return nil, err
	}
	s := &Server{Vault: v, Approvals: approval.NewWithClock(v.Proof, now), listener: l, ctx: ctx, cancel: cancel, release: release, slots: make(chan struct{}, 64), challenges: make(map[string]time.Time), dir: dir, audit: log, channels: make(map[string]*callerChannel), requestChannels: make(map[string]*callerChannel), requestOwners: make(map[string]string), tlsConfig: tlsConfig, publicKeyPin: publicKeyPin}
	if managed {
		registry, err := ownercap.NewRegistry(epoch, func(id string) { s.ownerSessionRevoked(id) })
		if err != nil {
			cancel()
			l.Close()
			log.close()
			localipc.DestroyServerTLS(tlsConfig)
			v.Close()
			release()
			return nil, err
		}
		s.ownerRegistry = registry
		s.ownerLeases = make(map[string]*OwnerLease)
	}
	// Only an authenticated owner session can publish initial trust. Existing
	// different anchors remain untouched and require explicit owner repair.
	_ = localipc.PairBroker(dir, publicKeyPin, false)
	s.wg.Add(2)
	go s.serve()
	go func() {
		defer s.wg.Done()
		t := time.NewTicker(time.Second)
		defer t.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-t.C:
				s.checkChannels()
				if s.ownerRegistry != nil {
					s.ownerRegistry.Sweep()
				}
				s.Approvals.Sweep()
				s.auditExpiries()
				// A pooled connection whose profile is never used again has no
				// other chance to expire.
				s.conns.sweep()
			}
		}
	}()
	return s, nil
}

func (s *Server) serve() {
	defer s.wg.Done()
	for {
		c, e := s.listener.Accept()
		if e != nil {
			if s.ctx.Err() == nil {
				s.recordFault(fmt.Errorf("代理核心接受连接: %w", e))
				go s.Close()
			}
			return
		}
		select {
		case s.slots <- struct{}{}:
			s.wg.Add(1)
			go func() { defer s.wg.Done(); defer func() { <-s.slots }(); s.handle(c) }()
		default:
			c.Close()
		}
	}
}

var closedServerDone = func() <-chan struct{} {
	done := make(chan struct{})
	close(done)
	return done
}()

// Done reports when this authority has stopped accepting new work. Close is
// still the barrier for complete resource cleanup.
func (s *Server) Done() <-chan struct{} {
	if s == nil || s.ctx == nil {
		return closedServerDone
	}
	return s.ctx.Done()
}

// Err returns the first fatal serving error, if any. A normal Close is not a
// fault.
func (s *Server) Err() error {
	if s == nil {
		return nil
	}
	s.errMu.Lock()
	defer s.errMu.Unlock()
	return s.err
}

func (s *Server) recordFault(err error) {
	if s == nil || err == nil {
		return
	}
	s.errMu.Lock()
	if s.err == nil {
		s.err = err
	}
	s.errMu.Unlock()
}

func (s *Server) Close() {
	s.once.Do(func() {
		s.roots.clear()
		s.cancel()
		s.listener.Close()
		if s.ownerRegistry != nil {
			s.ownerRegistry.Close()
		}
		s.Approvals.Lock()
		s.stopMonitorCollectors()
		s.wg.Wait()
		localipc.DestroyServerTLS(s.tlsConfig)
		s.monitorWG.Wait()
		_ = s.SealAudit()
		s.conns.clear()
		s.audit.close()
		s.Vault.Close()
		s.release()
	})
}

// SealAudit writes the closing record and stores its head in the vault. The
// desktop calls it while the vault can still be sealed — the signing key is
// dropped right after, and a sealed vault cannot be written again, which would
// leave the anchor behind the history it was meant to commit. Close calls it
// too, and the two cannot both run, so a lock followed by a quit writes one
// closing record and one anchor.
func (s *Server) SealAudit() error {
	s.sealOnce.Do(func() {
		if s.audit == nil || s.Vault == nil {
			return
		}
		s.auditExpiries()
		_ = s.auditRequest("locked", nil)
		head := s.audit.head()
		if head == "" {
			return
		}
		s.Vault.SetAuditHead(head)
		s.sealErr = vault.Save(VaultPath(s.dir), s.Vault, false)
	})
	return s.sealErr
}
func (s *Server) handleMessage(w *localipc.Wire, q Message, channel *callerChannel) error {
	if q.Action == "run" {
		w.Conn.SetReadDeadline(time.Time{})
		s.run(w, q, channel)
		return nil
	}
	var response Message
	var err error
	if q.Action == "probeHost" || q.Action == "measureRoutes" {
		response, err = s.probeHost(w, q, channel)
	} else {
		response, err = s.controlMessage(q, channel)
	}
	if err != nil {
		response = errorResponse(err)
	}
	return w.Send(response)
}

// The unauthenticated SSH probe needs the wire only to observe disconnects.
// Authorization state transitions use the transport-free controlMessage path.
func (s *Server) probeHost(w *localipc.Wire, q Message, channel *callerChannel) (result Message, resultErr error) {
	if !s.ownerQuery(q, channel) {
		return Message{}, s.recordAuthorityFailure(managementAttempt(q), "", "owner_required", approval.ErrDenied)
	}
	ctx, cancel := context.WithTimeout(channel.ctx, 8*time.Second)
	defer cancel()
	watch := context.AfterFunc(ctx, func() { _ = w.Close() })
	defer watch()
	// A disconnect cancels the unauthenticated probe too.
	go func() { var ignored Message; _ = w.Receive(&ignored); cancel() }()
	network := connectcfg.Network{}
	if q.Network != nil {
		network = *q.Network
	}
	if q.ID != "" {
		if saved, err := s.Vault.Profile(q.ID); err == nil {
			if q.Network == nil {
				network = saved.Network
			}
			if q.KeepProxyCredential && network.Proxy.Public() == saved.Network.Proxy.Public() {
				network.Proxy.Password = saved.Network.Proxy.Password
			}
		}
	}
	route, e := s.Vault.ResolveRoute(vault.Profile{ID: q.ID, Network: network})
	if e != nil {
		return Message{}, e
	}
	if len(route) > 1 {
		// The relay authenticates even though the destination probe does not.
		request := approval.Request{ID: vault.ShortID(), Nonce: vault.ShortID(), OwnerIssued: true, Status: "running", Expires: time.Now().Add(8 * time.Second), Operation: approval.Operation{Kind: "route-probe", Host: q.ID, Client: "所有者连接探测", Endpoint: net.JoinHostPort(q.Target, strconv.Itoa(q.Port)), HostFingerprint: q.ExpectedFingerprint, Destination: network.Relay}}
		if e := s.auditRequest("started", &request); e != nil {
			return Message{}, e
		}
		defer func() {
			outcome := "success"
			if resultErr != nil {
				outcome = "failed"
			}
			if ctx.Err() != nil {
				outcome = "cancelled"
			}
			if len(result.RouteMeasurements) > 0 {
				available := false
				for _, measurement := range result.RouteMeasurements {
					available = available || measurement.Available
				}
				if !available {
					outcome = "failed"
				}
			}
			if e := s.auditCompletion(&request, outcome, nil); e != nil {
				result, resultErr = Message{}, e
			}
		}()
	}
	if q.Action == "measureRoutes" {
		if q.ExpectedFingerprint == "" {
			return Message{}, errors.New("请先保存并核对目标主机指纹")
		}
		route[0].Host, route[0].Port, route[0].Fingerprint = q.Target, q.Port, q.ExpectedFingerprint
		measured, e := sshconn.MeasureRoutes(ctx, route)
		return Message{RouteMeasurements: measured}, e
	}
	identity, e := sshconn.ProbeRoute(ctx, q.Target, q.Port, network, route[1:])
	if e != nil {
		return Message{}, e
	}
	return Message{HostIdentity: &identity}, nil
}

type dataWriter struct {
	w      *localipc.Wire
	action string
}

func (d dataWriter) Write(p []byte) (int, error) {
	n := 0
	for len(p) > 0 {
		size := min(len(p), 48<<10)
		if e := d.w.Send(Message{Action: d.action, Data: p[:size]}); e != nil {
			return n, e
		}
		n += size
		p = p[size:]
	}
	return n, nil
}

func (s *Server) run(w *localipc.Wire, q Message, channel *callerChannel) {
	fail := func(e error) { _ = w.Send(Message{Action: "error", Error: e.Error()}) }
	s.ownerMu.Lock()
	if s.auditFault.Load() {
		s.ownerMu.Unlock()
		fail(errAuditUnavailable)
		return
	}
	if !s.ownsRequest(channel, q.ID) {
		s.ownerMu.Unlock()
		fail(s.recordAuthorityFailure(approval.Operation{Kind: "run"}, q.ID, "invalid_authority", approval.ErrDenied))
		return
	}
	ctx, op, cancel, e := s.Approvals.Start(channel.ctx, q.ID, q.Ticket)
	if e != nil {
		s.ownerMu.Unlock()
		attempt := approval.Operation{Kind: "run", Client: q.Operation.Client}
		parent := ""
		if known, err := s.Approvals.Get(q.ID); err == nil {
			attempt.Host = known.Operation.Host
			parent = known.ParentID
		}
		fail(s.recordAuthorityFailure(attempt, parent, "invalid_authority", e))
		return
	}
	defer cancel()
	defer s.Approvals.Finish(q.ID)
	r, e := s.Approvals.Get(q.ID)
	if e != nil {
		s.ownerMu.Unlock()
		fail(e)
		return
	}
	p, e := s.Vault.Profile(op.Host)
	var route []vault.Profile
	if e == nil {
		route, e = s.Vault.ResolveRoute(p)
	}
	var scope approval.GrantScope
	if e == nil && r.ParentID != "" {
		parent, err := s.Approvals.Get(r.ParentID)
		if err != nil {
			e = err
		} else {
			scope = parent.Operation.Grant
		}
	}
	if e == nil && !r.OwnerIssued {
		e = s.checkAutomation(p, op, r.ParentID)
	}
	if e != nil {
		s.ownerMu.Unlock()
		fail(e)
		return
	}
	if e = s.auditRequest("started", &r); e != nil {
		s.Approvals.Lock()
		s.ownerMu.Unlock()
		fail(e)
		return
	}
	ended := false
	finish := func(result Message) error {
		if ended {
			return errors.New("操作已结束")
		}
		ended = true
		outcome := "success"
		if result.Action == "error" || result.ExitCode != 0 {
			outcome = "failed"
		}
		if ctx.Err() != nil {
			outcome = "cancelled"
		}
		var code *int
		if result.Action == "done" {
			code = &result.ExitCode
		}
		if err := s.auditCompletion(&r, outcome, code); err != nil {
			return err
		}
		return w.Send(result)
	}
	var opFailed bool
	fail = func(err error) {
		opFailed = true
		_ = finish(Message{Action: "error", Error: err.Error()})
	}
	defer func() {
		if !ended {
			outcome := "unknown"
			if ctx.Err() != nil {
				outcome = "cancelled"
			}
			_ = s.auditCompletion(&r, outcome, nil)
		}
	}()
	stop := context.AfterFunc(ctx, func() { w.Close() })
	defer stop()
	// Read from the instant execution starts, including while SSH is still
	// dialing/handshaking. EOF must cancel that work before a ready frame exists.
	input := make(chan Message, 1)
	var inputReady atomic.Bool
	streamInput := op.Kind == "shell" || op.Kind == "docker-terminal" || op.Kind == "put" || op.Kind == "replace" || op.Kind == "put-resume"
	go func() {
		defer close(input)
		for {
			var message Message
			if err := w.Receive(&message); err != nil {
				cancel()
				return
			}
			if !streamInput || !inputReady.Load() {
				cancel()
				return
			}
			select {
			case input <- message:
			case <-ctx.Done():
				return
			}
		}
	}()
	receive := func(message *Message) error {
		if err := ctx.Err(); err != nil {
			return err
		}
		select {
		case value, ok := <-input:
			if !ok {
				return io.EOF
			}
			*message = value
			return nil
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	// A short operation reuses the connection the previous one left behind. A
	// terminal or a stream keeps its own, bound to its own context: revoking it
	// must still end the transport it opened.
	key := pooledKey(p)
	// Snapshot configuration while ownerMu protects the profile. Both transport
	// reuse and canonical path caches retain this exact configuration identity.
	lease := s.conns.lease(key)
	recyclable := pooledKinds[op.Kind]
	var c *sshconn.Client
	if recyclable {
		c = s.conns.take(ctx, lease)
	}
	s.ownerMu.Unlock()
	if c == nil {
		if c, e = sshconn.DialRoute(ctx, route); e != nil {
			fail(e)
			return
		}
	}
	defer func() {
		// A failed operation may leave its channel wedged, so only a clean one
		// hands the connection on.
		if recyclable && !opFailed && ctx.Err() == nil {
			s.conns.put(lease, c)
			return
		}
		c.Close()
	}()
	writeLocked := false
	lockWrites := func() {
		if !writeLocked {
			s.writes.Lock()
			writeLocked = true
		}
	}
	defer func() {
		if writeLocked {
			s.writes.Unlock()
		}
	}()
	if !r.OwnerIssued && !p.Policy.Unrestricted {
		roots := p.Policy.Paths()
		// Resolving the protected roots is the expensive part and they are the
		// owner's configuration, so the canonical form is cached briefly. The
		// path being changed is still resolved on every operation.
		var canonical []string
		if len(accesspolicy.MutationPaths(op)) > 0 || len(accesspolicy.ReadPaths(op)) > 0 {
			key := rootCacheKey(lease, roots)
			cached, ok := s.roots.get(key)
			if !ok {
				if cached, e = c.ResolveProtectedRoots(ctx, roots); e != nil {
					fail(s.recordRefusal(op, r.ParentID, "protected_path", p.Name, e))
					return
				}
				s.roots.put(key, cached)
			}
			canonical = cached
		}
		if paths := accesspolicy.MutationPaths(op); len(paths) > 0 {
			// Hold the same lock from remote path inspection through commit;
			// another operation in this broker cannot swap a checked ancestor.
			lockWrites()
			if e = c.CheckProtectedPathsCanonical(ctx, paths, roots, canonical); e != nil {
				fail(s.recordRefusal(op, r.ParentID, "protected_path", p.Name, e))
				return
			}
		}
		if paths := accesspolicy.ReadPaths(op); len(paths) > 0 {
			lockWrites()
			if e = c.CheckProtectedReadPathsCanonical(ctx, paths, roots, canonical); e != nil {
				fail(s.recordRefusal(op, r.ParentID, "protected_path", p.Name, e))
				return
			}
		}
	}
	if scope.ReadRoot != "" || scope.WriteRoot != "" {
		lockWrites()
		if e = c.CheckAllowedPaths(ctx, accesspolicy.ReadPaths(op), scope.ReadRoot); e == nil {
			e = c.CheckAllowedPaths(ctx, accesspolicy.MutationPaths(op), scope.WriteRoot)
		}
		if e != nil {
			fail(s.recordRefusal(op, r.ParentID, "scope_denied", p.Name, e))
			return
		}
	}
	var up *sshconn.UploadSession
	var down *sshconn.DownloadSession
	var meta *sshconn.TransferMeta
	var forward *sshconn.Forward
	if op.Kind == "tunnel" {
		forward, e = c.Forward(ctx, op.Tunnel)
		if e != nil {
			fail(e)
			return
		}
		defer forward.Close()
	}
	var shell *sshconn.Shell
	if op.Kind == "shell" || op.Kind == "docker-terminal" {
		command := ""
		if op.Kind == "docker-terminal" {
			command, e = op.Docker.Command(op.Kind)
			if e != nil {
				fail(e)
				return
			}
		}
		shell, e = c.ShellCommand(ctx, q.Cols, q.Rows, command)
		if e != nil {
			fail(e)
			return
		}
		defer shell.Close()
	}
	if op.Kind == "put-resume" || op.Kind == "put" && op.TransferID != "" {
		lockWrites()
		if op.Kind == "put" {
			up, e = c.PrepareNewUpload(ctx, op.Path, op.TransferID, op.ContentHash, op.Size)
		} else {
			up, e = c.PrepareUpload(ctx, op.Path, op.TransferID, op.ContentHash, op.Size)
		}
		if e != nil {
			fail(e)
			return
		}
		defer up.Close()
		meta = &up.Meta
	}
	if op.Kind == "get-resume" {
		down, e = c.PrepareDownload(ctx, op.Path, op.Offset, op.PrefixHash, op.ContentHash)
		if e != nil {
			fail(e)
			return
		}
		defer down.Close()
		meta = &down.Meta
	}
	inputReady.Store(true)
	if e = w.Send(Message{Action: "ready", Transfer: meta, ListenAddress: func() string {
		if forward != nil {
			return forward.Address()
		}
		return ""
	}()}); e != nil {
		return
	}
	switch op.Kind {
	case "tunnel":
		if e := forward.Wait(); e != nil && ctx.Err() == nil {
			fail(e)
			return
		}
		_ = finish(Message{Action: "done"})
	case "docker-logs", "compose-plan", "compose-update":
		if err := s.runDockerWork(ctx, c, w, op, finish); err != nil {
			fail(err)
		}
	case "file-info", "file-attributes":
		var info fileattrs.Info
		var err error
		if op.Kind == "file-info" {
			if r.OwnerIssued {
				info, err = c.FileInfo(ctx, op.Path)
			} else {
				info, err = c.FileInfoBasic(ctx, op.Path)
			}
		} else {
			lockWrites()
			info, err = c.SetFileAttributes(ctx, op.Path, op.Attributes)
		}
		if info.Valid {
			if e := w.Send(Message{Action: "file-info", FileInfo: &info}); e != nil {
				return
			}
		}
		if err != nil {
			fail(err)
			return
		}
		_ = finish(Message{Action: "done"})
	case "mkdir", "rename", "remove", "chmod":
		lockWrites()
		e := c.Mutate(ctx, op.Kind, op.Path, op.Destination, op.Mode)
		if e != nil {
			fail(e)
			return
		}
		_ = finish(Message{Action: "done"})
	case "monitor":
		if e := s.streamSharedMonitor(ctx, c, p.ID, p.Fingerprint, w); e != nil && ctx.Err() == nil {
			fail(e)
		}

	case "exec":
		r, e := c.Exec(ctx, op.Command)
		if e != nil {
			fail(e)
			return
		}
		if _, e = (dataWriter{w, "stdout"}).Write([]byte(r.Stdout)); e != nil {
			return
		}
		if _, e = (dataWriter{w, "stderr"}).Write([]byte(r.Stderr)); e != nil {
			return
		}
		_ = finish(Message{Action: "done", ExitCode: r.ExitCode})
	case "list":
		files, e := c.List(ctx, op.Path)
		if e != nil {
			fail(e)
			return
		}
		// Bound response frames even for large directories.
		for len(files) > 0 {
			n := min(len(files), 128)
			if e = w.Send(Message{Action: "files", Files: files[:n]}); e != nil {
				return
			}
			files = files[n:]
		}
		_ = finish(Message{Action: "done"})
	case "get-resume":
		if e = down.Copy(dataWriter{w, "data"}); e != nil {
			fail(e)
			return
		}
		_ = finish(Message{Action: "done"})
	case "get":
		if e = c.Download(ctx, op.Path, dataWriter{w, "data"}); e != nil {
			fail(e)
			return
		}
		_ = finish(Message{Action: "done"})
	case "put", "replace", "put-resume":
		lockWrites()
		reader, writer := io.Pipe()
		defer reader.Close()
		defer writer.Close()
		go func() {
			for {
				var m Message
				if e := receive(&m); e != nil {
					writer.CloseWithError(e)
					cancel()
					return
				}
				switch m.Action {
				case "data":
					if _, e := writer.Write(m.Data); e != nil {
						return
					}
				case "end":
					writer.Close()
					return
				default:
					writer.CloseWithError(errors.New("上传消息格式错误"))
					return
				}
			}
		}()
		if up != nil {
			e = up.Finish(reader)
		} else if op.Kind == "replace" {
			var backup string
			backup, e = c.Replace(ctx, op.Path, reader, op.ContentHash, op.OriginalHash)
			if e == nil {
				_, e = (dataWriter{w, "stdout"}).Write([]byte(backup))
			}
		} else {
			e = c.Upload(ctx, op.Path, reader, op.ContentHash)
		}
		if e != nil {
			fail(e)
			return
		}
		_ = finish(Message{Action: "done"})
	case "shell", "docker-terminal":
		sh := shell
		finishStatus := func() {}
		setStatusActive := func(bool) {}
		if op.ServerStatus {
			statusCtx, stopStatus := context.WithCancel(ctx)
			statusDone := make(chan struct{})
			var statusMu sync.Mutex
			var statusCollector *monitorCollector
			var statusSub *monitorSubscription
			ready := make(chan struct{})
			var markReady sync.Once
			go func() {
				defer close(statusDone)
				_ = s.streamSharedMonitorWith(statusCtx, c, p.ID, p.Fingerprint, w, func(coll *monitorCollector, sub *monitorSubscription) {
					statusMu.Lock()
					statusCollector, statusSub = coll, sub
					statusMu.Unlock()
					markReady.Do(func() { close(ready) })
				})
				// A subscription that failed before registering must not leave the
				// reader below waiting on it.
				markReady.Do(func() { close(ready) })
			}()
			finishStatus = sync.OnceFunc(func() {
				stopStatus()
				// Closing the transport also releases any blocked SSH channel opens.
				c.Close()
				<-statusDone
			})
			defer finishStatus()
			// "monitor": tell the broker whether this session's status is still on
			// screen. Without that the collector would keep probing a server nobody
			// is looking at.
			setStatusActive = func(active bool) {
				<-ready
				statusMu.Lock()
				coll, sub := statusCollector, statusSub
				statusMu.Unlock()
				s.activateMonitor(coll, sub, active)
			}
		}
		go func() {
			defer cancel()
			for {
				var m Message
				if receive(&m) != nil {
					return
				}
				switch m.Action {
				case "data":
					if _, e := sh.Write(m.Data); e != nil {
						return
					}
				case "resize":
					if sh.Resize(m.Cols, m.Rows) != nil {
						return
					}
				case "monitor":
					setStatusActive(m.MonitorActive)
				default:
					return
				}
			}
		}()
		_, e = io.Copy(dataWriter{w, "data"}, sh)
		if e != nil && ctx.Err() == nil {
			fail(e)
			return
		}
		finishStatus()
		_ = finish(Message{Action: "done"})
	}
}

func VaultPath(dir string) string { return filepath.Join(dir, "vault.json") }
func Exists(dir string) bool      { _, e := os.Stat(VaultPath(dir)); return e == nil }

func ProfileProof(challenge string, p vault.Profile, prove func([]byte) ([]byte, error)) ([]byte, error) {
	b, e := json.Marshal(p)
	if e != nil {
		return nil, e
	}
	return prove(append([]byte("ssh-client/profile-put/v1\x00"+challenge+"\x00"), b...))
}

func ProfileChangeProof(challenge string, p vault.ProfileChange, prove func([]byte) ([]byte, error)) ([]byte, error) {
	b, e := json.Marshal(p)
	if e != nil {
		return nil, e
	}
	return prove(append([]byte("ssh-client/profile-change/v1\x00"+challenge+"\x00"), b...))
}

func OwnerRequestProof(challenge string, op approval.Operation, prove func([]byte) ([]byte, error)) ([]byte, error) {
	b, e := json.Marshal(op)
	if e != nil {
		return nil, e
	}
	return prove(append([]byte("ssh-client/owner-request/v1\x00"+challenge+"\x00"), b...))
}
