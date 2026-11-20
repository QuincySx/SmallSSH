package desktop

import "slices"

// ShowDockIcon is the residency preference. A workbench with an open window
// always has a Dock entry, including while minimized; losing focus is unrelated.
func (a *app) syncPresenceDock() {
	e := a.presence.effects
	if e == nil || !e.dockSupported || !a.startup.mayPopup() {
		return
	}
	e.setDock(!a.presence.backgrounded || a.presence.dockFallback || a.presenceDock(a.behavior))
}

// Only actual, still-owned windows may be restored. A disconnected VNC or a
// closed/replaced file window must never come back from a saved interface value.
func (a *app) presenceWindows() []toolsWindow {
	var windows []toolsWindow
	for _, w := range []toolsWindow{a.settingsWin, a.editorWin, a.toolsWin} {
		if w != nil {
			windows = append(windows, w)
		}
	}
	if a.approvalWin != nil {
		windows = append(windows, a.approvalWin)
	}
	for _, run := range a.vnc.runs {
		if run.window != nil {
			windows = append(windows, run.window)
		}
	}
	return windows
}

func (a *app) parkPresenceWindow(w toolsWindow) {
	live := a.presenceWindows()
	parked := a.presence.hiddenWindows[:0]
	for _, old := range a.presence.hiddenWindows {
		if old != w && slices.Contains(live, old) {
			parked = append(parked, old)
		}
	}
	if slices.Contains(live, w) {
		parked = append(parked, w)
	}
	a.presence.hiddenWindows = parked
}

func (a *app) hidePresenceWindows() {
	for _, w := range a.presenceWindows() {
		if state, ok := w.(interface{ IsVisible() bool }); ok && state.IsVisible() {
			a.parkPresenceWindow(w)
			w.Hide()
		}
	}
}

func (a *app) restorePresenceWindows() {
	parked := a.presence.hiddenWindows
	a.presence.hiddenWindows = nil
	for _, w := range parked {
		if slices.Contains(a.presenceWindows(), w) {
			w.Show()
		}
	}
}

// Async creation can finish after the user backgrounds the app. Keep that
// window hidden until an explicit restore instead of leaving it on the desktop.
func (a *app) showPresenceWindow(w toolsWindow) bool {
	if w == nil {
		return false
	}
	if a.presence.backgrounded || !a.startup.mayPopup() {
		a.parkPresenceWindow(w)
		return false
	}
	a.syncPresenceDock()
	w.Show()
	return true
}
