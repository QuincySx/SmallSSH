package service

import (
	"crypto/subtle"
	"time"

	"sshclient/internal/localipc"
	"sshclient/internal/protocol"
)

// Quick unlock keeps one system-verification envelope in this process's memory
// only: the vault key as the desktop wrapped it with its OS-protected key
// (Secure Enclave, Windows Hello / DPAPI, Linux keyring). The service never
// sees that key or the vault key inside. The envelope survives locks, so the
// desktop can reopen the vault with a fresh system verification, and is gone
// when the service exits, so a restart or update needs the master password.
//
// Only an owner session — opened with the master password — may deposit, and
// the envelope is bound to the kernel-reported code identity of the depositing
// executable. Any other program, including this installation's CLI or a
// replaced desktop binary, is refused. Nothing is ever written to disk.
const (
	quickUnlockMaxAge  = 48 * time.Hour
	quickUnlockMaxSize = 64 << 10

	serviceActionDepositQuickUnlock = "depositQuickUnlock"
	serviceActionQuickUnlockStatus  = "quickUnlockStatus"
	serviceActionQuickUnlockRecord  = "quickUnlockRecord"
	serviceActionClearQuickUnlock   = "clearQuickUnlock"

	serviceActionQuickUnlockDeposited = "quickUnlockDeposited"
	serviceActionQuickUnlockCleared   = "quickUnlockCleared"
)

type quickUnlock struct {
	codeID  string
	record  []byte
	expires time.Time
}

func (q *quickUnlock) wipe() {
	clear(q.record)
	*q = quickUnlock{}
}

func isQuickUnlockAction(action string) bool {
	switch action {
	case serviceActionDepositQuickUnlock, serviceActionQuickUnlockStatus, serviceActionQuickUnlockRecord, serviceActionClearQuickUnlock:
		return true
	}
	return false
}

// matches reports whether the live caller is the executable that deposited the
// envelope and the envelope is still within its age limit. The caller holds h.mu.
func (q *quickUnlock) matches(identity *localipc.ProcessIdentity, now time.Time) bool {
	if q.codeID == "" || len(q.record) == 0 || !now.Before(q.expires) || identity == nil || identity.CodeID == "" {
		return false
	}
	if subtle.ConstantTimeCompare([]byte(q.codeID), []byte(identity.CodeID)) != 1 {
		return false
	}
	return identity.Verify() == nil
}

// handleQuickUnlock answers one quick-unlock request. owner is true on a
// committed owner control connection, the only place a deposit is accepted.
// It returns false when the connection must close.
func (h *Host) handleQuickUnlock(wire *localipc.Wire, identity *localipc.ProcessIdentity, minor int, generation uint64, owner bool, message protocol.ServiceMessage) bool {
	defer clear(message.QuickUnlock)
	refuse := func(code, text string) bool {
		// On an owner control connection a stray "error" action would end the
		// owner session; answer under the expected action and keep it open.
		if owner && message.Action == serviceActionDepositQuickUnlock && code != "invalid_identity" {
			return wire.Send(protocol.ServiceMessage{
				Action: serviceActionQuickUnlockDeposited, Major: protocol.ServiceProtocolMajor, Minor: minor, Epoch: h.epoch,
				Generation: generation, ErrorCode: code, Error: text, QuickUnlockState: &protocol.QuickUnlockState{},
			}) == nil
		}
		_ = wire.Send(protocol.ServiceMessage{
			Action: "error", Major: protocol.ServiceProtocolMajor, Minor: minor, Epoch: h.epoch,
			Generation: generation, ErrorCode: code, Error: text,
		})
		return false
	}
	if minor < 2 {
		return refuse("unsupported_action", "当前连接不支持快速解锁")
	}
	if identity == nil || identity.Verify() != nil {
		return refuse("invalid_identity", "无法验证调用进程")
	}
	now := time.Now()
	reply := protocol.ServiceMessage{Major: protocol.ServiceProtocolMajor, Minor: minor, Epoch: h.epoch, Generation: generation}
	switch message.Action {
	case serviceActionDepositQuickUnlock:
		if !owner {
			return refuse("owner_required", "需要当前连接的有效所有者会话")
		}
		if len(message.QuickUnlock) == 0 || len(message.QuickUnlock) > quickUnlockMaxSize {
			return refuse("invalid_quick_unlock", "快速解锁数据无效")
		}
		h.mu.Lock()
		h.quick.wipe()
		h.quick = quickUnlock{codeID: identity.CodeID, record: append([]byte(nil), message.QuickUnlock...), expires: now.Add(quickUnlockMaxAge)}
		state := protocol.QuickUnlockState{Available: true, Expires: h.quick.expires}
		h.mu.Unlock()
		reply.Action, reply.QuickUnlockState = serviceActionQuickUnlockDeposited, &state
	case serviceActionQuickUnlockStatus:
		h.mu.Lock()
		state := protocol.QuickUnlockState{}
		if h.quick.matches(identity, now) {
			state = protocol.QuickUnlockState{Available: true, Expires: h.quick.expires}
		}
		h.mu.Unlock()
		reply.Action, reply.QuickUnlockState = serviceActionQuickUnlockStatus, &state
	case serviceActionQuickUnlockRecord:
		h.mu.Lock()
		if !h.quick.matches(identity, now) {
			if !now.Before(h.quick.expires) {
				h.quick.wipe()
			}
			h.mu.Unlock()
			return refuse("quick_unlock_unavailable", "快速解锁不可用，请输入主密码")
		}
		reply.QuickUnlock = append([]byte(nil), h.quick.record...)
		state := protocol.QuickUnlockState{Available: true, Expires: h.quick.expires}
		h.mu.Unlock()
		reply.Action, reply.QuickUnlockState = serviceActionQuickUnlockRecord, &state
	case serviceActionClearQuickUnlock:
		// Clearing only reduces authority: an owner, or the depositor itself
		// while locked, may do it.
		h.mu.Lock()
		if owner || h.quick.matches(identity, now) {
			h.quick.wipe()
		}
		h.mu.Unlock()
		reply.Action, reply.QuickUnlockState = serviceActionQuickUnlockCleared, &protocol.QuickUnlockState{}
	default:
		return refuse("unsupported_action", "不支持该快速解锁操作")
	}
	err := wire.Send(reply)
	clear(reply.QuickUnlock)
	return err == nil
}
