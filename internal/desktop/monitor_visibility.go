package desktop

// Server status is sampled over SSH, so it follows the screen: the broker probes
// a server only while some subscriber still shows the samples. One collector per
// connection is shared by every terminal on that connection, so pausing here also
// stops the duplicate work several terminals would otherwise cause.

// monitorWanted reports whether this session's status is on screen right now.
func (a *app) monitorWanted(sess *session) bool {
	if sess == nil || !a.online || a.page != 1 || a.toolsHidden || a.dock != toolDiagnostics {
		return false
	}
	if a.win != nil && (a.win.IsDestroyed() || !a.win.IsVisible()) {
		return false
	}
	// A detached tools window shows the same session, so it counts as visible
	// even while the main window is hidden.
	if a.toolsDetached && a.toolsWin != nil {
		return sess == a.serverStateSession()
	}
	if a.win != nil && (a.win.IsDestroyed() || !a.win.IsVisible()) {
		return false
	}
	return sess == a.serverStateSession()
}

// syncMonitorVisibility tells the broker which sessions are still shown. It is
// cheap: a message goes out only when a session changes state.
func (a *app) syncMonitorVisibility() {
	for _, group := range a.terminalGroups() {
		for _, sess := range group.sessions {
			a.setMonitorActive(sess, a.monitorWanted(sess))
		}
	}
}

func (a *app) setMonitorActive(sess *session, active bool) {
	if sess == nil || sess.stream == nil || sess.monitorActive == active {
		return
	}
	sess.monitorActive = active
	_ = sess.stream.SetMonitorActive(active)
}
