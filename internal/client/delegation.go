package client

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"os"
	"path/filepath"
	"sync"
	"time"

	"sshclient/internal/approval"
	"sshclient/internal/broker"
)

var ErrSessionNotExportable = errors.New("会话仅属于当前进程的活连接，不能保存或重新加载；请使用 session serve 重新申请授权")

// Delegation owns an in-memory key and a live authenticated control channel.
// Public metadata (including Ticket) cannot confer authority on another channel.
// Copies share one private state, so closing any copy invalidates all of them.
type Delegation struct {
	ReadRoot, WriteRoot, ParentID, Directory string
	Ticket, Permission, Host, PublicKey      string
	state                                    *delegationState
}

type delegationState struct {
	mu      sync.Mutex
	key     ed25519.PrivateKey
	control *requestChannel
	pid     int
}

func (c Client) RequestSession(ctx context.Context, host, label, permission string, lifetime, idle time.Duration) (approval.Request, *Delegation, error) {
	return c.RequestScopedSession(ctx, host, label, permission, lifetime, idle, "", "")
}

func (c Client) RequestScopedSession(ctx context.Context, host, label, permission string, lifetime, idle time.Duration, readRoot, writeRoot string) (approval.Request, *Delegation, error) {
	pub, key, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		return approval.Request{}, nil, err
	}
	retained := false
	defer func() {
		if !retained {
			clear(key)
		}
	}()
	scope := approval.GrantScope{ReadRoot: readRoot, WriteRoot: writeRoot, PublicKey: base64.RawURLEncoding.EncodeToString(pub), Permission: permission, LifetimeSeconds: int(lifetime / time.Second), IdleSeconds: int(idle / time.Second)}
	if err = scope.Validate(); err != nil {
		return approval.Request{}, nil, err
	}
	base := c
	base.Delegation = nil
	r, ticket, err := base.Request(ctx, approval.Operation{Host: host, Client: label, Kind: "grant", Grant: scope}, 5*time.Minute)
	if err != nil {
		return approval.Request{}, nil, err
	}
	h := c.channel(r.ID, ticket)
	if h == nil {
		return approval.Request{}, nil, approval.ErrDenied
	}
	d := &Delegation{ReadRoot: readRoot, WriteRoot: writeRoot, ParentID: r.ID, Directory: h.dir, Ticket: ticket, Permission: permission, Host: host, PublicKey: scope.PublicKey, state: &delegationState{key: key, control: h, pid: os.Getpid()}}
	context.AfterFunc(h.ctx, d.Close)
	go d.monitor()
	retained = true
	return r, d, nil
}

func (d *Delegation) monitor() {
	control := d.state.control
	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-control.ctx.Done():
			return
		case <-ticker.C:
			r, err := control.call(control.ctx, broker.Message{Action: "sessionStatus", ID: d.ParentID, Ticket: d.Ticket})
			if err != nil || r.Request == nil || !(r.Request.Status == "pending" || r.Request.Status == "approved" || r.Request.Status == "expired") {
				d.Close()
				return
			}
		}
	}
}

func (d *Delegation) Close() {
	if d == nil || d.state == nil {
		return
	}
	state := d.state
	if state.control != nil {
		state.control.close()
	}
	state.mu.Lock()
	defer state.mu.Unlock()
	clear(state.key)
	state.key = nil
}

func (d *Delegation) Validate(dir string) error {
	if d == nil || d.state == nil {
		return approval.ErrDenied
	}
	state := d.state
	state.mu.Lock()
	defer state.mu.Unlock()
	abs, err := filepath.Abs(dir)
	if err != nil || abs != d.Directory || len(state.key) != ed25519.PrivateKeySize || state.control == nil || state.control.ctx.Err() != nil || state.pid != os.Getpid() || state.control.id != d.ParentID || state.control.ticket != d.Ticket {
		return approval.ErrDenied
	}
	return nil
}

func (d *Delegation) sign(message []byte) ([]byte, error) {
	if d == nil || d.state == nil {
		return nil, approval.ErrDenied
	}
	state := d.state
	state.mu.Lock()
	defer state.mu.Unlock()
	if len(state.key) != ed25519.PrivateKeySize || state.control == nil || state.control.ctx.Err() != nil || state.pid != os.Getpid() {
		return nil, approval.ErrDenied
	}
	return ed25519.Sign(state.key, message), nil
}

func (c Client) CallWithSession(ctx context.Context, q broker.Message, d *Delegation) (broker.Message, error) {
	if err := d.Validate(c.Dir); err != nil {
		return broker.Message{}, err
	}
	switch q.Action {
	case "status", "sessionStatus", "withdraw", "delegationChallenge", "delegate", "renewalChallenge", "renewSession":
	default:
		return broker.Message{}, approval.ErrDenied
	}
	r, err := d.state.control.call(ctx, q)
	if q.Action == "withdraw" {
		d.Close()
	}
	return r, err
}

func (c Client) RequestDelegated(ctx context.Context, op approval.Operation, ttl time.Duration, d *Delegation) (approval.Request, string, error) {
	if err := d.Validate(c.Dir); err != nil {
		return approval.Request{}, "", err
	}
	var h *requestChannel
	var r broker.Message
	var err error
	for attempt := 0; attempt < 2; attempt++ {
		h, err = c.openOperation(ctx, d)
		if err != nil {
			return approval.Request{}, "", err
		}
		r, err = c.CallWithSession(ctx, broker.Message{Action: "delegationChallenge", ID: d.ParentID, Ticket: d.Ticket, Operation: op, TTL: int(min(ttl, 5*time.Minute) / time.Second), Channel: h.binding}, d)
		if attempt == 0 && (errors.Is(err, approval.ErrSessionExpired) || errors.Is(err, approval.ErrSessionRenewing)) {
			h.close()
			if err = c.renewDelegation(ctx, d, op); err != nil {
				return approval.Request{}, "", err
			}
			continue
		}
		break
	}
	success := false
	defer func() {
		if !success {
			h.close()
		}
	}()
	if err != nil {
		return approval.Request{}, "", err
	}
	if r.Delegation == nil || r.Delegation.ParentID != d.ParentID || r.Delegation.Channel != h.binding {
		return approval.Request{}, "", errors.New("会话挑战未绑定到当前操作连接")
	}
	if op.Host != d.Host {
		return approval.Request{}, "", approval.ErrDenied
	}
	challenge := *r.Delegation
	if op.Endpoint != "" && op.Endpoint != challenge.Operation.Endpoint || op.HostFingerprint != "" && op.HostFingerprint != challenge.Operation.HostFingerprint {
		return approval.Request{}, "", errors.New("授权挑战的连接目标或指纹已变化")
	}
	bound := challenge.Operation
	bound.Endpoint, bound.HostFingerprint = op.Endpoint, op.HostFingerprint
	if bound != op {
		return approval.Request{}, "", errors.New("授权挑战中的操作已变化")
	}
	sig, err := d.sign(approval.DelegationProofMessage(challenge))
	if err != nil {
		return approval.Request{}, "", err
	}
	r, err = c.CallWithSession(ctx, broker.Message{Action: "delegate", ID: challenge.ID, Proof: sig}, d)
	if err != nil {
		return approval.Request{}, "", err
	}
	if r.Request == nil || r.Request.ParentID != d.ParentID || r.Request.Operation != challenge.Operation {
		return approval.Request{}, "", errors.New("子授权与签名挑战不一致")
	}
	if err = h.register(r); err != nil {
		return approval.Request{}, "", err
	}
	success = true
	return *r.Request, r.Ticket, nil
}

func (Delegation) MarshalJSON() ([]byte, error)          { return nil, ErrSessionNotExportable }
func (*Delegation) UnmarshalJSON([]byte) error           { return ErrSessionNotExportable }
func (*Delegation) Save(string) error                    { return ErrSessionNotExportable }
func ReadDelegation(string, string) (*Delegation, error) { return nil, ErrSessionNotExportable }
func DelegationPath(string, string) (string, error)      { return "", ErrSessionNotExportable }
