package desktop

import (
	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"
	"runtime"
	"sshclient/internal/desktop/components"
	"sshclient/internal/systemauth"
)

func (a *app) openSettingsWindow(section string) {
	if !a.online {
		return
	}
	if a.presence.backgrounded {
		// This entry is an explicit user action, unlike deferred creation.
		a.showFromPresence()
	}
	if !a.settingsOpen {
		a.clearInlineApproval()
	}
	if a.page == 3 || a.page == 4 {
		a.page = 1
	}
	a.selectSettingsSection(section)
	a.settingsOpen = true
	if a.settingsWin != nil {
		a.showPresenceWindow(a.settingsWin)
		a.invalidateWindows()
		return
	}
	if a.settingsWindowPending {
		return
	}
	a.settingsWindowSequence++
	seq := a.settingsWindowSequence
	factory := a.newSettingsWindow
	if factory == nil {
		if a.win == nil {
			return
		}
		factory = func(o mygo.WindowOptions, closed func()) toolsWindow {
			w := mygo.NewWindow(o)
			a.auth = systemauth.New(w.NativeHandle())
			w.OnReadyToShow(func() {
				if seq == a.settingsWindowSequence && a.settingsOpen {
					a.settingsWindowReady = true
					a.invalidateWindows()
				}
			})
			w.OnFocus(func() { a.queueInlineApproval() })
			w.OnShow(func() { a.queueInlineApproval() })
			w.OnHide(func() {
				if seq == a.settingsWindowSequence {
					a.clearInlineApproval()
					a.ownerPass = ""
					if a.authCancel != nil {
						a.authCancel()
					}
				}
			})
			w.OnClosed(closed)
			return w
		}
	}
	a.settingsWindowPending = true
	create := func() {
		if seq != a.settingsWindowSequence || !a.online || !a.settingsOpen {
			return
		}
		a.settingsWindowPending = false
		opts := mygo.WindowOptions{Title: tr(nil, "设置"), Width: 1080, Height: 760, MinWidth: 720, MinHeight: 520, Hidden: true, Content: ui.View(a.settingsWindowView)}
		applyHiddenWindowChrome(&opts, runtime.GOOS)
		w := factory(opts, func() {
			if seq == a.settingsWindowSequence {
				a.closeSettingsWindow()
			}
		})
		if seq != a.settingsWindowSequence || !a.online || !a.settingsOpen {
			if w != nil {
				w.Hide()
				w.Update(w.Close)
			}
			return
		}
		if w == nil {
			a.settingsOpen = false
			a.status = tr(nil, "无法打开设置窗口")
			return
		}
		a.settingsWin = w
		a.showPresenceWindow(w)
		a.invalidateWindows()
	}
	if a.win != nil || a.dispatch != nil {
		a.postUI(create)
	} else {
		create()
	}
}

func (a *app) closeSettingsWindow() {
	a.cancelIdentitySettings()
	w := a.settingsWin
	a.settingsWin = nil
	a.settingsOpen, a.settingsWindowPending, a.settingsWindowReady = false, false, false
	a.settingsWindowSequence++
	a.ownerPass = ""
	if a.authCancel != nil {
		a.authCancel()
		a.authCancel = nil
	}
	a.clearInlineApproval()
	if a.win != nil {
		a.auth = systemauth.New(a.win.NativeHandle())
	}
	if a.prompt.host == dialogHostSettings {
		a.prompt = promptState{}
	}
	if w != nil {
		w.Hide()
		w.Update(w.Close)
	}
	a.invalidateWindows()
}

func (a *app) settingsWindowView(c *ui.Context) {
	if !a.online || !a.settingsOpen {
		return
	}
	old := a.renderingSettingsWindow
	a.renderingSettingsWindow = true
	defer func() { a.renderingSettingsWindow = old; a.queueInlineApproval(); a.publishToolsState() }()
	c.SetZoom(a.viewZoom())
	components.ApplyTheme(c, false)
	a.settingsView(c)
	a.promptForHost(c, dialogHostSettings)
}

func (a *app) approvalCenterVisible() bool {
	return a.page == 3 || a.settingsOpen && a.settingsSection == settingsApprovals
}

func (a *app) approvalHostWindow() *mygo.Window {
	if a.settingsOpen && a.settingsSection == settingsApprovals {
		if w, ok := a.settingsWin.(*mygo.Window); ok {
			return w
		}
		return nil
	}
	return a.win
}

func (a *app) approvalHostReady() bool {
	if a.settingsOpen && a.settingsSection == settingsApprovals {
		return a.settingsWindowReady
	}
	return a.mainWindowReady
}
