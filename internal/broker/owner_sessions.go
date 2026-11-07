package broker

import (
	"context"
	"crypto/ed25519"
	"crypto/hmac"
	"errors"
	"sync"
	"time"

	"sshclient/internal/approval"
	"sshclient/internal/localipc"
	"sshclient/internal/ownercap"
)

// OwnerLease is an in-memory handle owned by the trusted local host. It exposes
// only public session metadata and a completion signal; it holds no root secret
// and cannot be constructed by a wire client.
type OwnerLease struct {
	info ownercap.SessionInfo
	done chan struct{}
	once sync.Once
}

var closedOwnerLeaseDone = func() <-chan struct{} {
	done := make(chan struct{})
	close(done)
	return done
}()

func (l *OwnerLease) Info() ownercap.SessionInfo {
	if l == nil {
		return ownercap.SessionInfo{}
	}
	return l.info
}

func (l *OwnerLease) Done() <-chan struct{} {
	if l == nil {
		return closedOwnerLeaseDone
	}
	return l.done
}

func (l *OwnerLease) finish() {
	if l == nil {
		return
	}
	l.once.Do(func() { close(l.done) })
}

// RegisterOwner is called only by trusted host code after it has authenticated
// the owner and obtained the live control connection's OS identity. It is not a
// wire action. The registry itself is fixed for this Server's lifetime.
func (s *Server) RegisterOwner(ctx context.Context, identity *localipc.ProcessIdentity, public ed25519.PublicKey, lifetime time.Duration) (*OwnerLease, error) {
	if s == nil || s.ownerRegistry == nil || s.ownerLeases == nil {
		return nil, errors.New("管理会话未启用")
	}
	// Holding ownerMu across Register and map publication makes a context
	// cancellation callback wait until the new lease is visible, so it cannot
	// miss closing Done. Register itself never calls onRevoke while its lock is
	// held, and the callback reacquires ownerMu only after Register returns.
	s.ownerMu.Lock()
	defer s.ownerMu.Unlock()
	info, err := s.ownerRegistry.Register(ctx, identity, public, lifetime)
	if err != nil {
		return nil, err
	}
	lease := &OwnerLease{info: info, done: make(chan struct{})}
	s.ownerLeases[info.ID] = lease
	return lease, nil
}

// VerifyOwnerControl authenticates a management command against one exact live
// lease. The registry checks the caller's real process, control context, expiry,
// and signature; the map checks prevent a public session ID from standing in for
// the lease pointer the host actually owns.
func (s *Server) VerifyOwnerControl(lease *OwnerLease, caller *localipc.ProcessIdentity, payload, proof []byte) error {
	if s == nil || s.ownerRegistry == nil || lease == nil || caller == nil || len(payload) == 0 || len(proof) == 0 {
		return approval.ErrDenied
	}
	id := lease.Info().ID
	if id == "" {
		return approval.ErrDenied
	}
	s.ownerMu.Lock()
	live := s.ownerLeases[id] == lease
	s.ownerMu.Unlock()
	if !live {
		return approval.ErrDenied
	}
	sessionID, err := s.ownerRegistry.Verify(caller, payload, proof)
	if err != nil || sessionID != id {
		return approval.ErrDenied
	}
	s.ownerMu.Lock()
	defer s.ownerMu.Unlock()
	if s.ownerLeases[id] != lease {
		return approval.ErrDenied
	}
	return nil
}

// RevokeOwnerSession revokes one in-memory management session. The registry
// callback closes its lease and only the channels that actually verified this
// session; global approval state and connection pools are not cleared.
func (s *Server) RevokeOwnerSession(id string) {
	if s == nil || s.ownerRegistry == nil || id == "" {
		return
	}
	s.ownerRegistry.Revoke(id)
}

func (s *Server) ownerSessionRevoked(id string) {
	if s == nil || id == "" {
		return
	}
	s.ownerMu.Lock()
	lease := s.ownerLeases[id]
	delete(s.ownerLeases, id)
	var channels []*callerChannel
	for _, channel := range s.channels {
		if channel.ownerSession == id {
			channels = append(channels, channel)
		}
	}
	s.ownerMu.Unlock()

	// Order matters: the host observes Done before the control work is torn
	// down, and no new proof can bind once the lease has left the private map.
	if lease != nil {
		lease.finish()
	}
	for _, channel := range channels {
		channel.cancel()
		_ = channel.conn.Close()
	}
}

type ownerPayloadBuilder func(prove func([]byte) ([]byte, error)) ([]byte, error)

// captureOwnerPayload runs the existing proof builder with a capture callback.
// It never asks the vault to sign and never persists the captured bytes.
func captureOwnerPayload(build ownerPayloadBuilder) ([]byte, error) {
	if build == nil {
		return nil, errors.New("所有者证明载荷构建器为空")
	}
	var payload []byte
	calls := 0
	_, err := build(func(input []byte) ([]byte, error) {
		calls++
		if calls != 1 {
			return nil, errors.New("所有者证明载荷回调重复")
		}
		payload = append([]byte(nil), input...)
		return nil, nil
	})
	if err != nil {
		return nil, err
	}
	if calls != 1 || len(payload) == 0 {
		return nil, errors.New("所有者证明载荷为空")
	}
	return payload, nil
}

func (s *Server) consumeOwnerChallenge(id string) bool {
	if s == nil || id == "" {
		return false
	}
	s.ownerMu.Lock()
	expires, ok := s.challenges[id]
	delete(s.challenges, id)
	s.ownerMu.Unlock()
	return ok && time.Now().Before(expires)
}

// verifyOwnerChallenge consumes a one-use challenge before validating either
// legacy vault HMAC bytes or an owner-session envelope. The builder supplies the
// exact bytes already covered by that action's existing proof domain.
func (s *Server) verifyOwnerChallenge(channel *callerChannel, challenge string, build ownerPayloadBuilder, proof []byte) (string, bool) {
	if !s.consumeOwnerChallenge(challenge) {
		return "", false
	}
	payload, err := captureOwnerPayload(build)
	if err != nil {
		return "", false
	}
	return s.verifyOwnerProofPayload(channel, payload, proof)
}

// verifyOwnerProofPayload preserves the legacy root HMAC path and then accepts
// an ownercap envelope only when it is signed for this exact payload and the
// actual accepted channel's live OS identity. A successful management proof is
// recorded on the channel; message fields and public session IDs never are.
func (s *Server) verifyOwnerProofPayload(channel *callerChannel, payload, proof []byte) (string, bool) {
	if s == nil || s.Vault == nil || len(payload) == 0 || len(proof) == 0 {
		return "", false
	}
	expected, err := s.Vault.Proof(payload)
	if err == nil && hmac.Equal(expected, proof) {
		return "", true
	}
	if s.ownerRegistry == nil || channel == nil {
		return "", false
	}

	s.ownerMu.Lock()
	live := channel.identity != nil && channel.ctx.Err() == nil && s.channels[channel.id] == channel
	s.ownerMu.Unlock()
	if !live {
		return "", false
	}
	sessionID, err := s.ownerRegistry.Verify(channel.identity, payload, proof)
	if err != nil || sessionID == "" {
		return "", false
	}

	s.ownerMu.Lock()
	defer s.ownerMu.Unlock()
	if s.ownerLeases[sessionID] == nil || channel.ctx.Err() != nil || s.channels[channel.id] != channel {
		return "", false
	}
	if channel.ownerSession != "" && channel.ownerSession != sessionID {
		return "", false
	}
	channel.ownerSession = sessionID
	return sessionID, true
}

func (s *Server) ownerSessionMayDecide(request approval.Request, sessionID string) bool {
	if s == nil || request.ID == "" || sessionID == "" || !request.OwnerIssued || request.Operation.Kind == "grant" {
		return false
	}
	s.ownerMu.Lock()
	defer s.ownerMu.Unlock()
	return s.requestOwners[request.ID] == sessionID &&
		s.requestChannels[request.ID] != nil &&
		s.requestChannels[request.ID].ownerSession == sessionID
}

func (s *Server) verifyDecisionProof(channel *callerChannel, request approval.Request, allow bool, proof []byte) (string, []byte, bool) {
	payload, err := captureOwnerPayload(func(prove func([]byte) ([]byte, error)) ([]byte, error) {
		return approval.DecisionProof(request, allow, prove)
	})
	if err != nil {
		return "", nil, false
	}
	sessionID, ok := s.verifyOwnerProofPayload(channel, payload, proof)
	return sessionID, payload, ok
}

func (s *Server) verifyRevokeProof(channel *callerChannel, request approval.Request, proof []byte) (string, []byte, bool) {
	payload, err := captureOwnerPayload(func(prove func([]byte) ([]byte, error)) ([]byte, error) {
		return approval.RevokeProof(request, prove)
	})
	if err != nil {
		return "", nil, false
	}
	sessionID, ok := s.verifyOwnerProofPayload(channel, payload, proof)
	return sessionID, payload, ok
}
