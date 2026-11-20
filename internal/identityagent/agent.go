// Package identityagent serves the standard SSH Agent protocol. Its owner
// interface is separate: agent clients can only list keys and request signatures.
package identityagent

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"math/big"
	"net"
	"path/filepath"
	"strings"
	"sync"
	"time"
	"unicode"

	"golang.org/x/crypto/ssh"
	"golang.org/x/crypto/ssh/agent"
	"sshclient/internal/identitymodel"
	"sshclient/internal/localipc"
)

type nativeKey struct {
	ID             string `json:"id"`
	Name           string `json:"name"`
	Authentication string `json:"authentication"`
	Public         []byte `json:"public"`
}
type command struct {
	ID              uint64 `json:"request"`
	Action          string `json:"action"`
	Key             string `json:"key,omitempty"`
	Name            string `json:"name,omitempty"`
	Authentication  string `json:"authentication,omitempty"`
	Seconds         int    `json:"seconds,omitempty"`
	Data            []byte `json:"data,omitempty"`
	PID             int    `json:"pid,omitempty"`
	Target          string `json:"target,omitempty"`
	TargetKind      string `json:"targetKind,omitempty"`
	TargetDetail    string `json:"targetDetail,omitempty"`
	TargetHost      string `json:"targetHost,omitempty"`
	Batch           string `json:"batch,omitempty"`
	ProcessInstance string `json:"processInstance,omitempty"`
	ExecutableID    string `json:"executableId,omitempty"`
	LeaseID         string `json:"leaseId,omitempty"`
	IdleSeconds     int    `json:"idleSeconds"`
}
type reply struct {
	Audit         *identitymodel.AuditEvent `json:"audit,omitempty"`
	Leases        []identitymodel.Lease     `json:"leases,omitempty"`
	Authorization string                    `json:"authorization,omitempty"`
	ID            uint64                    `json:"request"`
	Error         string                    `json:"error,omitempty"`
	Supported     bool                      `json:"supported"`
	Keys          []nativeKey               `json:"keys"`
	Signature     []byte                    `json:"signature,omitempty"`
	Pending       int                       `json:"pending"`
	Notifications string                    `json:"notifications"`
}
type backend interface {
	call(context.Context, command) (reply, error)
	Close() error
}

type Server struct {
	audit       func(*SignEvent) error
	backend     backend
	ctx         context.Context
	cancel      context.CancelFunc
	listener    net.Listener
	release     func()
	mu          sync.Mutex
	connections map[net.Conn]struct{}
	wg          sync.WaitGroup
	once        sync.Once
}

func Open(dir string) (*Server, error) { return open(dir, newBackend(dir)) }

// OpenWithAudit seals native authorization events and verified signing outcomes
// before a signature can leave the agent. No message payload or private key is logged.
func OpenWithAudit(dir string, sign func(*SignEvent) error, native func(identitymodel.AuditEvent) error) (*Server, error) {
	b := newBackend(dir)
	if observer, ok := b.(interface {
		setAudit(func(identitymodel.AuditEvent) error)
	}); ok {
		observer.setAudit(native)
	}
	s, err := openAudited(dir, b, sign)
	if err != nil {
		b.Close()
		return nil, err
	}
	return s, nil
}

type SignEvent struct {
	// The broker enriches this once, while the verified requester is present;
	// completion reuses it instead of looking up a potentially recycled PID.
	Executable, Bundle                                                                   string
	ID, Event, Key, Fingerprint, Target, Batch, DataHash, Authorization, Outcome, Reason string
	PeerPID                                                                              int
	ProcessInstance, ExecutableID                                                        string
}

func open(dir string, b backend) (*Server, error) { return openAudited(dir, b, nil) }
func openAudited(dir string, b backend, audit func(*SignEvent) error) (*Server, error) {
	release, err := localipc.Acquire(filepath.Dir(identitymodel.SocketPath(dir)))
	if err != nil {
		return nil, err
	}
	listener, err := localipc.Listen(filepath.Dir(identitymodel.SocketPath(dir)))
	if err != nil {
		release()
		return nil, err
	}
	ctx, cancel := context.WithCancel(context.Background())
	s := &Server{audit: audit, backend: b, ctx: ctx, cancel: cancel, listener: listener, release: release, connections: map[net.Conn]struct{}{}}
	s.wg.Add(1)
	go s.accept()
	return s, nil
}
func (s *Server) Close() {
	s.once.Do(func() {
		s.cancel()
		s.listener.Close()
		s.mu.Lock()
		for c := range s.connections {
			c.Close()
		}
		s.mu.Unlock()
		s.backend.Close()
		s.wg.Wait()
		s.release()
	})
}
func (s *Server) State(ctx context.Context) (identitymodel.State, error) {
	r, err := s.backend.call(ctx, command{Action: "list"})
	if err != nil {
		return identitymodel.State{}, err
	}
	out := identitymodel.State{Supported: r.Supported, Pending: r.Pending, Notifications: r.Notifications, Socket: s.listener.Addr().String(), Leases: r.Leases}
	for _, k := range r.Keys {
		pub, e := publicKey(k)
		if e != nil {
			return out, e
		}
		out.Keys = append(out.Keys, identitymodel.Key{ID: k.ID, Name: k.Name, Authentication: k.Authentication, PublicKey: string(ssh.MarshalAuthorizedKey(pub)), Fingerprint: ssh.FingerprintSHA256(pub)})
	}
	return out, nil
}
func (s *Server) Change(ctx context.Context, c identitymodel.Change) error {
	if len(c.ID) > 128 || len(c.Name) > 128 {
		return errors.New("密钥名称或标识过长")
	}
	switch c.Action {
	case "create":
		if strings.TrimSpace(c.Name) == "" || strings.IndexFunc(c.Name, unicode.IsControl) >= 0 || !validAuthentication(c.Authentication) {
			return errors.New("请选择密钥名称与验证方式")
		}
	case "delete":
		if c.ID == "" {
			return errors.New("密钥标识为空")
		}
	case "lease-policy", "revoke":
		if c.ID == "" || c.LeaseID == "" || len(c.LeaseID) > 128 || c.Action == "lease-policy" && c.IdleSeconds != 0 && (c.IdleSeconds < 60 || c.IdleSeconds > 1800) {
			return errors.New("密钥授权规则无效")
		}
	case "persist":
		if c.ID == "" || !validDuration(c.Seconds) {
			return errors.New("无效的密钥限时授权")
		}
	case "pending", "notifications", "clear":
	default:
		return errors.New("未知的密钥管理操作")
	}
	_, err := s.backend.call(ctx, command{Action: c.Action, Key: c.ID, Name: c.Name, Authentication: c.Authentication, Seconds: c.Seconds, LeaseID: c.LeaseID, IdleSeconds: c.IdleSeconds})
	return err
}
func validAuthentication(a string) bool { return a == "presence" || a == "biometry" || a == "none" }
func validDuration(n int) bool          { return n == 60 || n == 300 || n == 3600 || n == 86400 }
func publicKey(k nativeKey) (ssh.PublicKey, error) {
	x, y := elliptic.Unmarshal(elliptic.P256(), k.Public)
	if x == nil {
		return nil, errors.New("无效的安全隔区公钥")
	}
	return ssh.NewPublicKey(&ecdsa.PublicKey{Curve: elliptic.P256(), X: x, Y: y})
}
func (s *Server) accept() {
	defer s.wg.Done()
	for {
		c, err := s.listener.Accept()
		if err != nil {
			return
		}
		s.mu.Lock()
		if len(s.connections) >= 32 || s.ctx.Err() != nil {
			s.mu.Unlock()
			c.Close()
			continue
		}
		s.connections[c] = struct{}{}
		s.wg.Add(1)
		s.mu.Unlock()
		go s.serve(c)
	}
}
func (s *Server) serve(c net.Conn) {
	defer s.wg.Done()
	defer c.Close()
	defer func() { s.mu.Lock(); delete(s.connections, c); s.mu.Unlock() }()
	peer, err := localipc.Identify(c)
	if err != nil {
		return
	}
	defer peer.Close()
	ctx, cancel := context.WithCancel(s.ctx)
	defer cancel()
	go localipc.WatchDisconnect(ctx, c, cancel)
	serveProtocol(c, &connectionAgent{server: s, ctx: ctx, peer: peer})
}

var denied = errors.New("SSH代理仅允许列出公钥与经过验证的签名")

type connectionAgent struct {
	server *Server
	ctx    context.Context
	peer   *localipc.ProcessIdentity
}

func (a *connectionAgent) List() ([]*agent.Key, error) {
	if a.peer.Verify() != nil {
		return nil, denied
	}
	r, err := a.server.backend.call(a.ctx, command{Action: "list"})
	if err != nil {
		return nil, err
	}
	var keys []*agent.Key
	for _, k := range r.Keys {
		pub, e := publicKey(k)
		if e != nil {
			return nil, e
		}
		keys = append(keys, &agent.Key{Format: pub.Type(), Blob: pub.Marshal(), Comment: k.Name})
	}
	return keys, nil
}
func (a *connectionAgent) Sign(key ssh.PublicKey, data []byte) (*ssh.Signature, error) {
	return a.SignWithFlags(key, data, 0)
}
func (a *connectionAgent) SignWithFlags(key ssh.PublicKey, data []byte, flags agent.SignatureFlags) (result *ssh.Signature, signErr error) {
	if flags != 0 || len(data) > 256<<10 || a.peer.Verify() != nil {
		return nil, denied
	}
	var identifier [16]byte
	if _, err := rand.Read(identifier[:]); err != nil {
		return nil, err
	}
	digest := sha256.Sum256(data)
	target := signingTarget(data)
	event := SignEvent{ID: hex.EncodeToString(identifier[:]), Event: "ssh_requested", Fingerprint: ssh.FingerprintSHA256(key), Target: target.Label, Batch: target.Batch, DataHash: hex.EncodeToString(digest[:]), PeerPID: a.peer.PID, ProcessInstance: a.peer.Instance, ExecutableID: a.peer.CodeID, Bundle: a.peer.AppID}
	if a.server.audit != nil {
		if err := a.server.audit(&event); err != nil {
			return nil, err
		}
	}
	defer func() {
		event.Event = "ssh_signed"
		event.Outcome = "success"
		if signErr != nil {
			event.Outcome = "failed"
			event.Reason = "signing_failed"
			if a.ctx.Err() != nil {
				event.Outcome = "cancelled"
				event.Reason = "caller_disconnected"
			}
		}
		if a.server.audit != nil {
			if err := a.server.audit(&event); err != nil {
				result = nil
				signErr = err
			}
		}
	}()
	ctx, cancel := context.WithTimeout(a.ctx, 2*time.Minute)
	defer cancel()
	r, err := a.server.backend.call(ctx, command{Action: "list"})
	if err != nil {
		return nil, err
	}
	id := ""
	for _, k := range r.Keys {
		pub, e := publicKey(k)
		if e == nil && string(pub.Marshal()) == string(key.Marshal()) {
			id = k.ID
			break
		}
	}
	if id == "" {
		return nil, denied
	}
	event.Key = id
	r, err = a.server.backend.call(ctx, command{
		Action: "sign", Key: id, Data: append([]byte(nil), data...), PID: a.peer.PID,
		Target: target.Label, TargetKind: target.Kind, TargetDetail: target.Detail, TargetHost: target.Host,
		Batch: target.Batch, ProcessInstance: a.peer.Instance, ExecutableID: a.peer.CodeID,
	})
	if err != nil {
		return nil, err
	}
	event.Authorization = r.Authorization
	if ctx.Err() != nil || a.peer.Verify() != nil || len(r.Signature) != 64 {
		return nil, denied
	}
	encoded := ssh.Marshal(struct{ R, S *big.Int }{new(big.Int).SetBytes(r.Signature[:32]), new(big.Int).SetBytes(r.Signature[32:])})
	signature := &ssh.Signature{Format: key.Type(), Blob: encoded}
	if err = key.Verify(data, signature); err != nil {
		return nil, err
	}
	return signature, nil
}
func (*connectionAgent) Add(agent.AddedKey) error       { return denied }
func (*connectionAgent) Remove(ssh.PublicKey) error     { return denied }
func (*connectionAgent) RemoveAll() error               { return denied }
func (*connectionAgent) Lock([]byte) error              { return denied }
func (*connectionAgent) Unlock([]byte) error            { return denied }
func (*connectionAgent) Signers() ([]ssh.Signer, error) { return nil, denied }
func (*connectionAgent) Extension(string, []byte) ([]byte, error) {
	return nil, agent.ErrExtensionUnsupported
}
