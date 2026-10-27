package desktop

import (
	"context"
	"errors"
	"sync"
	"time"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"
	"sshclient/internal/broker"
	"sshclient/internal/systemauth"
	"sshclient/internal/vault"
)

type unlockState struct {
	passwordMode, busy, systemAttempt bool
	autoAttempted, autoQueued, closed bool
	autoRetryUsed                     bool
	autoRetryAt                       time.Time
	sequence                          uint64
	cancel                            context.CancelFunc
	presentation                      *systemauth.EmbeddedPresentation
	anchor                            anchorTracker
	attempt                           *unlockAttempt
	// Isolated tests substitute authentication and broker creation, never an
	// authentication-success flag. Both paths still transfer a real vault.
	authenticate func(context.Context, []byte, bool) (*vault.Session, error)
	openBroker   func(string, *vault.Session) (*broker.Server, error)
}

// An authenticated vault remains revocable until the final UI handoff. The
// background broker worker owns cleanup and waits for this single decision.
type unlockAttempt struct {
	mu                  sync.Mutex
	owner               *vault.Session
	cancelled, accepted bool
	decision            chan bool
}

func newUnlockAttempt() *unlockAttempt { return &unlockAttempt{decision: make(chan bool, 1)} }

func (u *unlockAttempt) adopt(owner *vault.Session) bool {
	u.mu.Lock()
	defer u.mu.Unlock()
	if u.cancelled {
		owner.Close()
		return false
	}
	u.owner = owner
	return true
}

func (u *unlockAttempt) reject() {
	u.mu.Lock()
	defer u.mu.Unlock()
	if u.accepted || u.cancelled {
		return
	}
	u.cancelled = true
	if u.owner != nil {
		u.owner.Close()
	}
	u.decision <- false
}

func (u *unlockAttempt) accept() bool {
	u.mu.Lock()
	defer u.mu.Unlock()
	if u.cancelled || u.accepted {
		return false
	}
	u.accepted = true
	u.owner = nil
	u.decision <- true
	return true
}

func (a *app) ensureUnlockState() *unlockState {
	if a.unlockState == nil || a.unlockState.closed {
		a.unlockState = &unlockState{}
	}
	s := a.unlockState
	if s.presentation == nil && a.win != nil && a.auth != nil && a.auth.SupportsEmbedded() {
		s.presentation = systemauth.NewEmbeddedPresentation(mygo.RunOnMain)
	}
	return s
}

func (a *app) unlockSystemReady() bool {
	return !a.create && a.approvalSystemReady()
}

func (a *app) cancelUnlockAttempt() {
	s := a.unlockState
	if s == nil {
		return
	}
	s.sequence++
	if s.attempt != nil {
		s.attempt.reject() // Synchronously revoke a vault already handed to OpenSession.
	}
	if s.cancel != nil {
		s.cancel()
		s.cancel = nil
	}
	if s.presentation != nil {
		s.presentation.Cancel()
	}
	if s.busy {
		a.busy = false
	}
	s.busy, s.systemAttempt = false, false
	a.pass = ""
}

func (a *app) disposeUnlockState() {
	a.cancelUnlockAttempt()
	if s := a.unlockState; s != nil {
		s.closed = true
		if s.presentation != nil {
			s.presentation.Close()
		}
	}
	a.unlockState = nil
}

func (a *app) expandUnlockPassword() {
	a.cancelUnlockAttempt()
	s := a.ensureUnlockState()
	s.passwordMode, s.autoAttempted = true, true
	a.unlockError = ""
}

func (a *app) placeUnlockAuthentication(bounds ui.Rect) {
	placeAuthenticationFrame(a.win, a.ensureUnlockState().presentation, bounds)
}

// readyForAutomaticUnlock keeps the landing page's own state out of the shared
// readiness check: a page already in password mode or already trying must not
// start another authentication.
func (s *unlockState) readyForAutomaticUnlock(now time.Time, r authReadiness) bool {
	return !s.closed && !s.busy && !s.passwordMode &&
		r.ready(now, s.autoAttempted, s.autoRetryAt, s.presentation)
}

func (s *unlockState) queueAutomaticUnlock(r authReadiness, now func() time.Time, post func(func()), current func() (authReadiness, bool), start func()) {
	if s.autoQueued || !s.readyForAutomaticUnlock(now(), r) {
		return
	}
	s.autoQueued = true
	post(func() {
		s.autoQueued = false
		r, valid := current()
		if !valid || !s.readyForAutomaticUnlock(now(), r) {
			return
		}
		s.autoAttempted, s.autoRetryAt = true, time.Time{}
		start()
	})
}

func (s *unlockState) retryAutomaticUnlock(err error, automatic bool, now time.Time) bool {
	at, retry := retryAutomatic(err, automatic, s.autoRetryUsed, now)
	if !retry {
		return false
	}
	// Retry only a system readiness interruption, once. A rejected fingerprint
	// or an explicit cancellation must never trigger another authentication.
	s.autoRetryUsed, s.autoAttempted, s.autoRetryAt = true, false, at
	return true
}

func (a *app) queueUnlockAuthentication() {
	s, win := a.unlockState, a.win
	if s == nil || win == nil || win.IsDestroyed() || a.online || a.create || a.busy {
		return
	}
	readiness := func() authReadiness {
		return authReadiness{windowReady: a.mainWindowReady, visible: win.IsVisible(), focused: win.IsFocused(), active: a.isActive(), systemReady: a.unlockSystemReady()}
	}
	s.queueAutomaticUnlock(readiness(), time.Now, win.Update, func() (authReadiness, bool) {
		return readiness(), a.unlockState == s && !win.IsDestroyed() && !a.online && !a.create && !a.busy
	}, func() { a.beginVaultUnlockAttempt(true, true) })
}

func (a *app) systemUnlock() { a.beginVaultUnlock(true) }

func (a *app) beginVaultUnlock(system bool) {
	a.beginVaultUnlockAttempt(system, false)
}

func (a *app) beginVaultUnlockAttempt(system, automatic bool) {
	if a.online || a.create || a.busy {
		return
	}
	s := a.ensureUnlockState()
	if system {
		if !a.unlockSystemReady() || a.auth == nil && s.authenticate == nil || s.presentation != nil && !s.presentation.Ready() {
			return
		}
	} else if a.pass == "" {
		return
	}
	base := a.operationContext()
	if base.Err() != nil {
		return
	}
	ctx, cancel := context.WithCancel(base)
	s.sequence++
	seq, gen := s.sequence, a.generation
	u := newUnlockAttempt()
	stopCancellation := context.AfterFunc(ctx, u.reject)
	s.cancel, s.attempt, s.busy, s.systemAttempt = cancel, u, true, system
	a.busy, a.unlockError = true, ""
	pass, dir, auth, presentation := []byte(a.pass), a.dir, a.auth, s.presentation
	a.pass = ""
	authenticate := s.authenticate
	if authenticate == nil {
		authenticate = func(ctx context.Context, pass []byte, system bool) (*vault.Session, error) {
			if system {
				if auth == nil {
					return nil, errors.New(tr(nil, "系统验证不可用"))
				}
				if presentation != nil {
					return auth.OpenEmbedded(ctx, dir, tr(nil, "解锁 SmallSSH 保险库"), presentation)
				}
				return auth.Open(ctx, dir, tr(nil, "解锁 SmallSSH 保险库"))
			}
			return vault.Read(broker.VaultPath(dir), pass)
		}
	}
	openBroker := s.openBroker
	if openBroker == nil {
		openBroker = broker.OpenSession
	}
	valid := func() bool {
		return a.unlockState == s && !s.closed && s.sequence == seq && a.generation == gen && !a.online && !a.create && ctx.Err() == nil &&
			(a.win == nil || !a.win.IsDestroyed() && a.win.IsVisible())
	}
	finishError := func(err error) {
		u.reject()
		stopCancellation()
		if !valid() {
			cancel()
			return
		}
		s.busy, s.systemAttempt, s.cancel = false, false, nil
		a.busy = false
		if system && systemauth.IsAuthenticationCancelled(err) {
			a.status, a.unlockError = "", ""
		} else if system && s.retryAutomaticUnlock(err, automatic, time.Now()) {
			a.status, a.unlockError = "", ""
		} else {
			// A temporary system interruption does not mean the device is
			// unavailable, and does not choose password mode for the user.
			if system && !systemauth.IsAuthenticationNotReady(err) {
				s.passwordMode = true
			}
			a.status, a.unlockError = err.Error(), err.Error()
		}
		cancel()
	}
	go func() {
		defer wipe(pass)
		owner, err := authenticate(ctx, pass, system)
		if err != nil || owner == nil {
			if owner != nil {
				owner.Close()
			}
			if err == nil {
				err = errors.New(tr(nil, "系统验证不可用"))
			}
			a.postUI(func() { finishError(err) })
			return
		}
		if !u.adopt(owner) || ctx.Err() != nil {
			u.reject()
			return
		}
		a.postUI(func() {
			if !valid() {
				u.reject()
				cancel()
				return
			}
			// Serialize only broker startup/cleanup. A cancelled old startup
			// must release its directory lock before a retry can acquire it.
			previous := a.unlockCleanup
			done := make(chan struct{})
			a.unlockCleanup = done
			go func() {
				defer close(done)
				defer stopCancellation()
				if previous != nil {
					// Even a cancelled waiter must preserve this barrier for
					// subsequent retries. Its vault is revoked immediately by
					// cancellation while the old directory lock drains.
					<-previous
				}
				if ctx.Err() != nil {
					u.reject()
					return
				}
				server, err := openBroker(dir, owner) // Takes ownership, even on failure.
				if err != nil {
					a.postUI(func() { finishError(err) })
					return
				}
				if ctx.Err() != nil {
					u.reject()
				} else {
					a.postUI(func() {
						if !valid() || !u.accept() {
							u.reject()
							return
						}
						a.server, a.online, a.page, a.create = server, true, 1, false
						a.generation++
						a.epoch.Add(1)
						a.startUnlockedSession()
						a.status, a.unlockError, a.busy = "", "", false
						s.busy = false
						a.disposeUnlockState() // Accepted owner now belongs to a.server.
						a.initializeWorkspace()
					})
				}
				if !<-u.decision {
					server.Vault.Close()
					server.Close()
				}
			}()
		})
	}()
}
