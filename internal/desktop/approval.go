package desktop

import (
	"context"
	"time"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"
	"sshclient/internal/approval"
	"sshclient/internal/desktop/components"
	"sshclient/internal/systemauth"
)

type approvalPromptState struct {
	password, status      string
	busy, submitting      bool
	passwordMode          bool
	cancel                context.CancelFunc
	sequence              uint64
	requestKey            string
	closed, autoAttempted bool
	autoQueued            bool
	flow                  *approvalAuthFlow
	host                  *mygo.Window
	presentation          *systemauth.EmbeddedPresentation
	systemAttempt         bool
}

const approvalWindowHeight = 540

// Authentication stays below the scroll area so long commands cannot hide the
// decision. Rendering this view never starts system authentication.
func (a *app) approvalPrompt(c *ui.Context, r approval.Request, state *approvalPromptState, decide func(bool, bool), close func()) {
	components.ApplyTheme(c, false)
	applicant, note := approvalApplicant(r)
	components.ConsentPanel(c, components.ConsentOptions{Icon: appIcon, Title: compactApprovalTitle(r), Applicant: applicant, Note: note},
		func() { a.compactApprovalReview(c, r) },
		func() { a.approvalActions(c, r, state, decide, close) })
}

func approvalConsentLabel(r approval.Request) string {
	if r.Renewal {
		return tr(nil, "允许续期")
	}
	return tr(nil, "允许")
}

func (a *app) approvalSystemLabel(c *ui.Context) string {
	if a.authStatus.Name == "" || a.authStatus.Name == "Touch ID" {
		return tr(c, "触控 ID")
	}
	return a.authStatus.Name
}

func (a *app) approvalSystemReady() bool {
	if !a.authStatus.Enabled || !a.authStatus.Available {
		return false
	}
	// An embedded biometric view cannot display the macOS login-password
	// fallback. Without Touch ID the app uses its own vault-password field.
	return a.auth == nil || !a.auth.SupportsEmbedded() || a.authStatus.Name == "Touch ID"
}

func (a *app) approvalActions(c *ui.Context, r approval.Request, state *approvalPromptState, decide func(bool, bool), close func()) {
	systemReady := a.approvalSystemReady()
	usePassword := state.passwordMode || !systemReady
	if state.status != "" {
		ui.Scroll(c).FillWidth().MaxHeight(54).Children(func() {
			ui.Text(c, reviewText(state.status)).FillWidth().FontSize(12).TextColor(c.Theme().Danger).Label(tr(c, "授权错误"))
		})
	}
	submitted, systemClicked := false, false
	nativeIcon := state.presentation != nil && state.busy && state.systemAttempt && !state.submitting
	var anchor ui.Element
	if usePassword {
		input, action := components.AuthenticationPasswordWithAnchor(c, &state.password, tr(c, "保险库主密码"), a.approvalSystemLabel(c), systemReady, state.busy && !nativeIcon, nativeIcon)
		input.Placeholder(tr(c, "输入保险库主密码")).AutoFocus()
		action.Disabled(state.busy)
		submitted, systemClicked = input.Submitted(), systemReady && action.Clicked()
		if systemReady {
			anchor = action
		}
		if nativeIcon && (input.Changed() || submitted) {
			// Typing is itself the choice of password authentication. It must
			// not require cancelling Touch ID in a separate step first.
			password := state.password
			state.stopAttempt()
			state.password = password
		}
		if input.Changed() {
			state.status = ""
		}
	} else {
		label := tr(c, "授权方式：") + a.approvalSystemLabel(c)
		method := components.Button(c, "", components.ButtonOptions{Variant: components.Quiet, Disabled: state.submitting}).Label(label).
			FillWidth().Height(36).AutoFocus().Children(func() {
			icon := ui.Box(c).Key("approval-biometric-icon").Size(32, 32).Center()
			anchor = icon
			icon.Children(func() {
				if nativeIcon {
					return
				} else if state.busy && !state.submitting {
					ui.Spinner(c).Size(16, 16)
				} else {
					ui.Icon(c, components.FingerprintIcon).Size(18, 18)
				}
			})
			ui.Text(c, label).SingleLine()
		})
		if method.Clicked() {
			state.expandPassword()
		}
	}
	if state.presentation != nil {
		state.placeAuthentication(anchor.Bounds())
		// Bounds belong to the committed frame. Schedule the initial anchor
		// and one follow-up frame when layout changes during authentication.
		if !state.presentation.Ready() || nativeIcon {
			c.After(33 * time.Millisecond)
		}
	}
	ui.Row(c).FillWidth().Gap(8).Children(func() {
		if components.Button(c, tr(c, "不允许")).Grow(1).Height(36).Radius(8).Disabled(state.submitting).Clicked() {
			if state.busy {
				state.stopAttempt()
			}
			decide(false, false)
		}
		if usePassword {
			label := approvalConsentLabel(r)
			if state.submitting {
				label = tr(c, "正在提交…")
			}
			if components.Button(c, label, components.ButtonOptions{Variant: components.Primary, Busy: state.submitting}).Grow(1).Height(36).Radius(8).
				Disabled(state.busy || state.password == "").Clicked() || !state.busy && state.password != "" && submitted {
				decide(true, false)
			}
		} else if components.Button(c, tr(c, "取消")).Grow(1).Height(36).Radius(8).Disabled(state.submitting).Clicked() {
			state.stopAttempt()
			close()
		}
	})
	if systemClicked {
		// A direct retry keeps the password field expanded, including after a
		// failure or cancellation of the biometric attempt.
		state.passwordMode, state.autoAttempted = true, true
		decide(true, true)
	}
	if c.Shortcut(0, ui.KeyEscape) || c.Shortcut(ui.Cmd, ui.KeyPeriod) {
		if state.submitting {
			return
		}
		if state.busy {
			state.stopAttempt()
		} else {
			state.password = ""
			close()
		}
	}
}

// The broker still verifies a fresh proof for the exact reviewed request.
func (a *app) showApproval(r approval.Request) {
	if !a.startup.mayPopup() {
		return
	}
	a.clearInlineApproval()
	state := &approvalPromptState{}
	var win *mygo.Window
	ready := false
	current := func() bool {
		return !state.closed && a.popupApproval == state && a.approvalWin == win && win != nil && !win.IsDestroyed() && a.pendingApproval(r)
	}
	queue := func() {
		if win == nil {
			return
		}
		state.queueAutomatic(r, ready, win.IsVisible(), win.IsFocused(), a.approvalSystemReady(), win.Update, func() bool {
			return current() && win.IsVisible() && win.IsFocused() && a.approvalSystemReady()
		})
	}
	win = mygo.NewWindow(mygo.WindowOptions{Title: tr(nil, "访问授权"), Width: 420, Height: approvalWindowHeight, UseContentSize: true, MinWidth: 380, MinHeight: 540,
		Parent: a.win, DisableMinimize: true, DisableMaximize: true, DisableFullScreen: true, Content: ui.View(func(c *ui.Context) {
			a.approvalPrompt(c, r, state, func(allow, system bool) { state.start(r, allow, system) }, func() { win.Close() })
			queue()
		})})
	state.host = win
	a.approvalWin, a.popupApproval = win, state
	a.approvalID, a.approvalNonce = r.ID, r.Nonce
	a.bindApprovalFlow(r, state, win.Update, current, win.Close)
	win.OnReadyToShow(func() { ready = true; queue() })
	win.OnFocus(queue)
	win.OnShow(queue)
	win.OnHide(func() { state.stopAttempt() })
	win.OnClosed(func() {
		state.dispose()
		if a.popupApproval == state {
			a.popupApproval = nil
		}
		if a.approvalWin == win {
			a.approvalWin, a.popupApproval = nil, nil
			a.approvalID, a.approvalNonce = "", ""
		}
	})
}
