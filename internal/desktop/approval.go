package desktop

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"
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
	anchor                anchorTracker
	autoRetryUsed         bool
	autoRetryAt           time.Time
	systemAttempt         bool
	// lifetime is the owner-chosen session duration in seconds. A caller only
	// suggests one; the owner decides before approving.
	lifetime    int
	idleSeconds int
	grantKey    string
	// noIdle keeps the grant alive for its whole lifetime: the owner unticks
	// the idle disconnect.
	noIdle bool
	// showPassword reveals the vault password while the owner types it.
	showPassword bool
}

// grantLifetimeOptions are the durations the owner can hand out. The requester
// cannot pick one of its own.
func grantLifetimeOptions() []int {
	return []int{15 * 60, 30 * 60, 60 * 60, 2 * 60 * 60, 3 * 60 * 60,
		6 * 60 * 60, 9 * 60 * 60, 12 * 60 * 60, 24 * 60 * 60}
}

// grantLifetimeChipLabel keeps five durations inside one row of the popup.
func grantLifetimeChipLabel(seconds int) string {
	if seconds < 3600 {
		return fmt.Sprintf(tr(nil, "%d 分"), seconds/60)
	}
	return fmt.Sprintf(tr(nil, "%d 小时"), seconds/3600)
}

func defaultGrantLifetime(r approval.Request) int {
	requested := r.Operation.Grant.LifetimeSeconds
	for _, option := range grantLifetimeOptions() {
		if requested == option {
			return option
		}
	}
	return 6 * 60 * 60
}

func (s *approvalPromptState) chosenLifetime(r approval.Request) int {
	if s.lifetime > 0 {
		return s.lifetime
	}
	return defaultGrantLifetime(r)
}

type approvalTerms struct {
	lifetime, idleSeconds int
	noIdle                bool
}

func (s *approvalPromptState) chosenIdle(r approval.Request) int {
	lifetime := s.chosenLifetime(r)
	if s.noIdle {
		return lifetime
	}
	idle := s.idleSeconds
	if idle <= 0 {
		idle = 15 * 60
	}
	return min(idle, lifetime)
}

func (s *approvalPromptState) initializeGrant(r approval.Request) {
	s.initializeGrantWithDefault(r, 15*60)
}

func (s *approvalPromptState) initializeGrantWithDefault(r approval.Request, idle int) {
	if r.Operation.Kind != "grant" || r.Status != "pending" {
		return
	}
	key := approvalRequestKey(r)
	if s.grantKey == key {
		return
	}
	s.grantKey = key
	s.lifetime = defaultGrantLifetime(r)
	s.noIdle = idle == 0
	s.idleSeconds = idle
	if r.Renewal {
		s.lifetime = r.Operation.Grant.LifetimeSeconds
		s.noIdle = r.Operation.Grant.IdleSeconds >= s.lifetime
		if !s.noIdle {
			s.idleSeconds = r.Operation.Grant.IdleSeconds
		} else {
			s.idleSeconds = 15 * 60
		}
	}
	if s.idleSeconds <= 0 {
		s.idleSeconds = 15 * 60
	}
}

func (a *app) initializeApprovalGrant(r approval.Request, s *approvalPromptState) {
	if r.Operation.Kind != "grant" || r.Status != "pending" || s.grantKey == approvalRequestKey(r) {
		return
	}
	s.initializeGrantWithDefault(r, a.sessionIdleSeconds())
	if s.noIdle && !r.Renewal {
		s.idleSeconds = behaviorMinutes(a.behavior.SessionIdleMinutes, true) * 60
	}
	if terms, ok := a.approvalTerms[approvalRequestKey(r)]; ok {
		s.lifetime, s.idleSeconds, s.noIdle = terms.lifetime, terms.idleSeconds, terms.noIdle
	}
}

func (a *app) changeApprovalTerms(r approval.Request, s *approvalPromptState, change func()) {
	if s.submitting {
		return
	}
	s.stopAttempt()
	s.autoAttempted, s.autoRetryAt = true, time.Time{}
	key := approvalRequestKey(r)
	if a.approvalAutoAttempted == nil {
		a.approvalAutoAttempted = make(map[string]bool)
	}
	a.approvalAutoAttempted[key] = true
	change()
	if a.approvalTerms == nil {
		a.approvalTerms = make(map[string]approvalTerms)
	}
	a.approvalTerms[key] = approvalTerms{s.lifetime, s.idleSeconds, s.noIdle}
}

func (a *app) pruneApprovalTerms() {
	pending := make(map[string]bool)
	for _, r := range a.requests {
		if r.Status == "pending" {
			pending[approvalRequestKey(r)] = true
		}
	}
	for key := range a.approvalTerms {
		if !pending[key] {
			delete(a.approvalTerms, key)
		}
	}
	for key := range a.approvalAutoAttempted {
		if !pending[key] {
			delete(a.approvalAutoAttempted, key)
		}
	}
}

const approvalWindowHeight = 520

// Authentication stays below the scroll area so long commands cannot hide the
// decision. Rendering this view never starts system authentication.
func (a *app) approvalPrompt(c *ui.Context, r approval.Request, state *approvalPromptState, decide func(bool, bool), close func()) {
	components.ApplyTheme(c, false)
	applicant := peerName(r)
	a.initializeApprovalGrant(r, state)
	ui.Column(c).Fill().Padding(20, 22, 18).Gap(12).Children(func() {
		a.approvalHeader(c, r, applicant)
		if state.status != "" {
			ui.Text(c, reviewText(state.status)).FillWidth().Shrink(0).MaxLines(2).FontSize(11).TextColor(c.Theme().Danger)
		}
		ui.Scroll(c).FillWidth().Grow(1).MinHeight(0).Children(func() {
			ui.Column(c).FillWidth().Gap(10).Children(func() {
				a.approvalCard(c, r, state)
				if r.Operation.Kind == "grant" && r.Status == "pending" {
					a.approvalGrantChoices(c, r, state)
				}
				if r.Operation.Command != "" {
					ui.Scroll(c).FillWidth().MaxHeight(96).Children(func() {
						ui.Text(c, reviewText(r.Operation.Command)).FillWidth().Padding(8).Radius(8).
							Background(c.Theme().Surface).Border(1, c.Theme().Border).
							Font("monospace").FontSize(11).Selectable()
					})
				}
			})
		})
		a.approvalDecisionControls(c, r, state, decide, close)
	})
}

// Both approval surfaces share one password field, decision row and native anchor.
func (a *app) approvalDecisionControls(c *ui.Context, r approval.Request, state *approvalPromptState, decide func(bool, bool), close func()) {
	if delay := time.Until(state.autoRetryAt); delay > 0 {
		c.After(delay)
	}
	systemReady := a.approvalSystemReady()
	showField := state.passwordMode || !systemReady
	nativeIcon := state.presentation != nil && state.busy && state.systemAttempt && !state.submitting
	var anchor ui.Element
	hasAnchor := false
	if showField {
		_, input := components.SecretInput(c, "approval-password", &state.password, tr(c, "保险库主密码"), tr(c, "输入保险库主密码"), &state.showPassword)
		input.AutoFocus()
		if input.Changed() {
			state.status = ""
			if nativeIcon {
				password := state.password
				state.stopAttempt()
				state.password = password
			}
		}
		if (input.Submitted() || approvalEnter(c)) && state.password != "" && !state.busy && !state.submitting {
			decide(true, false)
		}
	} else if !state.busy && !state.submitting && approvalEnter(c) {
		state.expandPassword()
		showField = true
	}
	ui.Row(c).FillWidth().Shrink(0).Gap(8).AlignItems(ui.Center).Children(func() {
		if systemReady {
			method := tr(c, "授权方式：") + a.approvalSystemLabel(c)
			if components.Button(c, method, components.ButtonOptions{Variant: components.Quiet}).Height(34).FontSize(11).
				TextColor(c.Theme().TextMuted).Label(tr(c, "改用保险库主密码")).Tooltip(tr(c, "改用保险库主密码")).Clicked() {
				state.expandPassword()
				return
			}
			button := components.Button(c, "", components.ButtonOptions{Variant: components.Quiet, Disabled: state.submitting}).
				Size(40, 40).Padding(0).Radius(8).Label(a.approvalSystemLabel(c)).
				Tooltip(tr(c, "使用") + a.approvalSystemLabel(c) + tr(c, "确认"))
			button.Children(func() {
				icon := ui.Box(c).Key("approval-biometric-icon").Size(36, 36).Center()
				anchor, hasAnchor = button, true
				icon.Children(func() {
					if nativeIcon {
						return
					} else if state.busy && !state.submitting {
						ui.Spinner(c).Size(16, 16)
					} else {
						ui.Icon(c, components.SystemAuthIcon()).Size(24, 24).TextColor(c.Theme().Accent)
					}
				})
			})
			if button.Clicked() && !state.submitting {
				if state.busy {
					state.stopAttempt()
				}
				state.password = ""
				state.passwordMode = false
				decide(true, true)
			}
		}
		ui.Spacer(c).Grow(1)
		if components.Button(c, tr(c, "拒绝")).Height(34).Radius(8).Disabled(state.submitting).Clicked() {
			if state.busy {
				state.stopAttempt()
			}
			decide(false, false)
			return
		}
		label := approvalConsentLabel(r)
		if state.submitting {
			label = tr(c, "正在提交…")
		}
		allow := components.Button(c, label, components.ButtonOptions{Variant: components.Primary, Busy: state.submitting,
			Disabled: showField && (state.busy || state.password == "")}).Height(34).Radius(8)
		if allow.Clicked() {
			if showField {
				decide(true, false)
			} else {
				state.expandPassword()
			}
		}
	})
	if state.presentation != nil && hasAnchor && anchor.Bounds().W > 1 && anchor.Bounds().H > 1 {
		state.placeAuthentication(anchor.Bounds())
		if state.anchor.follow(anchor.Bounds(), state.presentation.Ready(), nativeIcon, systemReady) {
			c.After(33 * time.Millisecond)
		}
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

// peerName is the process the broker saw, which is what can be checked. The
// caller's own label is only a fallback when the platform cannot report one.
func peerName(r approval.Request) string {
	if r.Peer.Path != "" {
		name := filepath.Base(r.Peer.Path)
		name = strings.TrimSuffix(name, ".exe")
		if name != "" && name != "." && name != "/" {
			return name
		}
	}
	if r.Peer.Bundle != "" {
		return r.Peer.Bundle
	}
	applicant, _ := approvalApplicant(r)
	return applicant
}

// approvalHeader says who asks for what, in one line, without repeating it.
func (a *app) approvalHeader(c *ui.Context, r approval.Request, applicant string) {
	ui.Column(c).FillWidth().Shrink(0).AlignItems(ui.Center).Gap(8).Children(func() {
		ui.Row(c).AlignItems(ui.Center).Gap(8).Children(func() {
			ui.Image(c, appIcon).Size(40, 40).Label(tr(c, "应用图标"))
			ui.Row(c).Width(34).Height(2).Background(c.Theme().Border).AlignItems(ui.Center).Justify(ui.Center).Children(func() {
				ui.Icon(c, components.CheckCircleIcon).Size(15, 15).TextColor(c.Theme().Success)
			})
			ui.Box(c).Size(40, 40).Radius(10).Background(c.Theme().Surface).Border(1, c.Theme().Border).
				Center().Label(applicant).Children(func() {
				ui.Icon(c, components.WorkspaceIcon).Size(21, 21).TextColor(c.Theme().TextMuted)
			})
		})
		if r.Renewal {
			ui.Text(c, compactApprovalTitle(r)).FillWidth().TextAlign(ui.Center).FontSize(16).Bold()
		} else {
			ui.Text(c, applicant).FillWidth().TextAlign(ui.Center).FontSize(16).Bold()
		}
		ui.Text(c, tr(c, "请求访问你的服务器")).FillWidth().TextAlign(ui.Center).FontSize(12).
			TextColor(c.Theme().TextMuted)
	})
}

// approvalCard holds the request facts behind one chevron.
func (a *app) approvalCard(c *ui.Context, r approval.Request, state *approvalPromptState) {
	expanded := ui.Local(c.Root(), "approval-card", func() bool { return false })
	op := r.Operation
	permission, _, caution := approvalPermission(r)
	ui.Column(c).FillWidth().Gap(8).Padding(10).Radius(10).
		Background(c.Theme().Surface).Border(1, c.Theme().Border).Children(func() {
		ui.Row(c).FillWidth().Gap(2).AlignItems(ui.Center).Children(func() {
			chevron := components.SVGButton(c, components.ChevronRightIcon, tr(c, "详情")).Size(22, 22)
			if *expanded {
				chevron.Rotate(90)
			}
			if chevron.Clicked() {
				*expanded = !*expanded
				c.Invalidate()
			}
			label := components.Button(c, "", components.ButtonOptions{Variant: components.Quiet}).Grow(1).Height(24).
				Padding(0).Justify(ui.Start).Label(permission)
			label.Children(func() {
				color := c.Theme().Text
				if caution != "" && !(op.Kind == "grant" && op.Grant.Permission == "read") {
					color = c.Theme().Warning
				}
				ui.Text(c, permission).FontSize(12).Bold().SingleLine().TextColor(color)
			})
			if label.Clicked() {
				*expanded = !*expanded
				c.Invalidate()
			}
		})
		if !*expanded {
			// One supporting line, so the fold is not a single bare word.
			name := a.approvalServerName(r)
			target := op.Endpoint
			if target == "" {
				target = op.Host
			}
			line := target
			if name != "" && name != target {
				line = name + " · " + target
			}
			ui.Text(c, reviewText(line)).FillWidth().FontSize(11).TextColor(c.Theme().TextMuted).SingleLine()
			if actions := approvalActionsText(r); actions != "" {
				ui.Text(c, actions).FillWidth().FontSize(11).TextColor(c.Theme().TextMuted)
			}
			if op.Kind == "grant" && r.Status == "pending" {
				ui.Text(c, grantTermsText(c, state.chosenLifetime(r), state.chosenIdle(r))).FillWidth().FontSize(11).TextColor(c.Theme().TextMuted)
			}
			ui.Text(c, tr(c, "批准后可在授权中心随时撤销。")).FillWidth().FontSize(11).TextColor(c.Theme().TextMuted)
			return
		}
		// Open, the card takes the room the window has left instead of hiding
		// the same paragraph behind a small inner scroller.
		ui.Column(c).FillWidth().Gap(6).Children(func() {
			if _, actions, _ := approvalPermission(r); actions != "" {
				ui.Text(c, actions).FillWidth().FontSize(11).TextColor(c.Theme().TextMuted)
			}
			if caution != "" {
				ui.Text(c, caution).FillWidth().FontSize(11).TextColor(c.Theme().Warning)
			}
			if op.Kind == "grant" {
				requestGrantRoots(c, op.Grant)
				if r.Status == "pending" {
					ui.Text(c, grantTermsText(c, state.chosenLifetime(r), state.chosenIdle(r))).FillWidth().FontSize(11)
				}
			}
			ui.Column(c).FillWidth().Gap(4).Children(func() { requestTechnicalReview(c, r) })
		})
	})
}

// approvalActionsText is the short "what it can do" line for the fold.
func approvalActionsText(r approval.Request) string {
	_, actions, _ := approvalPermission(r)
	return actions
}

// approvalGrantChoices are the owner's two decisions about a session: how long
// it runs, and whether idling ends it early.
func (a *app) approvalGrantChoices(c *ui.Context, r approval.Request, state *approvalPromptState) {
	options := grantLifetimeOptions()
	labels := make([]string, len(options))
	for i, option := range options {
		labels[i] = grantLifetimeChipLabel(option)
	}
	chosen := grantLifetimeChipLabel(state.chosenLifetime(r))
	ui.Row(c).FillWidth().Gap(6).AlignItems(ui.Center).Disabled(state.submitting).Children(func() {
		choice := chosen
		menu := ui.SelectBase(c, &choice)
		menu.Trigger.Height(26).Radius(6).Padding(0, 9).Gap(5).Background(c.Theme().Surface).
			Border(1, c.Theme().Border).Label(tr(c, "授权时长")).Children(func() {
			ui.Text(c, choice).FontSize(11).SingleLine()
			ui.Icon(c, components.ChevronDownIcon).Size(10, 10).TextColor(c.Theme().TextMuted)
		})
		menu.Popup(func(panel ui.Element) {
			panel.MinWidth(96).Padding(3).Background(c.Theme().Background).Border(1, c.Theme().Border).Radius(8)
			for i, label := range labels {
				item := menu.Item(label).Height(26).Padding(0, 9)
				if item.Highlighted() {
					item.Background(c.Theme().Accent.Alpha(.12))
				}
				item.Children(func() { ui.Text(c, label).FontSize(11) })
				_ = i
			}
		})
		if menu.Trigger.Changed() {
			for i, label := range labels {
				if label == choice && options[i] != state.lifetime {
					a.changeApprovalTerms(r, state, func() { state.lifetime = options[i] })
				}
			}
		}
	})
	ui.Row(c).FillWidth().Gap(6).AlignItems(ui.Center).Disabled(state.submitting).Children(func() {
		idleOn := !state.noIdle
		ui.Text(c, tr(c, "会话闲置自动断开")).FontSize(11).Grow(1)
		if ui.Switch(c, &idleOn).Label(tr(c, "会话闲置自动断开开关")).Shrink(0).Changed() && idleOn == state.noIdle {
			a.changeApprovalTerms(r, state, func() { state.noIdle = !idleOn })
		}
	})
	ui.Row(c).FillWidth().Gap(6).AlignItems(ui.Center).Disabled(state.submitting).Children(func() {
		if state.noIdle {
			ui.Text(c, tr(c, "闲置不断开")).FontSize(11).Grow(1)
		} else {
			ui.Text(c, tr(c, "闲置等待时间")).FontSize(11).Grow(1)
		}
		options := []int{5, 10, 15, 20, 30}
		labels := make([]string, len(options))
		selected := ""
		for i, minute := range options {
			labels[i] = fmt.Sprintf(tr(c, "%d 分钟"), minute)
			if minute*60 == state.idleSeconds {
				selected = labels[i]
			}
		}
		if ui.Select(c, &selected, labels).Width(110).MinWidth(0).Label(tr(c, "会话闲置时长")).Disabled(state.noIdle).Changed() {
			for i, label := range labels {
				if selected == label && options[i]*60 != state.idleSeconds {
					a.changeApprovalTerms(r, state, func() { state.idleSeconds = options[i] * 60 })
				}
			}
		}
	})
	ui.Row(c).FillWidth().Gap(6).AlignItems(ui.Center).Children(func() {
		ui.Text(c, tr(c, "配对码")).FontSize(11).TextColor(c.Theme().TextMuted)
		ui.Text(c, r.Operation.Grant.PairingCode()).FontSize(11).Selectable().Font("monospace").TextColor(c.Theme().TextMuted)
	})
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

// approvalEnter reports Enter anywhere in the prompt, so the owner can reveal
// the password field or submit it without aiming at a control.
func approvalEnter(c *ui.Context) bool { return c.Shortcut(0, ui.KeyEnter) }

func (a *app) approvalActions(c *ui.Context, r approval.Request, state *approvalPromptState, decide func(bool, bool), close func()) {
	if state.status != "" {
		ui.Scroll(c).FillWidth().MaxHeight(54).Children(func() {
			ui.Text(c, reviewText(state.status)).FillWidth().FontSize(12).TextColor(c.Theme().Danger).Label(tr(c, "授权错误"))
		})
	}
	if r.Operation.Kind == "grant" && r.Status == "pending" {
		a.initializeApprovalGrant(r, state)
		a.approvalGrantChoices(c, r, state)
	}
	a.approvalDecisionControls(c, r, state, decide, close)
}

// The broker still verifies a fresh proof for the exact reviewed request.
func (a *app) showApproval(r approval.Request) {
	if !a.startup.mayPopup() || a.presence.backgrounded {
		return
	}
	// An authorization request is one of the few moments the app must come
	// forward: a prompt hidden behind another window is indistinguishable from
	// no prompt at all. Development builds still stay hidden until revealed.
	if a.win != nil {
		if a.win.IsMinimized() {
			a.win.Restore()
		}
		if !a.win.IsVisible() {
			a.win.Show()
		}
	}
	mygo.App.Focus()
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
		readiness := authReadiness{windowReady: ready, visible: win.IsVisible(), focused: win.IsFocused(), active: a.isActive(), systemReady: a.approvalSystemReady()}
		state.queueAutomatic(r, readiness, time.Now, win.Update, func() bool {
			return current() && win.IsVisible() && win.IsFocused() && a.isActive() && a.approvalSystemReady()
		})
	}
	win = mygo.NewWindow(mygo.WindowOptions{Title: a.approvalWindowTitle(r), Width: 420, Height: approvalWindowHeight, UseContentSize: true, MinWidth: 380, MinHeight: 540,
		Parent: a.win, DisableMinimize: true, DisableMaximize: true, DisableFullScreen: true, Content: ui.View(func(c *ui.Context) {
			a.approvalPrompt(c, r, state, func(allow, system bool) { state.start(r, allow, system, false) }, func() { win.Close() })
			queue()
		})})
	state.host = win
	a.approvalWin, a.popupApproval = win, state
	a.approvalID, a.approvalNonce = r.ID, r.Nonce
	a.bindApprovalFlow(r, state, win.Update, current, win.Close)
	win.OnReadyToShow(func() {
		ready = true
		win.Show()
		win.Focus()
		mygo.App.Focus()
		queue()
	})
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
