package service

import (
	"context"

	"sshclient/internal/broker"
	"sshclient/internal/localipc"
	"sshclient/internal/protocol"
)

const (
	serviceActionLock         = "lock"
	serviceActionStop         = "stop"
	serviceActionLocked       = "locked"
	serviceActionStopAccepted = "stopAccepted"
)

// Lock closes the currently unlocked broker core without stopping the service
// listener. It is the trusted local entry point for a future OS lock or sleep
// adapter; wire clients cannot call it without their live owner lease.
func (h *Host) Lock() error {
	if h == nil {
		return nil
	}
	h.lifecycle.Lock()
	defer h.lifecycle.Unlock()
	return h.lockCoreLocked()
}

// lockCoreLocked invalidates the current generation, removes pending owner
// state, and closes the broker core. The caller holds h.lifecycle.
func (h *Host) lockCoreLocked() error {
	if h == nil {
		return nil
	}
	h.mu.Lock()
	core := h.core
	h.core = nil
	h.generation++
	h.mu.Unlock()

	h.cleanupPendingLocked()
	if core == nil {
		return nil
	}
	core.Close()
	return core.SealAudit()
}

func (h *Host) lockOwnerControl(
	controlCtx context.Context,
	watcher *controlCloseWatcher,
	core *broker.Server,
	lease *broker.OwnerLease,
	identity *localipc.ProcessIdentity,
	binding string,
	activeGeneration uint64,
	negotiatedMinor int,
	message protocol.ServiceMessage,
) (protocol.ServiceStatus, error, bool) {
	if h == nil {
		return protocol.ServiceStatus{}, nil, false
	}
	h.lifecycle.Lock()
	if !h.ownerControlAuthorizedLocked(
		serviceActionLock, controlCtx, core, lease, identity, binding,
		activeGeneration, negotiatedMinor, message,
	) {
		h.lifecycle.Unlock()
		return protocol.ServiceStatus{}, nil, false
	}
	watcher.Stop()
	if identity.Verify() != nil || !h.ownerControlLive(controlCtx, core, lease, activeGeneration) {
		h.lifecycle.Unlock()
		return protocol.ServiceStatus{}, nil, false
	}
	err := h.lockCoreLocked()
	status := h.Status()
	h.lifecycle.Unlock()
	return status, err, true
}

func (h *Host) stopOwnerControl(
	controlCtx context.Context,
	watcher *controlCloseWatcher,
	core *broker.Server,
	lease *broker.OwnerLease,
	identity *localipc.ProcessIdentity,
	binding string,
	activeGeneration uint64,
	negotiatedMinor int,
	message protocol.ServiceMessage,
) (protocol.ServiceStatus, bool) {
	if h == nil {
		return protocol.ServiceStatus{}, false
	}
	h.lifecycle.Lock()
	if !h.ownerControlAuthorizedLocked(
		serviceActionStop, controlCtx, core, lease, identity, binding,
		activeGeneration, negotiatedMinor, message,
	) {
		h.lifecycle.Unlock()
		return protocol.ServiceStatus{}, false
	}
	watcher.Stop()
	if identity.Verify() != nil || !h.ownerControlLive(controlCtx, core, lease, activeGeneration) {
		h.lifecycle.Unlock()
		return protocol.ServiceStatus{}, false
	}

	h.mu.Lock()
	select {
	case <-lease.Done():
		h.mu.Unlock()
		h.lifecycle.Unlock()
		return protocol.ServiceStatus{}, false
	default:
	}
	if h.closing || h.stopping || h.ctx.Err() != nil || controlCtx.Err() != nil ||
		h.core != core || h.generation != activeGeneration {
		h.mu.Unlock()
		h.lifecycle.Unlock()
		return protocol.ServiceStatus{}, false
	}
	h.stopping = true
	h.generation++
	h.mu.Unlock()

	h.cleanupPendingLocked()
	status := h.Status()
	h.lifecycle.Unlock()
	return status, true
}

func (h *Host) ownerControlAuthorizedLocked(
	action string,
	controlCtx context.Context,
	core *broker.Server,
	lease *broker.OwnerLease,
	identity *localipc.ProcessIdentity,
	binding string,
	activeGeneration uint64,
	negotiatedMinor int,
	message protocol.ServiceMessage,
) bool {
	if h == nil || core == nil || lease == nil || identity == nil || binding == "" || message.Action != action {
		return false
	}
	info := lease.Info()
	if info.ID == "" || message.Major != protocol.ServiceProtocolMajor ||
		message.Minor != negotiatedMinor || message.Epoch != h.epoch ||
		message.Generation != activeGeneration || message.ID != info.ID {
		return false
	}
	if !h.ownerControlLive(controlCtx, core, lease, activeGeneration) {
		return false
	}
	control := protocol.ServiceOwnerControl{
		Action:     message.Action,
		Epoch:      h.epoch,
		Generation: activeGeneration,
		Binding:    binding,
		OwnerID:    info.ID,
	}
	payload, err := protocol.ServiceOwnerControlPayload(control)
	if err != nil {
		return false
	}
	defer clear(payload)
	if err := core.VerifyOwnerControl(lease, identity, payload, message.Proof); err != nil {
		return false
	}
	return h.ownerControlLive(controlCtx, core, lease, activeGeneration)
}
