package client

import (
	"context"
	"errors"
	"sync"
	"time"

	"sshclient/internal/localipc"
	"sshclient/internal/ownercap"
	"sshclient/internal/protocol"
)

const ownerSessionHeartbeatInterval = 10 * time.Second

var (
	errOwnerSessionClosed  = errors.New("所有者会话已关闭")
	errOwnerSessionExpired = errors.New("所有者会话已到期")
)

type ownerSessionWaiter struct {
	action     string
	generation uint64
	response   chan protocol.ServiceMessage
}

// OwnerSession manages an in-memory signer over an already verified control
// connection. It never owns root material.
type OwnerSession struct {
	mu       sync.Mutex
	closed   bool
	wire     *localipc.Wire
	signer   *ownercap.Signer
	inflight *ownerSessionWaiter

	info       ownercap.SessionInfo
	pin        string
	minor      int
	generation uint64
	binding    string

	ctx     context.Context
	cancel  context.CancelFunc
	expiry  *time.Timer
	request chan struct{}
	done    chan struct{}
}

func newOwnerSession(wire *localipc.Wire, signer *ownercap.Signer, info ownercap.SessionInfo, pin string, minor int, generation uint64, binding string) (*OwnerSession, error) {
	if wire == nil || wire.Conn == nil || signer == nil {
		return nil, errors.New("所有者会话资源无效")
	}
	if info.ID == "" || info.Epoch == "" || info.Expires.IsZero() ||
		pin == "" || binding == "" || minor < 1 || generation == 0 {
		return nil, errors.New("所有者会话元数据无效")
	}
	if !time.Now().Before(info.Expires) {
		return nil, errOwnerSessionExpired
	}
	if err := wire.Conn.SetDeadline(time.Time{}); err != nil {
		return nil, err
	}

	ctx, cancel := context.WithCancel(context.Background())
	session := &OwnerSession{
		wire:       wire,
		signer:     signer,
		info:       info,
		pin:        pin,
		minor:      minor,
		generation: generation,
		binding:    binding,
		ctx:        ctx,
		cancel:     cancel,
		request:    make(chan struct{}, 1),
		done:       make(chan struct{}),
	}
	session.mu.Lock()
	session.expiry = time.AfterFunc(time.Until(info.Expires), session.Close)
	session.mu.Unlock()
	go session.readLoop(wire)
	go session.heartbeatLoop(ctx)
	return session, nil
}

// Proof signs payload through the session's in-memory signer.
func (s *OwnerSession) Proof(payload []byte) ([]byte, error) {
	if s == nil {
		return nil, errOwnerSessionClosed
	}
	s.mu.Lock()
	if s.closed || s.signer == nil {
		s.mu.Unlock()
		return nil, errOwnerSessionClosed
	}
	if !time.Now().Before(s.info.Expires) {
		s.mu.Unlock()
		s.Close()
		return nil, errOwnerSessionExpired
	}
	signer := s.signer
	s.mu.Unlock()
	return signer.Proof(payload)
}

// Close releases the control connection and signer exactly once.
func (s *OwnerSession) Close() {
	if s == nil {
		return
	}
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return
	}
	s.closed = true
	wire := s.wire
	signer := s.signer
	cancel := s.cancel
	expiry := s.expiry
	done := s.done
	s.wire = nil
	s.signer = nil
	s.cancel = nil
	s.expiry = nil
	s.mu.Unlock()

	if cancel != nil {
		cancel()
	}
	if expiry != nil {
		expiry.Stop()
	}
	if wire != nil {
		_ = wire.Close()
	}
	if signer != nil {
		signer.Close()
	}
	if done != nil {
		close(done)
	}
}

// Done is closed when the session is closed or expires.
func (s *OwnerSession) Done() <-chan struct{} {
	if s == nil {
		return nil
	}
	return s.done
}

// Info returns a copy of the immutable public session metadata.
func (s *OwnerSession) Info() ownercap.SessionInfo {
	if s == nil {
		return ownercap.SessionInfo{}
	}
	return s.info
}

// BrokerPublicKeyPin returns the immutable broker identity pin captured when
// the session was constructed.
func (s *OwnerSession) BrokerPublicKeyPin() (string, error) {
	if s == nil || s.pin == "" {
		return "", errors.New("所有者会话主机密钥固定值不可用")
	}
	return s.pin, nil
}

func (s *OwnerSession) acquireRequest(ctx context.Context) error {
	select {
	case s.request <- struct{}{}:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	case <-s.Done():
		return errOwnerSessionClosed
	}
}

func (s *OwnerSession) exchange(ctx context.Context, request protocol.ServiceMessage, expectedAction string, expectedGeneration uint64, terminal bool) (protocol.ServiceMessage, error) {
	if s == nil {
		return protocol.ServiceMessage{}, errOwnerSessionClosed
	}
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		return protocol.ServiceMessage{}, err
	}
	if err := s.acquireRequest(ctx); err != nil {
		return protocol.ServiceMessage{}, err
	}
	defer func() {
		if terminal {
			s.Close()
		}
		<-s.request
	}()
	if err := ctx.Err(); err != nil {
		return protocol.ServiceMessage{}, err
	}

	waiter := &ownerSessionWaiter{
		action:     expectedAction,
		generation: expectedGeneration,
		response:   make(chan protocol.ServiceMessage, 1),
	}
	s.mu.Lock()
	if s.closed || s.wire == nil || s.signer == nil {
		s.mu.Unlock()
		return protocol.ServiceMessage{}, errOwnerSessionClosed
	}
	if !time.Now().Before(s.info.Expires) {
		s.mu.Unlock()
		s.Close()
		return protocol.ServiceMessage{}, errOwnerSessionExpired
	}
	s.inflight = waiter
	wire := s.wire
	s.mu.Unlock()

	stopCancel := context.AfterFunc(ctx, s.Close)
	sendErr := serviceSend(ctx, wire, request)
	if sendErr != nil {
		s.mu.Lock()
		if s.inflight == waiter {
			s.inflight = nil
		}
		s.mu.Unlock()
		stopCancel()
		_ = wire.Conn.SetDeadline(time.Time{})
		s.Close()
		if err := ctx.Err(); err != nil {
			return protocol.ServiceMessage{}, err
		}
		return protocol.ServiceMessage{}, serviceCallError(ctx, sendErr)
	}

	var response protocol.ServiceMessage
	var waitErr error
	received := false
	select {
	case response = <-waiter.response:
		received = true
	case <-ctx.Done():
		select {
		case response = <-waiter.response:
			received = true
		default:
			waitErr = ctx.Err()
		}
	case <-s.Done():
		select {
		case response = <-waiter.response:
			received = true
		default:
			waitErr = errOwnerSessionClosed
		}
	}

	s.mu.Lock()
	if s.inflight == waiter {
		s.inflight = nil
	}
	s.mu.Unlock()
	stopCancel()
	_ = wire.Conn.SetDeadline(time.Time{})
	if !received {
		s.Close()
		if waitErr == nil {
			waitErr = errOwnerSessionClosed
		}
		return protocol.ServiceMessage{}, waitErr
	}
	return response, nil
}

func (s *OwnerSession) readLoop(wire *localipc.Wire) {
	for {
		var message protocol.ServiceMessage
		err := wire.Receive(&message)
		hasSecrets := len(message.UnlockKey) != 0 || len(message.Proof) != 0
		clearServiceMessageSecrets(&message)
		if err != nil {
			s.Close()
			return
		}

		s.mu.Lock()
		if s.closed {
			s.mu.Unlock()
			return
		}
		waiter := s.inflight
		if waiter == nil ||
			hasSecrets ||
			message.Major != protocol.ServiceProtocolMajor ||
			message.Minor != s.minor ||
			message.Epoch != s.info.Epoch ||
			message.Action != waiter.action ||
			message.Generation != waiter.generation ||
			message.Challenge != "" ||
			message.ID != "" ||
			message.OwnerPrepare != nil ||
			message.OwnerInfo != nil {
			s.mu.Unlock()
			s.Close()
			return
		}
		s.inflight = nil
		waiter.response <- message
		s.mu.Unlock()
	}
}

func (s *OwnerSession) heartbeatLoop(ctx context.Context) {
	ticker := time.NewTicker(ownerSessionHeartbeatInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-s.Done():
			return
		case <-ticker.C:
		}

		heartbeatCtx, cancel := context.WithTimeout(ctx, serviceCallTimeout)
		response, err := s.exchange(heartbeatCtx, protocol.ServiceMessage{
			Action:     "heartbeat",
			Major:      protocol.ServiceProtocolMajor,
			Minor:      s.minor,
			Epoch:      s.info.Epoch,
			Generation: s.generation,
		}, "heartbeat", s.generation, false)
		cancel()
		if err != nil || response.ErrorCode != "" || response.Error != "" || response.Status != nil {
			s.Close()
			return
		}
	}
}
