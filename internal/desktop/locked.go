package desktop

import (
	"time"

	"github.com/egoist/mygo/ui"
	"sshclient/internal/desktop/components"
)

var vaultLockIcon = ui.MustParseSVG([]byte(`<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.7" stroke-linecap="round" stroke-linejoin="round"><rect x="5" y="10" width="14" height="11" rx="3"/><path d="M8 10V7a4 4 0 0 1 8 0v3"/><path d="M12 14v3"/></svg>`))

func (a *app) lockedView(c *ui.Context) {
	t := c.Theme()
	state := a.ensureUnlockState()
	if !state.autoRetryAt.IsZero() {
		if delay := time.Until(state.autoRetryAt); delay > 0 {
			c.After(delay)
		}
	}
	systemReady := !a.create && a.unlockSystemReady()
	usePassword := a.create || state.passwordMode || !systemReady
	nativeIcon := state.presentation != nil && a.busy && state.systemAttempt
	backdrop, surface := t.Surface.Mix(t.Background, 0.4), t.Background
	if t.Dark {
		backdrop, surface = t.Background, t.Surface
	}
	title, button := tr(c, "解锁保险库"), tr(c, "解锁")
	if a.create {
		title, button = tr(c, "创建本机保险库"), tr(c, "创建保险库")
	}
	ui.Column(c).Grow(1).FillWidth().Center().Padding(28).Background(backdrop).Children(func() {
		ui.Column(c).FillWidth().MaxWidth(392).Gap(16).Children(func() {
			ui.Column(c).FillWidth().Padding(26).Gap(20).Radius(16).Background(surface).
				Border(1, t.Border.Alpha(0.7)).Shadow(0, 8, 32, 0, ui.RGBA(0, 0, 0, 0.06)).Label(tr(c, "保险库卡片")).Children(func() {
				ui.Column(c).FillWidth().AlignItems(ui.Center).Gap(10).Children(func() {
					ui.Box(c).Size(40, 40).Radius(12).Background(t.Accent.Alpha(0.10)).Center().Children(func() {
						ui.Icon(c, vaultLockIcon).Size(22, 22).TextColor(t.Accent)
					})
					ui.Text(c, title).FontSize(22).Bold().TextAlign(ui.Center)
					if a.create {
						ui.Text(c, tr(c, "为这台电脑上的连接资料设置一个主密码。")).FontSize(13).TextColor(t.TextMuted).FillWidth().TextAlign(ui.Center)
					}
				})
				submitted, systemClicked := false, false
				var anchor ui.Rect
				ui.Column(c).FillWidth().Gap(12).Children(func() {
					if usePassword {
						// Mode-specific keys prevent a newly expanded field from
						// reusing the method row's previously committed bounds.
						input, action := components.AuthenticationPasswordWithAnchor(c.Key("vault-unlock-password"), &a.pass, tr(c, "主密码"), a.approvalSystemLabel(c), systemReady, a.busy && !nativeIcon, nativeIcon)
						input.Placeholder(tr(c, "输入主密码")).Tooltip(tr(c, "本机保险库主密码，与服务器登录密码不同")).AutoFocus()
						action.Disabled(a.busy)
						submitted, systemClicked = input.Submitted(), systemReady && action.Clicked()
						if systemReady {
							bounds := action.Bounds()
							if bounds.W > 0 && bounds.H > 0 {
								anchor = ui.Rect{X: bounds.X + (bounds.W-32)/2, Y: bounds.Y + (bounds.H-32)/2, W: 32, H: 32}
							}
						}
						if nativeIcon && (input.Changed() || submitted) {
							password := a.pass
							a.cancelUnlockAttempt()
							a.pass = password
						}
						if input.Changed() {
							state.passwordMode = true
							a.unlockError = ""
						}
						if a.create {
							input, _ := components.AuthenticationPasswordWithAnchor(c.Key("vault-unlock-confirm"), &a.confirm, tr(c, "确认主密码"), "", false, a.busy, false)
							input.Placeholder(tr(c, "再次输入主密码"))
							if input.Changed() {
								state.passwordMode = true
								a.unlockError = ""
							}
							submitted = submitted || input.Submitted()
							ui.Text(c, tr(c, "请使用较长、独立的主密码，用于保护已保存的连接。")).FillWidth().FontSize(12).TextColor(t.TextMuted)
						}
					} else {
						method := components.Button(c, "", components.ButtonOptions{Variant: components.Quiet}).Key("vault-unlock-method").
							Label(a.approvalSystemLabel(c)).FillWidth().Height(38).Padding(0).AutoFocus().Disabled(a.busy && !state.systemAttempt)
						method.Children(func() {
							ui.Text(c, a.approvalSystemLabel(c)).FontSize(14).Grow(1).SingleLine()
							ui.Box(c).Size(36, 34).Center().Children(func() {
								icon := ui.Box(c).Key("vault-unlock-biometric-icon").Size(32, 32).Center()
								anchor = icon.Bounds()
								icon.Children(func() {
									if nativeIcon {
										return
									}
									if a.busy {
										ui.Spinner(c).Size(18, 18)
									} else {
										ui.Icon(c, components.FingerprintIcon).Size(22, 22).TextColor(t.Accent)
									}
								})
							})
						})
						if method.Clicked() || c.Shortcut(0, ui.KeyEnter) {
							a.expandUnlockPassword()
						}
					}
					if a.unlockError != "" {
						ui.Box(c).FillWidth().Padding(10, 12).Radius(8).Background(t.Danger.Alpha(0.07)).Children(func() {
							ui.Text(c, a.unlockError).FillWidth().FontSize(12).TextColor(t.Danger).Label(tr(c, "解锁错误"))
						})
					}
				})
				if state.presentation != nil {
					a.placeUnlockAuthentication(anchor)
					// Ask for one more frame only while no usable anchor exists yet
					// or right after the anchor moved; a stable anchor during an
					// authentication asked for 30 rebuilds a second before.
					if state.anchor.follow(anchor, state.presentation.Ready(), nativeIcon, systemReady) {
						c.After(33 * time.Millisecond)
					}
				}
				if usePassword {
					ready := !a.busy && a.pass != "" && (!a.create || a.confirm != "")
					label := button
					if a.busy && !state.systemAttempt {
						label = tr(c, "正在解锁…")
						if a.create {
							label = tr(c, "正在创建…")
						}
					}
					clicked := components.PrimaryButton(c, label).FillWidth().Height(38).Label(tr(c, "确认解锁")).Disabled(!ready).Clicked()
					if ready && (clicked || submitted) {
						a.unlock()
					}
				}
				if systemClicked {
					a.systemUnlock()
				}
			})
			if a.isTestVault() {
				ui.Text(c, tr(c, "测试保险库 · 不要录入真实凭据")).FillWidth().TextAlign(ui.Center).FontSize(12).TextColor(t.Warning)
			}
		})
	})
}
