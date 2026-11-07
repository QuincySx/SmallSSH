package desktop

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"

	"github.com/egoist/mygo"
)

type presenceTray interface {
	destroy()
	relabel()
}

type presenceEffects struct {
	createTray                   func() (presenceTray, error)
	setDock                      func(bool)
	hide                         func()
	mainNeedsRestore             func() bool
	show                         func()
	focus                        func()
	post                         func(func())
	save                         func(behaviorPreferences) error
	dockSupported, traySupported bool
}

type presenceState struct {
	effects      *presenceEffects
	tray         presenceTray
	quitting     bool
	backgrounded bool
	dockFallback bool
	trayFailed   bool
	warning      string
	quitAttempt  uint64
}

type nativePresenceTray struct {
	tray             *mygo.Tray
	show, lock, quit *mygo.MenuItem
}

func (t *nativePresenceTray) destroy() { t.tray.Destroy() }
func (t *nativePresenceTray) relabel() {
	t.show.SetLabel(tr(nil, "显示窗口"))
	t.lock.SetLabel(tr(nil, "立即锁定"))
	t.quit.SetLabel(tr(nil, "退出应用"))
}

func (a *app) installPresence() {
	a.presence.effects = &presenceEffects{
		dockSupported: runtime.GOOS == "darwin", traySupported: runtime.GOOS == "darwin" || runtime.GOOS == "windows" || runtime.GOOS == "linux",
		setDock: func(show bool) {
			policy := mygo.ActivationPolicyAccessory
			if show {
				policy = mygo.ActivationPolicyRegular
			}
			if mygo.App.ActivationPolicy() != policy {
				mygo.App.SetActivationPolicy(policy)
			}
		},
		hide:             func() { a.win.Hide() },
		mainNeedsRestore: func() bool { return !a.win.IsVisible() || a.win.IsMinimized() },
		show: func() {
			if a.win.IsMinimized() {
				a.win.Restore()
			}
			a.win.Show()
		}, focus: func() { a.win.Focus() },
		post: a.postUI, save: a.applyBehaviorPreferences,
		createTray: func() (presenceTray, error) {
			resources, err := mygo.App.Path(mygo.PathResources)
			if err != nil {
				return nil, err
			}
			icon, err := os.ReadFile(filepath.Join(resources, "icon.png"))
			if err != nil {
				return nil, err
			}
			show := &mygo.MenuItem{Label: tr(nil, "显示窗口"), Click: func(*mygo.MenuItem, *mygo.Window) { a.showFromPresence() }}
			lock := &mygo.MenuItem{Label: tr(nil, "立即锁定"), Click: func(*mygo.MenuItem, *mygo.Window) { a.lock() }}
			quit := &mygo.MenuItem{Label: tr(nil, "退出应用"), Click: func(*mygo.MenuItem, *mygo.Window) { mygo.App.Quit() }}
			tray, err := mygo.NewTray(mygo.TrayOptions{ID: "ssh-client-workbench", Icon: icon, IconIsTemplate: false, ToolTip: applicationName, Menu: mygo.NewMenu([]*mygo.MenuItem{show, lock, quit})})
			if err != nil {
				return nil, err
			}
			return &nativePresenceTray{tray: tray, show: show, lock: lock, quit: quit}, nil
		},
	}
	if !a.startup.background {
		a.restorePresence()
	}
}

func (a *app) presenceDock(p behaviorPreferences) bool {
	return a.presence.effects != nil && a.presence.effects.dockSupported && behaviorEnabled(p.ShowDockIcon)
}

func (a *app) applyPresence(next behaviorPreferences) error {
	e := a.presence.effects
	if e == nil || a.startup.background && !a.startup.revealed {
		return errors.New(tr(nil, "后台设置暂不可用"))
	}
	if next.BackgroundMode != "application" {
		next.BackgroundMode = "off"
	}
	if !e.dockSupported {
		next.ShowDockIcon = nil
	}
	if !e.traySupported {
		next.ShowTrayIcon = false
	}
	if next.BackgroundMode == "application" && !a.presenceDock(next) && !next.ShowTrayIcon {
		return errors.New(tr(nil, "驻留时请至少保留一个恢复入口"))
	}
	var prepared presenceTray
	keepFallback := a.presence.trayFailed && a.behavior.ShowTrayIcon && next.ShowTrayIcon && a.presence.tray == nil && (next.BackgroundMode == "off" || a.presenceDock(next))
	if next.ShowTrayIcon && a.presence.tray == nil && !keepFallback {
		var err error
		prepared, err = e.createTray()
		if err != nil {
			return err
		}
	}
	if err := e.save(next); err != nil {
		if prepared != nil {
			prepared.destroy()
		}
		return err
	}
	if prepared != nil {
		a.presence.tray = prepared
	}
	a.presence.dockFallback = keepFallback && a.presenceDock(next)
	a.presence.trayFailed = keepFallback
	if !keepFallback {
		a.presence.warning = ""
	} else if next.BackgroundMode == "off" && !a.presenceDock(next) {
		if e.dockSupported {
			a.presence.warning = tr(nil, "菜单栏图标不可用。")
		} else {
			a.presence.warning = tr(nil, "通知区域图标不可用。")
		}
	}
	if e.dockSupported {
		e.setDock(a.presenceDock(next))
	}
	if !next.ShowTrayIcon && a.presence.tray != nil {
		a.presence.tray.destroy()
		a.presence.tray = nil
	}
	return nil
}

func (a *app) restorePresence() {
	e := a.presence.effects
	if e == nil || a.startup.background && !a.startup.revealed {
		return
	}
	if a.behavior.ShowTrayIcon && e.traySupported && a.presence.tray == nil {
		tray, err := e.createTray()
		if err != nil {
			a.presence.trayFailed = true
			a.presence.dockFallback = e.dockSupported
			if e.dockSupported {
				a.presence.warning = fmt.Sprintf(tr(nil, "菜单栏图标不可用，已保留 Dock 入口：%s"), err)
				e.setDock(true)
			} else {
				a.presence.warning = fmt.Sprintf(tr(nil, "通知区域图标不可用，窗口将保持打开：%s"), err)
			}
			return
		}
		a.presence.tray = tray
	}
	a.presence.trayFailed = false
	missingEntry := a.behavior.BackgroundMode == "application" && !a.presenceDock(a.behavior) && a.presence.tray == nil
	a.presence.dockFallback = missingEntry && e.dockSupported
	if missingEntry {
		a.presence.warning = tr(nil, "驻留时请至少保留一个恢复入口")
	} else {
		a.presence.warning = ""
	}
	if e.dockSupported {
		e.setDock(a.presence.dockFallback || a.presenceDock(a.behavior))
	}
}

func (a *app) presenceClose(prevent func()) bool {
	if a.presence.quitting || a.behavior.BackgroundMode != "application" || a.presence.effects == nil {
		return false
	}
	prevent()
	if a.presence.tray == nil && !a.presenceDock(a.behavior) && !a.presence.dockFallback {
		a.presence.warning = tr(nil, "驻留时请至少保留一个恢复入口")
		a.status = a.presence.warning
		return true
	}
	a.presence.backgrounded = true
	a.presence.effects.hide()
	return true
}

func (a *app) beginPresenceQuit() {
	a.presence.quitAttempt++
	attempt := a.presence.quitAttempt
	a.presence.quitting = true
	if a.presence.effects != nil {
		a.presence.effects.post(func() {
			if a.presence.quitAttempt == attempt {
				a.presence.quitting = false
			}
		})
	}
}

func (a *app) showFromPresence() {
	if a.startup.background && !a.startup.revealed {
		return
	}
	if a.presence.effects != nil {
		a.presence.backgrounded = false
		a.presence.effects.show()
		a.presence.effects.focus()
	}
}
func (a *app) presenceActivate(_ bool) {
	if a.presence.effects != nil && a.presence.effects.mainNeedsRestore() {
		a.showFromPresence()
	}
}
func (a *app) refreshPresenceLabels() {
	if a.presence.tray != nil {
		a.presence.tray.relabel()
	}
}
func (a *app) destroyPresence() {
	if a.presence.tray != nil {
		a.presence.tray.destroy()
		a.presence.tray = nil
	}
}
