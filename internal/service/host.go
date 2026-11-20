package service

import (
	"context"
	"crypto/rand"
	"crypto/tls"
	"encoding/hex"
	"errors"
	"fmt"
	"net"
	"path/filepath"
	"sync"
	"time"

	"sshclient/internal/broker"
	"sshclient/internal/localipc"
	"sshclient/internal/protocol"
	"sshclient/internal/systemauth"
)

const (
	serviceSubdirectory   = "service"
	serviceMaxConnections = 16
	serviceDeadline       = 10 * time.Second
)

const serviceControlDeadline = 30 * time.Second

type Host struct {
	ctx            context.Context
	cancel         context.CancelFunc
	listener       net.Listener
	configuration  *tls.Config
	release        func()
	dir            string
	epoch          string
	startedAt      time.Time
	generation     uint64
	unlockNoticeAt time.Time
	acceptDone     chan struct{}
	done           chan struct{}
	closeOnce      sync.Once
	wg             sync.WaitGroup
	coreWatchWG    sync.WaitGroup
	lifecycle      sync.Mutex
	mu             sync.Mutex
	active         map[net.Conn]struct{}
	quick          quickUnlock
	core           *broker.Server
	pending        map[*preparedOwner]struct{}
	pendingOwners  int
	closing        bool
	stopping       bool
	failure        error
}

func Open(dir string) (*Host, error) {
	if dir == "" {
		return nil, errors.New("服务数据目录为空")
	}
	serviceDir := filepath.Join(dir, serviceSubdirectory)
	release, err := localipc.Acquire(serviceDir)
	if err != nil {
		return nil, err
	}
	// The old daemon is gone once its kernel lock is acquired. Before accepting
	// any new envelopes, remove its registered external device keys. A locked or
	// unavailable keyring keeps its identifiers for a later startup retry.
	cleanupContext, cleanupCancel := context.WithTimeout(context.Background(), 5*time.Second)
	_ = systemauth.CleanupOrphans(cleanupContext, dir)
	cleanupCancel()
	listener, err := localipc.Listen(serviceDir)
	if err != nil {
		release()
		return nil, fmt.Errorf("启动本地服务监听: %w", err)
	}
	configuration, _, err := localipc.ServerTLS()
	if err != nil {
		_ = listener.Close()
		release()
		return nil, fmt.Errorf("创建本地服务临时身份: %w", err)
	}
	var rawEpoch [16]byte
	if _, err := rand.Read(rawEpoch[:]); err != nil {
		localipc.DestroyServerTLS(configuration)
		_ = listener.Close()
		release()
		return nil, fmt.Errorf("生成本地服务代次: %w", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	h := &Host{
		ctx:           ctx,
		cancel:        cancel,
		listener:      listener,
		configuration: configuration,
		release:       release,
		dir:           dir,
		epoch:         hex.EncodeToString(rawEpoch[:]),
		startedAt:     time.Now(),
		generation:    1,
		acceptDone:    make(chan struct{}),
		done:          make(chan struct{}),
		active:        make(map[net.Conn]struct{}),
		pending:       make(map[*preparedOwner]struct{}),
	}
	go h.accept()
	return h, nil
}

func (h *Host) Close() error {
	if h == nil {
		return nil
	}
	h.closeOnce.Do(h.shutdown)
	<-h.done
	return nil
}

func (h *Host) Done() <-chan struct{} { return h.done }

func (h *Host) Status() protocol.ServiceStatus {
	return h.status(protocol.ServiceProtocolMinor, 0)
}

// A locked service records only a short-lived signal. It has no broker core
// to accept an operation, and the signal carries no requester or server data.
func (h *Host) status(minor int, requestGeneration uint64) protocol.ServiceStatus {
	h.mu.Lock()
	defer h.mu.Unlock()
	state := protocol.ServiceStateLocked
	if h.closing || h.stopping {
		state = protocol.ServiceStateStopping
	} else if h.core != nil {
		state = protocol.ServiceStateUnlocked
	}
	status := protocol.ServiceStatus{
		State:         state,
		StartedAt:     h.startedAt,
		ProtocolMajor: protocol.ServiceProtocolMajor,
		ProtocolMinor: minor,
		Generation:    h.generation,
	}
	if state == protocol.ServiceStateLocked && minor >= protocol.ServiceUnlockNoticeMinor {
		now := time.Now()
		if requestGeneration == h.generation && now.Sub(h.unlockNoticeAt) >= 30*time.Second {
			h.unlockNoticeAt = now
		}
		if until := h.unlockNoticeAt.Add(10 * time.Second); now.Before(until) {
			status.UnlockNoticeUntil = &until
		}
	}
	return status
}

func Run(ctx context.Context, dir string) error {
	h, err := Open(dir)
	if err != nil {
		return err
	}
	select {
	case <-ctx.Done():
		closeErr := h.Close()
		if failure := h.failureError(); failure != nil {
			return failure
		}
		return closeErr
	case <-h.Done():
		return h.failureError()
	}
}

func (h *Host) shutdown() {
	h.mu.Lock()
	h.closing = true
	active := make([]net.Conn, 0, len(h.active))
	for connection := range h.active {
		active = append(active, connection)
	}
	h.mu.Unlock()
	h.cancel()
	_ = h.listener.Close()
	for _, connection := range active {
		_ = connection.Close()
	}
	<-h.acceptDone
	h.wg.Wait()
	h.lifecycle.Lock()
	h.cleanupPendingLocked()
	h.mu.Lock()
	core := h.core
	h.core = nil
	h.mu.Unlock()
	if core != nil {
		core.Close()
	}
	h.lifecycle.Unlock()
	h.mu.Lock()
	h.quick.wipe()
	h.mu.Unlock()
	h.coreWatchWG.Wait()
	localipc.DestroyServerTLS(h.configuration)
	if h.release != nil {
		h.release()
		h.release = nil
	}
	close(h.done)
}

func (h *Host) startCoreWatcherLocked(core *broker.Server) {
	if h == nil || core == nil {
		return
	}
	h.coreWatchWG.Add(1)
	go h.watchCore(core)
}

func (h *Host) watchCore(core *broker.Server) {
	defer h.coreWatchWG.Done()
	select {
	case <-h.ctx.Done():
		return
	case <-core.Done():
	}
	if h.ctx.Err() != nil {
		return
	}
	h.lifecycle.Lock()
	if h.ctx.Err() != nil {
		h.lifecycle.Unlock()
		return
	}
	h.mu.Lock()
	matches := h.core == core
	h.mu.Unlock()
	if !matches {
		h.lifecycle.Unlock()
		return
	}
	_ = h.lockCoreLocked()
	h.lifecycle.Unlock()
}

func (h *Host) accept() {
	defer close(h.acceptDone)
	for {
		connection, err := h.listener.Accept()
		if err != nil {
			if h.ctx.Err() != nil {
				return
			}
			h.recordFailure(fmt.Errorf("本地服务接受连接: %w", err))
			go func() { _ = h.Close() }()
			return
		}
		if !h.register(connection) {
			_ = connection.Close()
			continue
		}
		h.wg.Add(1)
		go func() {
			defer h.wg.Done()
			defer h.unregister(connection)
			h.handle(connection)
		}()
	}
}

func (h *Host) register(connection net.Conn) bool {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.closing || h.stopping || len(h.active) >= serviceMaxConnections {
		return false
	}
	h.active[connection] = struct{}{}
	return true
}

func (h *Host) unregister(connection net.Conn) {
	h.mu.Lock()
	delete(h.active, connection)
	h.mu.Unlock()
}

func (h *Host) recordFailure(err error) {
	h.mu.Lock()
	if h.failure == nil {
		h.failure = err
	}
	h.mu.Unlock()
}

func (h *Host) failureError() error {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.failure
}

func (h *Host) handle(raw net.Conn) {
	defer raw.Close()
	if err := raw.SetDeadline(time.Now().Add(serviceDeadline)); err != nil {
		return
	}
	identity, err := localipc.Identify(raw)
	if err != nil || identity == nil {
		return
	}
	defer identity.Close()
	controlCtx, cancel := context.WithCancel(h.ctx)
	var watcher *controlCloseWatcher
	var activeCore *broker.Server
	var activeLease *broker.OwnerLease
	var activeGeneration uint64
	defer func() {
		if watcher != nil {
			watcher.Stop()
		}
		cancel()
		if activeCore != nil && activeLease != nil {
			activeCore.RevokeOwnerSession(activeLease.Info().ID)
		}
	}()
	connection := tls.Server(raw, h.configuration)
	if err := connection.HandshakeContext(controlCtx); err != nil {
		return
	}
	binding, err := localipc.ChannelBinding(connection)
	if err != nil {
		return
	}
	wire := &localipc.Wire{Conn: connection, Verify: identity.Verify, WriteTimeout: serviceDeadline}
	if err := connection.SetDeadline(time.Now().Add(serviceDeadline)); err != nil {
		return
	}
	var hello protocol.ServiceMessage
	if err := wire.Receive(&hello); err != nil {
		return
	}
	if hello.Action != "hello" {
		_ = wire.Send(protocol.ServiceMessage{
			Action: "error", Major: protocol.ServiceProtocolMajor, Minor: protocol.ServiceProtocolMinor,
			Epoch: h.epoch, ErrorCode: "hello_required", Error: "必须先协商协议版本",
		})
		return
	}
	minor, compatible := negotiateServiceVersion(hello)
	if !compatible {
		_ = wire.Send(protocol.ServiceMessage{
			Action: "hello", Major: protocol.ServiceProtocolMajor, Minor: protocol.ServiceProtocolMinor,
			Epoch: h.epoch, ErrorCode: "unsupported_version", Error: "不支持的本地服务协议版本",
		})
		return
	}
	challenge, err := randomServiceToken()
	if err != nil {
		return
	}
	generation := h.generationSnapshot()
	if err := wire.Send(protocol.ServiceMessage{
		Action: "hello", Major: protocol.ServiceProtocolMajor, Minor: minor, Epoch: h.epoch,
		Challenge: challenge, Generation: generation,
	}); err != nil {
		return
	}
	var pending *preparedOwner
	var pendingTimer *time.Timer
	defer func() {
		if pendingTimer != nil {
			pendingTimer.Stop()
		}
		h.releasePending(pending)
	}()
	for {
		deadline := time.Now().Add(serviceDeadline)
		if activeLease != nil {
			deadline = time.Now().Add(serviceControlDeadline)
		} else if pending != nil {
			deadline = pending.expiresAt
		}
		if !time.Now().Before(deadline) {
			return
		}
		if err := connection.SetDeadline(deadline); err != nil {
			return
		}
		var message protocol.ServiceMessage
		if err := wire.Receive(&message); err != nil {
			return
		}
		if activeLease != nil {
			switch message.Action {
			case "status":
				status := h.status(minor, 0)
				if err := wire.Send(protocol.ServiceMessage{
					Action: "status", Major: protocol.ServiceProtocolMajor, Minor: minor, Epoch: h.epoch,
					Generation: status.Generation, Status: &status,
				}); err != nil {
					return
				}
			case "heartbeat":
				if !h.ownerControlLive(controlCtx, activeCore, activeLease, activeGeneration) {
					return
				}
				if err := wire.Send(protocol.ServiceMessage{
					Action: "heartbeat", Major: protocol.ServiceProtocolMajor, Minor: minor,
					Epoch: h.epoch, Generation: activeGeneration,
				}); err != nil {
					return
				}
			case serviceActionLock:
				status, sealErr, accepted := h.lockOwnerControl(
					controlCtx, watcher, activeCore, activeLease, identity, binding,
					activeGeneration, minor, message,
				)
				if !accepted {
					_ = wire.Send(protocol.ServiceMessage{
						Action: "error", Major: protocol.ServiceProtocolMajor, Minor: minor, Epoch: h.epoch,
						Generation: h.generationSnapshot(), ErrorCode: "invalid_owner",
						Error: "所有者控制验证失败",
					})
					return
				}
				status.ProtocolMinor = minor
				response := protocol.ServiceMessage{
					Action: serviceActionLocked, Major: protocol.ServiceProtocolMajor, Minor: minor,
					Epoch: h.epoch, Generation: status.Generation, Status: &status,
				}
				if sealErr != nil {
					response.ErrorCode = "audit_unavailable"
					response.Error = "已锁定，但审计封存失败"
				}
				_ = wire.Send(response)
				return
			case serviceActionStop:
				status, accepted := h.stopOwnerControl(
					controlCtx, watcher, activeCore, activeLease, identity, binding,
					activeGeneration, minor, message,
				)
				if !accepted {
					_ = wire.Send(protocol.ServiceMessage{
						Action: "error", Major: protocol.ServiceProtocolMajor, Minor: minor, Epoch: h.epoch,
						Generation: h.generationSnapshot(), ErrorCode: "invalid_owner",
						Error: "所有者控制验证失败",
					})
					return
				}
				status.ProtocolMinor = minor
				_ = wire.Send(protocol.ServiceMessage{
					Action: serviceActionStopAccepted, Major: protocol.ServiceProtocolMajor, Minor: minor,
					Epoch: h.epoch, Generation: status.Generation, Status: &status,
				})
				go h.Close()
				return
			case serviceActionDepositQuickUnlock, serviceActionQuickUnlockStatus, serviceActionQuickUnlockRecord, serviceActionClearQuickUnlock:
				if !h.ownerControlLive(controlCtx, activeCore, activeLease, activeGeneration) {
					return
				}
				if !h.handleQuickUnlock(wire, identity, minor, activeGeneration, true, message) {
					return
				}
			default:
				_ = wire.Send(protocol.ServiceMessage{
					Action: "error", Major: protocol.ServiceProtocolMajor, Minor: minor, Epoch: h.epoch,
					Generation: h.generationSnapshot(),
					ErrorCode:  "unsupported_action", Error: "控制会话不支持该操作",
				})
				return
			}
			continue
		}
		switch message.Action {
		case "status", protocol.ServiceActionRequestUnlock:
			var requestGeneration uint64
			if message.Action == protocol.ServiceActionRequestUnlock {
				if pending != nil || minor < protocol.ServiceUnlockNoticeMinor || message.Major != protocol.ServiceProtocolMajor ||
					message.Minor != minor || message.Epoch != h.epoch || message.Generation != generation {
					return
				}
				requestGeneration = message.Generation
			}
			status := h.status(minor, requestGeneration)
			if err := wire.Send(protocol.ServiceMessage{
				Action: message.Action, Major: protocol.ServiceProtocolMajor, Minor: minor, Epoch: h.epoch,
				Generation: status.Generation, Status: &status,
			}); err != nil {
				return
			}
		case serviceActionLock, serviceActionStop:
			clear(message.UnlockKey)
			clear(message.Proof)
			_ = wire.Send(protocol.ServiceMessage{
				Action: "error", Major: protocol.ServiceProtocolMajor, Minor: minor, Epoch: h.epoch,
				Generation: h.generationSnapshot(), ErrorCode: "owner_required",
				Error: "需要当前连接的有效所有者会话",
			})
			return
		case serviceActionQuickUnlockStatus, serviceActionQuickUnlockRecord, serviceActionClearQuickUnlock:
			if pending != nil || !h.handleQuickUnlock(wire, identity, minor, h.generationSnapshot(), false, message) {
				return
			}
		case serviceActionDepositQuickUnlock:
			clear(message.QuickUnlock)
			_ = wire.Send(protocol.ServiceMessage{
				Action: "error", Major: protocol.ServiceProtocolMajor, Minor: minor, Epoch: h.epoch,
				Generation: h.generationSnapshot(), ErrorCode: "owner_required",
				Error: "需要当前连接的有效所有者会话",
			})
			return
		case "prepareOwner":
			if minor < 1 || pending != nil {
				clear(message.UnlockKey)
				clear(message.Proof)
				_ = wire.Send(protocol.ServiceMessage{
					Action: "error", Major: protocol.ServiceProtocolMajor, Minor: minor, Epoch: h.epoch,
					Generation: h.generationSnapshot(),
					ErrorCode:  "unsupported_action", Error: "当前连接不支持该所有者操作",
				})
				return
			}
			prepared, prepareErr := func() (*preparedOwner, error) {
				defer clear(message.UnlockKey)
				defer clear(message.Proof)
				return h.prepareOwner(controlCtx, raw, identity, minor, challenge, binding, message)
			}()
			if prepareErr != nil {
				_ = wire.Send(protocol.ServiceMessage{
					Action: "error", Major: protocol.ServiceProtocolMajor, Minor: minor, Epoch: h.epoch,
					Generation: h.generationSnapshot(),
					ErrorCode:  "invalid_owner", Error: "所有者认证失败",
				})
				return
			}
			pending = prepared
			pendingTimer = time.AfterFunc(time.Until(pending.expiresAt), func() { _ = raw.Close() })
			if err := wire.Send(protocol.ServiceMessage{
				Action: "ownerPrepared", Major: protocol.ServiceProtocolMajor, Minor: minor, Epoch: h.epoch,
				Generation: pending.generation, ID: pending.id,
			}); err != nil {
				return
			}
		case "commitOwner":
			if minor < 1 || pending == nil {
				_ = wire.Send(protocol.ServiceMessage{
					Action: "error", Major: protocol.ServiceProtocolMajor, Minor: minor, Epoch: h.epoch,
					Generation: h.generationSnapshot(),
					ErrorCode:  "invalid_owner", Error: "所有者提交失败",
				})
				return
			}
			committedCore, committedLease, ownerInfo, commitErr := h.commitOwner(pending, pendingTimer, controlCtx, identity, minor, message)
			if commitErr != nil {
				_ = wire.Send(protocol.ServiceMessage{
					Action: "error", Major: protocol.ServiceProtocolMajor, Minor: minor, Epoch: h.epoch,
					Generation: h.generationSnapshot(),
					ErrorCode:  "invalid_owner", Error: "所有者提交失败",
				})
				return
			}
			activeCore = committedCore
			activeLease = committedLease
			activeGeneration = pending.generation
			pending = nil
			pendingTimer = nil
			watcher = newControlCloseWatcher(raw, activeLease.Done(), controlCtx.Done())
			if err := connection.SetDeadline(time.Now().Add(serviceControlDeadline)); err != nil {
				return
			}
			if err := wire.Send(protocol.ServiceMessage{
				Action: "ownerCommitted", Major: protocol.ServiceProtocolMajor, Minor: minor,
				Epoch: h.epoch, Generation: activeGeneration, OwnerInfo: &ownerInfo,
			}); err != nil {
				return
			}
		default:
			_ = wire.Send(protocol.ServiceMessage{
				Action: "error", Major: protocol.ServiceProtocolMajor, Minor: minor, Epoch: h.epoch,
				Generation: h.generationSnapshot(),
				ErrorCode:  "unsupported_action", Error: "服务不支持该操作",
			})
			return
		}
	}
}

func negotiateServiceVersion(message protocol.ServiceMessage) (int, bool) {
	if message.Major != protocol.ServiceProtocolMajor || message.Minor < 0 {
		return 0, false
	}
	minor := message.Minor
	if minor > protocol.ServiceProtocolMinor {
		minor = protocol.ServiceProtocolMinor
	}
	return minor, true
}
