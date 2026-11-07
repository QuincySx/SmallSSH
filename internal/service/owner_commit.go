package service

import (
	"context"
	"errors"
	"net"
	"sync"
	"time"

	"sshclient/internal/broker"
	"sshclient/internal/localipc"
	"sshclient/internal/protocol"
)

var errOwnerCommitFailed = errors.New("owner commit failed")

func coreDone(core *broker.Server) bool {
	if core == nil {
		return false
	}
	select {
	case <-core.Done():
		return true
	default:
		return false
	}
}

func coreStopped(core *broker.Server) bool {
	return coreDone(core) || core.Err() != nil
}

func (h *Host) commitOwner(
	pending *preparedOwner,
	pendingTimer *time.Timer,
	controlCtx context.Context,
	identity *localipc.ProcessIdentity,
	negotiatedMinor int,
	message protocol.ServiceMessage,
) (*broker.Server, *broker.OwnerLease, protocol.ServiceOwnerInfo, error) {
	deny := func() (*broker.Server, *broker.OwnerLease, protocol.ServiceOwnerInfo, error) {
		return nil, nil, protocol.ServiceOwnerInfo{}, errOwnerCommitFailed
	}
	if h == nil || pending == nil || pendingTimer == nil || controlCtx == nil || identity == nil {
		return deny()
	}
	if message.Action != "commitOwner" || message.Major != protocol.ServiceProtocolMajor ||
		message.Minor != negotiatedMinor || message.Epoch != h.epoch ||
		message.Generation != pending.generation || message.ID != pending.id {
		return deny()
	}
	if !pendingTimer.Stop() {
		return deny()
	}

	h.lifecycle.Lock()
	defer h.lifecycle.Unlock()
	if pending.cleaned {
		return deny()
	}
	if _, ok := h.pending[pending]; !ok {
		return deny()
	}
	acceptedAt := time.Now()
	h.mu.Lock()
	closing := h.closing || h.stopping
	generation := h.generation
	currentCore := h.core
	h.mu.Unlock()
	if closing || h.ctx.Err() != nil || controlCtx.Err() != nil || identity.Verify() != nil ||
		generation != pending.generation || message.Generation != generation ||
		!acceptedAt.Before(pending.expiresAt) || pending.session == nil {
		return deny()
	}
	if coreStopped(currentCore) {
		_ = h.lockCoreLocked()
		return deny()
	}

	key, err := pending.session.UnlockKey()
	if err != nil {
		return deny()
	}
	defer clear(key)
	expectedPin, err := pending.session.BrokerPublicKeyPin()
	if err != nil || expectedPin == "" || len(key) != 32 {
		return deny()
	}

	core := currentCore
	createdCore := false
	if core == nil {
		core, err = broker.OpenManagedKey(h.dir, key, h.epoch, expectedPin)
		if err != nil {
			return deny()
		}
		createdCore = true
	} else if core.PublicKeyPin() != expectedPin {
		return deny()
	}
	if coreStopped(core) {
		if createdCore {
			core.Close()
		} else {
			_ = h.lockCoreLocked()
		}
		return deny()
	}
	lease, err := core.RegisterOwner(controlCtx, identity, pending.publicKey, pending.lifetime)
	if err != nil {
		if createdCore {
			core.Close()
		}
		return deny()
	}
	rollback := func() {
		core.RevokeOwnerSession(lease.Info().ID)
		if createdCore {
			core.Close()
		}
	}
	if h.ctx.Err() != nil || controlCtx.Err() != nil || identity.Verify() != nil {
		rollback()
		return deny()
	}
	if coreStopped(core) {
		if createdCore {
			rollback()
		} else {
			_ = h.lockCoreLocked()
		}
		return deny()
	}

	h.mu.Lock()
	stopped := coreStopped(core)
	published := !stopped && !h.closing && !h.stopping && h.ctx.Err() == nil && h.generation == generation && h.core == currentCore
	if published && createdCore {
		h.core = core
		h.startCoreWatcherLocked(core)
	}
	h.mu.Unlock()
	if !published {
		if stopped && !createdCore {
			_ = h.lockCoreLocked()
		} else {
			rollback()
		}
		return deny()
	}

	info := lease.Info()
	pending.cleanupLocked(h)
	return core, lease, protocol.ServiceOwnerInfo{
		ID:        info.ID,
		Epoch:     info.Epoch,
		Expires:   info.Expires,
		BrokerKey: core.PublicKeyPin(),
	}, nil
}

func (h *Host) ownerControlLive(controlCtx context.Context, core *broker.Server, lease *broker.OwnerLease, generation uint64) bool {
	if h == nil || controlCtx == nil || core == nil || lease == nil || controlCtx.Err() != nil {
		return false
	}
	select {
	case <-lease.Done():
		return false
	default:
	}
	if coreDone(core) {
		return false
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	return !h.closing && !h.stopping && h.ctx.Err() == nil && h.core == core && h.generation == generation
}

type controlCloseWatcher struct {
	mu     sync.Mutex
	active bool
	cancel context.CancelFunc
	done   chan struct{}
}

func newControlCloseWatcher(raw net.Conn, leaseDone, controlDone <-chan struct{}) *controlCloseWatcher {
	ctx, cancel := context.WithCancel(context.Background())
	watcher := &controlCloseWatcher{active: true, cancel: cancel, done: make(chan struct{})}
	go func() {
		select {
		case <-leaseDone:
		case <-controlDone:
		case <-ctx.Done():
		}
		watcher.mu.Lock()
		if watcher.active {
			watcher.active = false
			_ = raw.Close()
		}
		watcher.mu.Unlock()
		close(watcher.done)
	}()
	return watcher
}

func (watcher *controlCloseWatcher) Stop() {
	if watcher == nil {
		return
	}
	watcher.mu.Lock()
	watcher.active = false
	cancel := watcher.cancel
	watcher.mu.Unlock()
	if cancel != nil {
		cancel()
	}
	<-watcher.done
}
