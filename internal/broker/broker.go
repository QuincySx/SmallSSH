// Package broker is the sole SSH authority for both desktop and AI clients.
package broker

import (
	"context"
	"crypto/hmac"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"time"

	"sshclient/internal/accesspolicy"
	"sshclient/internal/approval"
	"sshclient/internal/auditlog"
	"sshclient/internal/fileattrs"
	"sshclient/internal/localipc"
	"sshclient/internal/monitor"
	"sshclient/internal/serverops"
	"sshclient/internal/sshconn"
	"sshclient/internal/vault"
)

// Message is the local wire protocol. Credentials are never response fields.
type Message struct {
	Challenge string `json:"challenge,omitempty"`
	After     string `json:"after,omitempty"`
	Next      string `json:"next,omitempty"`
	Limit     int    `json:"limit,omitempty"`
	// Redacted marks a host list served without owner proof: connection IDs,
	// names and groups only.
	Redacted      bool                          `json:"redacted,omitempty"`
	AuditQuery    *auditlog.Query               `json:"auditQuery,omitempty"`
	AuditPage     *auditlog.Page                `json:"auditPage,omitempty"`
	Renewal       *approval.RenewalChallenge    `json:"renewal,omitempty"`
	ErrorCode     string                        `json:"errorCode,omitempty"`
	ComposePlan   *serverops.ComposePlan        `json:"composePlan,omitempty"`
	ComposeStatus []serverops.ComposeStatus     `json:"composeStatus,omitempty"`
	Phase         string                        `json:"phase,omitempty"`
	FileInfo      *fileattrs.Info               `json:"fileInfo,omitempty"`
	Delegation    *approval.DelegationChallenge `json:"delegation,omitempty"`
	HostIdentity  *sshconn.HostIdentity         `json:"hostIdentity,omitempty"`
	Identities    []sshconn.KeyInfo             `json:"identities,omitempty"`
	ProfileChange *vault.ProfileChange          `json:"profileChange,omitempty"`
	Port          int                           `json:"port,omitempty"`
	MonitorError  string                        `json:"monitorError,omitempty"`
	// MonitorActive is whether the client still shows this session's server
	// status. The broker samples a server only while some subscriber shows it.
	MonitorActive bool                  `json:"monitorActive,omitempty"`
	Transfer      *sshconn.TransferMeta `json:"transfer,omitempty"`
	Stats         *monitor.Stats        `json:"stats,omitempty"`
	Action        string                `json:"action,omitempty"`
	Operation     approval.Operation    `json:"operation,omitempty"`
	TTL           int                   `json:"ttl,omitempty"`
	ID            string                `json:"id,omitempty"`
	Ticket        string                `json:"ticket,omitempty"`
	Allow         bool                  `json:"allow,omitempty"`
	Proof         []byte                `json:"proof,omitempty"`
	Request       *approval.Request     `json:"request,omitempty"`
	Requests      []approval.Request    `json:"requests,omitempty"`
	Hosts         []string              `json:"hosts,omitempty"`
	Files         []sshconn.File        `json:"files,omitempty"`
	Data          []byte                `json:"data,omitempty"`
	Cols          int                   `json:"cols,omitempty"`
	Rows          int                   `json:"rows,omitempty"`
	ExitCode      int                   `json:"exitCode,omitempty"`
	Error         string                `json:"error,omitempty"`
	Profile       *vault.Profile        `json:"profile,omitempty"`
	Connections   []vault.Summary       `json:"connections,omitempty"`
	Details       *vault.Summary        `json:"details,omitempty"`
	Target        string                `json:"target,omitempty"`
}

type Server struct {
	Vault          *vault.Session
	Approvals      *approval.Manager
	listener       net.Listener
	ctx            context.Context
	cancel         context.CancelFunc
	release        func()
	once           sync.Once
	wg             sync.WaitGroup
	slots          chan struct{}
	ownerMu        sync.Mutex
	challenges     map[string]time.Time
	dir            string
	writes         sync.Mutex
	monitorBudgets sync.Map
	monitorMu      sync.Mutex
	monitors       map[monitorTarget]*monitorCollector
	monitorClosed  bool
	monitorWG      sync.WaitGroup
	audit          *audit
	auditFault     atomic.Bool
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
	release, e := localipc.Acquire(dir)
	if e != nil {
		v.Close()
		return nil, e
	}
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
	if e = log.write("unlocked", nil); e != nil {
		log.close()
		l.Close()
		v.Close()
		release()
		return nil, e
	}
	ctx, cancel := context.WithCancel(context.Background())
	s := &Server{Vault: v, Approvals: approval.NewWithClock(v.Proof, now), listener: l, ctx: ctx, cancel: cancel, release: release, slots: make(chan struct{}, 32), challenges: make(map[string]time.Time), dir: dir, audit: log}
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
				s.Approvals.Sweep()
				s.auditExpiries()
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
func (s *Server) Close() {
	s.once.Do(func() {
		s.cancel()
		s.listener.Close()
		s.Approvals.Lock()
		s.stopMonitorCollectors()
		s.wg.Wait()
		s.monitorWG.Wait()
		s.auditExpiries()
		s.auditRequest("locked", nil)
		s.audit.close()
		s.Vault.Close()
		s.release()
	})
}
func (s *Server) handle(c net.Conn) {
	defer c.Close()
	stop := context.AfterFunc(s.ctx, func() { c.Close() })
	defer stop()
	w := &localipc.Wire{Conn: c}
	c.SetDeadline(time.Now().Add(20 * time.Second))
	var q Message
	if w.Receive(&q) != nil {
		return
	}
	fail := func(e error) {
		code := ""
		if errors.Is(e, approval.ErrSessionExpired) {
			code = "session_expired"
		}
		if errors.Is(e, approval.ErrSessionRenewing) {
			code = "session_renewing"
		}
		if policyDenial(e) {
			code = "automation_policy"
		}
		_ = w.Send(Message{Action: "error", Error: e.Error(), ErrorCode: code})
	}
	if s.auditFault.Load() {
		switch q.Action {
		case "request", "ownerRequest", "decide", "delegate", "renewSession", "run", "profileChange", "profilePut", "profileDetails", "profileDelete":
			fail(errAuditUnavailable)
			return
		}
	}
	switch q.Action {
	case "renewalChallenge", "renewSession":
		s.ownerMu.Lock()
		defer s.ownerMu.Unlock()
		result, e := s.renewSession(q)
		if e != nil {
			fail(e)
			return
		}
		_ = w.Send(result)
	case "sessionStatus":
		r, e := s.queryRequest(q)
		if e != nil {
			fail(e)
			return
		}
		_ = w.Send(Message{Request: &r})
	case "delegationChallenge":
		s.ownerMu.Lock()
		defer s.ownerMu.Unlock()
		p, e := s.Vault.Profile(q.Operation.Host)
		if e != nil {
			fail(s.recordAuthorityFailure(q.Operation, q.ID, "invalid_authority", e))
			return
		}
		q.Operation.Endpoint = fmt.Sprintf("%s@%s", p.User, net.JoinHostPort(p.Host, fmt.Sprint(p.Port)))
		q.Operation.HostFingerprint = p.Fingerprint
		if e = s.checkAutomation(p, q.Operation, q.ID); e != nil {
			fail(e)
			return
		}
		if _, e := s.Approvals.GetForTicket(q.ID, q.Ticket); e != nil {
			fail(s.recordAuthorityFailure(q.Operation, q.ID, "invalid_authority", e))
			return
		}
		challenge, e := s.Approvals.DelegationChallenge(q.ID, q.Operation, time.Duration(q.TTL)*time.Second)
		if e != nil {
			if !errors.Is(e, approval.ErrSessionExpired) && !errors.Is(e, approval.ErrSessionRenewing) {
				e = s.recordRefusal(q.Operation, q.ID, "scope_denied", p.Name, e)
			}
			fail(e)
			return
		}
		_ = w.Send(Message{Delegation: &challenge})
	case "delegate":
		s.ownerMu.Lock()
		defer s.ownerMu.Unlock()
		op, parent, known := s.Approvals.DelegationOperation(q.ID)
		r, t, e := s.Approvals.MintDelegated(q.ID, q.Proof)
		if e != nil {
			if !known {
				op = approval.Operation{Kind: "delegate"}
			}
			fail(s.recordAuthorityFailure(op, parent, "invalid_authority", e))
			return
		}
		p, e := s.Vault.Profile(r.Operation.Host)
		if e == nil {
			e = s.checkAutomation(p, r.Operation, r.ParentID)
		}
		if e != nil {
			s.Approvals.InvalidateAutomationHost(r.Operation.Host)
			fail(e)
			return
		}
		if e = s.auditRequest("delegated", &r); e != nil {
			s.Approvals.Lock()
			fail(e)
			return
		}
		_ = w.Send(Message{Request: &r, Ticket: t})
	case "probeHost":
		if !s.ownerQuery(q) {
			fail(s.recordAuthorityFailure(managementAttempt(q), "", "owner_required", approval.ErrDenied))
			return
		}
		ctx, cancel := context.WithTimeout(s.ctx, 8*time.Second)
		defer cancel()
		watch := context.AfterFunc(ctx, func() { c.Close() })
		defer watch()
		// A disconnect cancels the unauthenticated probe too.
		go func() { var ignored Message; _ = w.Receive(&ignored); cancel() }()
		identity, e := sshconn.ProbeHost(ctx, q.Target, q.Port)
		if e != nil {
			fail(e)
			return
		}
		_ = w.Send(Message{HostIdentity: &identity})
	case "agentIdentities":
		if !s.ownerQuery(q) {
			fail(s.recordAuthorityFailure(managementAttempt(q), "", "owner_required", approval.ErrDenied))
			return
		}
		keys, e := sshconn.AgentIdentities(s.ctx)
		if e != nil {
			fail(e)
			return
		}
		_ = w.Send(Message{Identities: keys})
	case "profileChange":
		s.ownerMu.Lock()
		defer s.ownerMu.Unlock()
		expires, ok := s.challenges[q.ID]
		delete(s.challenges, q.ID)
		if !ok || time.Now().After(expires) || q.ProfileChange == nil {
			fail(s.recordAuthorityFailure(managementAttempt(q), "", "owner_required", approval.ErrDenied))
			return
		}
		expected, e := ProfileChangeProof(q.ID, *q.ProfileChange, s.Vault.Proof)
		if e != nil || !hmac.Equal(expected, q.Proof) {
			fail(s.recordAuthorityFailure(managementAttempt(q), "", "owner_required", approval.ErrDenied))
			return
		}
		profile, e := s.Vault.ResolveChange(*q.ProfileChange)
		if e == nil && profile.Auth == "key" {
			_, e = sshconn.InspectPrivateKey(profile.PrivateKey, profile.KeyPassphrase)
		}
		if e != nil {
			fail(e)
			return
		}
		previous, previousErr := s.Vault.Profile(profile.ID)
		if e = s.Vault.SaveProfile(VaultPath(s.dir), profile); e != nil {
			fail(e)
			return
		}
		s.invalidateProfile(previous, previousErr == nil, profile)
		if e = s.auditPolicyChange(previous.Policy, previousErr == nil, profile); e != nil {
			fail(e)
			return
		}
		_ = w.Send(Message{Action: "ok"})
	case "ownerRequest":
		s.ownerMu.Lock()
		defer s.ownerMu.Unlock()
		expires, ok := s.challenges[q.ID]
		delete(s.challenges, q.ID)
		if !ok || time.Now().After(expires) {
			fail(s.recordAuthorityFailure(managementAttempt(q), "", "owner_required", approval.ErrDenied))
			return
		}
		expected, e := OwnerRequestProof(q.ID, q.Operation, s.Vault.Proof)
		if e != nil || !hmac.Equal(expected, q.Proof) {
			fail(s.recordAuthorityFailure(managementAttempt(q), "", "owner_required", approval.ErrDenied))
			return
		}
		profile, e := s.Vault.Profile(q.Operation.Host)
		if e != nil {
			fail(e)
			return
		}
		endpoint := fmt.Sprintf("%s@%s", profile.User, net.JoinHostPort(profile.Host, fmt.Sprint(profile.Port)))
		if (q.Operation.Endpoint != "" && q.Operation.Endpoint != endpoint) || (q.Operation.HostFingerprint != "" && q.Operation.HostFingerprint != profile.Fingerprint) {
			fail(errors.New("连接目标已变化，请重新连接后操作"))
			return
		}
		q.Operation.Endpoint = endpoint
		q.Operation.HostFingerprint = profile.Fingerprint
		r, t, e := s.Approvals.CreateOwner(q.Operation)
		if e != nil {
			fail(e)
			return
		}
		_ = w.Send(Message{Request: &r, Ticket: t})

	case "withdraw":
		if e := s.Approvals.Withdraw(q.ID, q.Ticket); e != nil {
			attempt := managementAttempt(q)
			if r, err := s.Approvals.Get(q.ID); err == nil {
				attempt.Host = r.Operation.Host
			}
			fail(s.recordAuthorityFailure(attempt, q.ID, "invalid_authority", e))
			return
		}
		if r, e := s.Approvals.Get(q.ID); e == nil {
			if e = s.auditRequest("withdrawn", &r); e != nil {
				fail(e)
				return
			}
		}
		_ = w.Send(Message{Action: "ok"})
	case "profileChallenge":
		s.ownerMu.Lock()
		defer s.ownerMu.Unlock()
		for k, v := range s.challenges {
			if time.Now().After(v) {
				delete(s.challenges, k)
			}
		}
		if len(s.challenges) >= 128 {
			fail(errors.New("管理请求过多"))
			return
		}
		id := vault.ShortID()
		s.challenges[id] = time.Now().Add(time.Minute)
		_ = w.Send(Message{ID: id})
	case "auditHistory":
		s.ownerMu.Lock()
		defer s.ownerMu.Unlock()
		expires, ok := s.challenges[q.ID]
		delete(s.challenges, q.ID)
		denied := approval.Operation{Kind: "audit-history", Client: q.Operation.Client}
		if !ok || time.Now().After(expires) || q.AuditQuery == nil {
			fail(s.recordAuthorityFailure(denied, "", "owner_required", approval.ErrDenied))
			return
		}
		expected, e := AuditHistoryProof(q.ID, *q.AuditQuery, s.Vault.Proof)
		if e != nil || !hmac.Equal(expected, q.Proof) {
			fail(s.recordAuthorityFailure(denied, "", "owner_required", approval.ErrDenied))
			return
		}
		page, e := s.audit.history(*q.AuditQuery)
		if e != nil {
			fail(e)
			return
		}
		_ = w.Send(Message{AuditPage: &page})
	case "profilePut":
		s.ownerMu.Lock()
		defer s.ownerMu.Unlock()
		expires, ok := s.challenges[q.ID]
		delete(s.challenges, q.ID)
		if !ok || time.Now().After(expires) || q.Profile == nil {
			fail(s.recordAuthorityFailure(managementAttempt(q), "", "owner_required", approval.ErrDenied))
			return
		}
		proof, e := ProfileProof(q.ID, *q.Profile, s.Vault.Proof)
		if e != nil || !hmac.Equal(proof, q.Proof) {
			fail(s.recordAuthorityFailure(managementAttempt(q), "", "owner_required", approval.ErrDenied))
			return
		}
		previous, previousErr := s.Vault.Profile(q.Profile.ID)
		if e = s.Vault.SaveProfile(VaultPath(s.dir), *q.Profile); e != nil {
			fail(e)
			return
		}
		s.invalidateProfile(previous, previousErr == nil, *q.Profile)
		if e = s.auditPolicyChange(previous.Policy, previousErr == nil, *q.Profile); e != nil {
			fail(e)
			return
		}
		_ = w.Send(Message{Action: "ok"})
	case "profileDetails":
		s.ownerMu.Lock()
		defer s.ownerMu.Unlock()
		expires, ok := s.challenges[q.ID]
		delete(s.challenges, q.ID)
		if !ok || time.Now().After(expires) || q.Details == nil {
			fail(s.recordAuthorityFailure(managementAttempt(q), "", "owner_required", approval.ErrDenied))
			return
		}
		b, _ := json.Marshal(q.Details)
		expected, e := s.Vault.Proof(append([]byte("ssh-client/profile-details/v1\x00"+q.ID+"\x00"), b...))
		if e != nil || !hmac.Equal(expected, q.Proof) {
			fail(s.recordAuthorityFailure(managementAttempt(q), "", "owner_required", approval.ErrDenied))
			return
		}
		previous, previousErr := s.Vault.Profile(q.Details.ID)
		if e = s.Vault.SaveDetails(VaultPath(s.dir), *q.Details); e != nil {
			fail(e)
			return
		}
		updated, e := s.Vault.Profile(q.Details.ID)
		if e != nil {
			fail(e)
			return
		}
		s.invalidateProfile(previous, previousErr == nil, updated)
		if e = s.auditPolicyChange(previous.Policy, previousErr == nil, updated); e != nil {
			fail(e)
			return
		}
		_ = w.Send(Message{Action: "ok"})
	case "profileDelete":
		s.ownerMu.Lock()
		defer s.ownerMu.Unlock()
		expires, ok := s.challenges[q.ID]
		delete(s.challenges, q.ID)
		if !ok || time.Now().After(expires) || q.Target == "" || len(q.Target) > 128 {
			fail(s.recordAuthorityFailure(managementAttempt(q), "", "owner_required", approval.ErrDenied))
			return
		}
		expected, e := s.Vault.Proof([]byte("ssh-client/profile-delete/v1\x00" + q.ID + "\x00" + q.Target))
		if e != nil || !hmac.Equal(expected, q.Proof) {
			fail(s.recordAuthorityFailure(managementAttempt(q), "", "owner_required", approval.ErrDenied))
			return
		}
		if e = s.Vault.DeleteProfile(VaultPath(s.dir), q.Target); e != nil {
			fail(e)
			return
		}
		s.Approvals.InvalidateHost(q.Target)
		_ = w.Send(Message{Action: "ok"})
	case "hosts":
		// Without owner proof a caller gets what it needs to ask for access and
		// nothing about where the machine is or how to log in to it.
		owner := s.ownerQuery(q)
		list := s.Vault.Summaries()
		if !owner {
			list = redactedSummaries(list)
			_ = w.Send(Message{Connections: list, Redacted: true})
			return
		}
		_ = w.Send(Message{Hosts: s.Vault.Names(), Connections: list})
	case "pending":
		page, e := s.pendingPage(q)
		if e != nil {
			fail(s.recordAuthorityFailure(managementAttempt(q), "", "owner_required", e))
			return
		}
		_ = w.Send(page)
	case "request":
		s.ownerMu.Lock()
		defer s.ownerMu.Unlock()
		p, e := s.Vault.Profile(q.Operation.Host)
		if e != nil {
			fail(s.recordAuthorityFailure(q.Operation, "", "invalid_authority", e))
			return
		}
		q.Operation.Endpoint = fmt.Sprintf("%s@%s", p.User, net.JoinHostPort(p.Host, fmt.Sprint(p.Port)))
		q.Operation.HostFingerprint = p.Fingerprint
		q.Operation.PathProtection = q.Operation.Kind == "grant" && !p.Policy.Unrestricted
		if e = s.checkAutomation(p, q.Operation, ""); e != nil {
			fail(e)
			return
		}
		r, t, e := s.Approvals.Create(q.Operation, time.Duration(q.TTL)*time.Second)
		if e != nil {
			fail(s.recordRefusal(q.Operation, "", "invalid_operation", p.Name, e))
			return
		}
		if e = s.auditRequest("requested", &r); e != nil {
			s.Approvals.Lock()
			fail(e)
			return
		}
		_ = w.Send(Message{Request: &r, Ticket: t})
	case "status":
		r, e := s.queryRequest(q)
		if e != nil {
			fail(e)
			return
		}
		_ = w.Send(Message{Request: &r})
	case "decide":
		s.ownerMu.Lock()
		defer s.ownerMu.Unlock()
		if e := s.Approvals.Decide(q.ID, q.Allow, q.Proof); e != nil {
			attempt := managementAttempt(q)
			if r, err := s.Approvals.Get(q.ID); err == nil {
				attempt.Host = r.Operation.Host
			}
			fail(s.recordAuthorityFailure(attempt, q.ID, "invalid_authority", e))
			return
		}
		r, e := s.Approvals.Get(q.ID)
		if e != nil {
			fail(e)
			return
		}
		if e = s.auditRequest(r.Status, &r); e != nil {
			s.Approvals.Lock()
			fail(e)
			return
		}
		_ = w.Send(Message{Action: "ok"})
	case "revoke":
		if e := s.Approvals.Revoke(q.ID, q.Proof); e != nil {
			attempt := managementAttempt(q)
			if r, err := s.Approvals.Get(q.ID); err == nil {
				attempt.Host = r.Operation.Host
			}
			fail(s.recordAuthorityFailure(attempt, q.ID, "invalid_authority", e))
			return
		}
		r, e := s.Approvals.Get(q.ID)
		if e != nil {
			r, e = s.Approvals.SessionStatus(q.ID)
		}
		if e == nil {
			if e = s.auditRequest("revoked", &r); e != nil {
				fail(e)
				return
			}
		}
		_ = w.Send(Message{Action: "ok"})
	case "run":
		c.SetDeadline(time.Time{})
		s.run(w, q)
	default:
		fail(errors.New("不支持的本地操作"))
	}
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

func (s *Server) run(w *localipc.Wire, q Message) {
	fail := func(e error) { _ = w.Send(Message{Action: "error", Error: e.Error()}) }
	s.ownerMu.Lock()
	if s.auditFault.Load() {
		s.ownerMu.Unlock()
		fail(errAuditUnavailable)
		return
	}
	ctx, op, cancel, e := s.Approvals.Start(s.ctx, q.ID, q.Ticket)
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
	fail = func(err error) { _ = finish(Message{Action: "error", Error: err.Error()}) }
	defer func() {
		if !ended {
			outcome := "unknown"
			if ctx.Err() != nil {
				outcome = "cancelled"
			}
			_ = s.auditCompletion(&r, outcome, nil)
		}
	}()
	stop := context.AfterFunc(ctx, func() { w.Conn.Close() })
	defer stop()
	s.ownerMu.Unlock()
	c, e := sshconn.Dial(ctx, p)
	if e != nil {
		fail(e)
		return
	}
	defer c.Close()
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
		if paths := accesspolicy.MutationPaths(op); len(paths) > 0 {
			// Hold the same lock from remote path inspection through commit;
			// another operation in this broker cannot swap a checked ancestor.
			lockWrites()
			if e = c.CheckProtectedPaths(ctx, paths, p.Policy.Paths()); e != nil {
				fail(s.recordRefusal(op, r.ParentID, "protected_path", p.Name, e))
				return
			}
		}
		if paths := accesspolicy.ReadPaths(op); len(paths) > 0 {
			lockWrites()
			if e = c.CheckProtectedReadPaths(ctx, paths, p.Policy.Paths()); e != nil {
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
	if e = w.Send(Message{Action: "ready", Transfer: meta}); e != nil {
		return
	}
	// Non-streaming input operations cancel immediately if the client disappears.
	if op.Kind != "shell" && op.Kind != "docker-terminal" && op.Kind != "put" && op.Kind != "replace" && op.Kind != "put-resume" {
		go func() { var ignored Message; _ = w.Receive(&ignored); cancel() }()
	}
	switch op.Kind {
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
				if e := w.Receive(&m); e != nil {
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
				if w.Receive(&m) != nil {
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
