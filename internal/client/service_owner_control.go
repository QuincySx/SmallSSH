package client

import (
	"context"
	"errors"
	"time"

	"sshclient/internal/protocol"
)

const (
	ownerControlLock         = "lock"
	ownerControlStop         = "stop"
	ownerControlLocked       = "locked"
	ownerControlStopAccepted = "stopAccepted"
	ownerControlAuditFailure = "audit_unavailable"
)

// Lock asks the service to close its current broker core through the owner
// session's existing authenticated control connection.
func (s *OwnerSession) Lock(ctx context.Context) (protocol.ServiceStatus, error) {
	return s.control(ctx, ownerControlLock)
}

// Stop asks the service to shut down through the owner session's existing
// authenticated control connection.
func (s *OwnerSession) Stop(ctx context.Context) (protocol.ServiceStatus, error) {
	return s.control(ctx, ownerControlStop)
}

func (s *OwnerSession) control(ctx context.Context, action string) (protocol.ServiceStatus, error) {
	if s == nil {
		return protocol.ServiceStatus{}, errOwnerSessionClosed
	}
	defer s.Close()
	if ctx == nil {
		return protocol.ServiceStatus{}, errors.New("所有者控制上下文无效")
	}
	if err := ctx.Err(); err != nil {
		return protocol.ServiceStatus{}, err
	}

	s.mu.Lock()
	if s.closed || s.signer == nil {
		s.mu.Unlock()
		return protocol.ServiceStatus{}, errOwnerSessionClosed
	}
	if !time.Now().Before(s.info.Expires) {
		s.mu.Unlock()
		return protocol.ServiceStatus{}, errOwnerSessionExpired
	}
	if s.generation == ^uint64(0) {
		s.mu.Unlock()
		return protocol.ServiceStatus{}, errors.New("所有者会话代次无法推进")
	}
	info := s.info
	minor := s.minor
	generation := s.generation
	binding := s.binding
	signer := s.signer
	s.mu.Unlock()

	control := protocol.ServiceOwnerControl{
		Action:     action,
		Epoch:      info.Epoch,
		Generation: generation,
		Binding:    binding,
		OwnerID:    info.ID,
	}
	payload, err := protocol.ServiceOwnerControlPayload(control)
	if err != nil {
		return protocol.ServiceStatus{}, err
	}
	proof, proofErr := signer.Proof(payload)
	clear(payload)
	defer clear(proof)
	if proofErr != nil {
		return protocol.ServiceStatus{}, proofErr
	}

	callCtx, cancel := context.WithTimeout(ctx, serviceCallTimeout)
	defer cancel()
	expectedAction := ownerControlLocked
	if action == ownerControlStop {
		expectedAction = ownerControlStopAccepted
	}
	response, err := s.exchange(callCtx, protocol.ServiceMessage{
		Action:     action,
		Major:      protocol.ServiceProtocolMajor,
		Minor:      minor,
		Epoch:      info.Epoch,
		Generation: generation,
		ID:         info.ID,
		Proof:      proof,
	}, expectedAction, generation+1, true)
	if err != nil {
		return protocol.ServiceStatus{}, err
	}
	defer clearServiceMessageSecrets(&response)

	status, responseErr, ok := ownerControlResponse(action, response, minor, info.Epoch, generation)
	if !ok {
		return protocol.ServiceStatus{}, errors.New("本地服务返回的所有者控制响应无效")
	}
	return status, responseErr
}

func ownerControlResponse(action string, response protocol.ServiceMessage, minor int, epoch string, generation uint64) (protocol.ServiceStatus, error, bool) {
	wantAction, wantState := ownerControlLocked, protocol.ServiceStateLocked
	if action == ownerControlStop {
		wantAction, wantState = ownerControlStopAccepted, protocol.ServiceStateStopping
	}
	if response.Action != wantAction ||
		response.Major != protocol.ServiceProtocolMajor ||
		response.Minor != minor ||
		response.Epoch != epoch ||
		response.Generation != generation+1 ||
		response.Status == nil ||
		response.Challenge != "" ||
		response.ID != "" ||
		response.OwnerPrepare != nil ||
		response.OwnerInfo != nil ||
		len(response.UnlockKey) != 0 ||
		len(response.Proof) != 0 {
		return protocol.ServiceStatus{}, nil, false
	}

	status := *response.Status
	if status.Generation != response.Generation ||
		status.ProtocolMajor != protocol.ServiceProtocolMajor ||
		status.ProtocolMinor != minor ||
		status.State != wantState ||
		status.StartedAt.IsZero() {
		return protocol.ServiceStatus{}, nil, false
	}

	if response.ErrorCode == "" && response.Error == "" {
		return status, nil, true
	}
	if action == ownerControlLock && response.ErrorCode == ownerControlAuditFailure {
		return status, serviceResponseError(response), true
	}
	return protocol.ServiceStatus{}, serviceResponseError(response), true
}
