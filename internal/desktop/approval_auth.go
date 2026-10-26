package desktop

import (
	"context"
	"errors"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"
	"sshclient/internal/approval"
	"sshclient/internal/broker"
	"sshclient/internal/systemauth"
	"sshclient/internal/vault"
)

// Authentication opens an owner session; its result must return through the UI
// gate before an approval proof can be sent for this reviewed generation.
type approvalAuthFlow struct {
	context      context.Context
	post         func(func())
	current      func() bool
	authenticate func(context.Context, []byte, bool) (*vault.Session, error)
	denyOwner    func() (*vault.Session, error)
	submit       func(context.Context, approval.Request, bool, *vault.Session) error
	done         func()
	claimAuto    func() bool
}

func approvalRequestKey(r approval.Request) string { return r.ID + "\x00" + r.Nonce }

func (s *approvalPromptState) stopAttempt() {
	// Invalidate before cancellation: authenticators may still return success.
	s.sequence++
	cancel := s.cancel
	s.cancel = nil
	s.busy, s.submitting, s.systemAttempt = false, false, false
	s.password = ""
	if cancel != nil {
		cancel()
	}
	if s.presentation != nil {
		// Release the old native token before another click can retry. Merely
		// cancelling its context would leave a short asynchronous overlap.
		s.presentation.Cancel()
	}
}

func (s *approvalPromptState) dispose() {
	s.closed = true
	s.stopAttempt()
	if s.presentation != nil {
		s.presentation.Close()
	}
}

func (s *approvalPromptState) placeAuthentication(bounds ui.Rect) {
	if s.presentation == nil {
		return
	}
	if s.host == nil || s.host.IsDestroyed() || !s.host.IsVisible() || bounds.W <= 0 || bounds.H <= 0 {
		s.presentation.SetFrame(0, systemauth.EmbeddedRect{})
		return
	}
	s.presentation.SetFrame(s.host.NativeHandle(), systemauth.EmbeddedRect{X: float64(bounds.X), Y: float64(bounds.Y), Width: float64(bounds.W), Height: float64(bounds.H)})
}

func (s *approvalPromptState) expandPassword() {
	if s.submitting {
		return
	}
	s.stopAttempt()
	s.passwordMode, s.autoAttempted = true, true
	s.status = ""
}

func (s *approvalPromptState) start(r approval.Request, allow, system bool) {
	f := s.flow
	if f == nil || s.closed || s.busy || s.requestKey != approvalRequestKey(r) || f.context.Err() != nil || !f.current() || !allow && f.denyOwner == nil || allow && !system && s.password == "" {
		return
	}
	s.sequence++
	seq := s.sequence
	key := approvalRequestKey(r)
	ctx, cancel := context.WithCancel(f.context)
	s.cancel, s.busy, s.status, s.systemAttempt = cancel, true, "", system
	pass := []byte(s.password)
	s.password = ""
	valid := func() bool {
		return !s.closed && s.sequence == seq && s.requestKey == key && ctx.Err() == nil && f.current()
	}
	finish := func(err error) {
		if !valid() {
			cancel()
			return
		}
		s.busy, s.submitting, s.systemAttempt, s.cancel = false, false, false, nil
		cancel()
		if err != nil {
			s.status = err.Error()
			return
		}
		f.done()
	}
	if !allow {
		wipe(pass)
		owner, err := f.denyOwner()
		if err != nil {
			finish(err)
			return
		}
		s.submitting = true
		go func() {
			err := f.submit(ctx, r, false, owner)
			f.post(func() { finish(err) })
		}()
		return
	}
	go func() {
		defer wipe(pass)
		owner, err := f.authenticate(ctx, pass, system)
		f.post(func() {
			if !valid() {
				cancel()
				if owner != nil {
					owner.Close()
				}
				return
			}
			if err != nil || owner == nil {
				if owner != nil {
					owner.Close()
				}
				if err == nil {
					err = errors.New(tr(nil, "系统验证不可用"))
				}
				finish(err)
				return
			}
			// After this final gate the decision may be sent. Method switching
			// stays disabled; cancelling an RPC cannot retract an approval.
			s.submitting = true
			go func() {
				defer owner.Close()
				err := f.submit(ctx, r, true, owner)
				f.post(func() { finish(err) })
			}()
		})
	}()
}

func (a *app) bindApprovalFlow(r approval.Request, state *approvalPromptState, post func(func()), current func() bool, done func()) {
	state.requestKey = approvalRequestKey(r)
	auth, dir, cli := a.auth, a.dir, a.client
	if state.host != nil && auth != nil && auth.SupportsEmbedded() {
		state.presentation = systemauth.NewEmbeddedPresentation(mygo.RunOnMain)
	}
	presentation := state.presentation
	state.flow = &approvalAuthFlow{
		context: a.operationContext(), post: post, current: current, done: done,
		claimAuto: func() bool {
			key := approvalRequestKey(r)
			if a.approvalAutoAttempted[key] {
				return false
			}
			if a.approvalAutoAttempted == nil {
				a.approvalAutoAttempted = make(map[string]bool)
			}
			a.approvalAutoAttempted[key] = true
			return true
		},
		authenticate: func(ctx context.Context, pass []byte, system bool) (*vault.Session, error) {
			if system {
				if auth == nil {
					return nil, errors.New(tr(nil, "系统验证不可用"))
				}
				if presentation != nil {
					return auth.OpenEmbedded(ctx, dir, approvalAuthReason(r), presentation)
				}
				return auth.Open(ctx, dir, approvalAuthReason(r))
			}
			return vault.Read(broker.VaultPath(dir), pass)
		},
		denyOwner: a.unlockedOwner,
		submit: func(ctx context.Context, reviewed approval.Request, allow bool, owner *vault.Session) error {
			return cli.DecideReviewedWithOwner(ctx, reviewed, allow, false, owner)
		},
	}
}

// Only native lifecycle code supplies readiness and visibility. Offline
// rendering has no window and never queues an OS authentication dialog.
func (s *approvalPromptState) queueAutomatic(r approval.Request, ready, visible, focused, systemReady bool, post func(func()), current func() bool) {
	if !ready || !visible || !focused || !systemReady || s.closed || s.passwordMode || s.busy || s.autoAttempted || s.autoQueued || !current() || s.presentation != nil && !s.presentation.Ready() {
		return
	}
	s.autoQueued = true
	post(func() {
		s.autoQueued = false
		if s.closed || s.passwordMode || s.busy || s.autoAttempted || !current() || s.presentation != nil && !s.presentation.Ready() {
			return
		}
		s.autoAttempted = true
		if s.flow != nil && s.flow.claimAuto != nil && !s.flow.claimAuto() {
			return
		}
		s.start(r, true, true)
	})
}

func (a *app) pendingApproval(r approval.Request) bool {
	for _, current := range a.requests {
		if current.ID == r.ID && current.Nonce == r.Nonce && current.Status == "pending" {
			return true
		}
	}
	return false
}

func (a *app) clearInlineApproval() {
	if a.inlineApproval != nil {
		a.inlineApproval.dispose()
		a.inlineApproval = nil
	}
	a.ownerPass, a.approvalPasswordMode = "", false
	a.approvalReviewKey = ""
}

func (a *app) queueInlineApproval() {
	state, win := a.inlineApproval, a.win
	if state == nil || win == nil || state.flow == nil {
		return
	}
	if !a.online || a.page != 3 || a.approvalTab != 0 || a.approvalWin != nil || !state.flow.current() {
		a.clearInlineApproval()
		return
	}
	for _, r := range a.requests {
		if approvalRequestKey(r) == state.requestKey {
			state.queueAutomatic(r, a.mainWindowReady, win.IsVisible(), win.IsFocused(), a.approvalSystemReady(), win.Update, func() bool {
				return a.inlineApproval == state && state.flow.current() && !win.IsDestroyed() && win.IsVisible() && win.IsFocused() && a.approvalSystemReady()
			})
			return
		}
	}
}

func (a *app) closeApprovalAuthentication() {
	a.clearInlineApproval()
	if a.popupApproval != nil {
		a.popupApproval.dispose()
		a.popupApproval = nil
	}
}

func (a *app) hideApprovalAuthentication() {
	a.clearInlineApproval()
	if a.popupApproval != nil {
		a.popupApproval.stopAttempt()
	}
}
