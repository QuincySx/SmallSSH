package desktop

import "sshclient/internal/monitor"

// Source IDs are issued by the broker only after verifying the SSH transport.
// Session fields remain aliases for rendering; this state owns the one history.
type serverStatusState struct {
	sourceID string
	stats    *monitor.Stats
	history  statusHistory
	sessions map[*session]struct{}
}

func (a *app) updateServerStatus(sess *session, generation int, stats *monitor.Stats, message string) {
	update := func() {
		if generation != a.generation || !a.hasSession(sess) || sess.closed {
			return
		}
		if a.applyServerStatus(sess, stats, message) {
			a.invalidateWindows()
		}
	}
	if a.win != nil {
		a.win.Update(update)
	} else if a.dispatch != nil {
		a.dispatch(update)
	}
}

func (a *app) applyServerStatus(sess *session, stats *monitor.Stats, message string) bool {
	if stats == nil {
		changed := sess.monitorStatus != message
		sess.monitorStatus = message
		return changed
	}
	if stats.SourceID == "" {
		// Older brokers and synthetic previews have no trusted sharing identity.
		if sess.statusSource != nil || !newerServerStats(stats, sess.stats) {
			return false
		}
		sess.stats = retainServerInventory(stats, sess.stats)
		sess.history.add(sess.stats)
		sess.monitorStatus = message
		return true
	}
	if stats.Time.IsZero() {
		return false
	}
	if sess.statusSource != nil && sess.statusSource.sourceID != stats.SourceID {
		// A stream is bound for its lifetime; queued delivery cannot rebind it.
		return false
	}
	if a.serverStatuses == nil {
		a.serverStatuses = make(map[string]*serverStatusState)
	}
	state := a.serverStatuses[stats.SourceID]
	if state == nil {
		state = &serverStatusState{sourceID: stats.SourceID, sessions: make(map[*session]struct{})}
		a.serverStatuses[stats.SourceID] = state
	}
	joined := sess.statusSource == nil
	if joined {
		sess.statusSource = state
		state.sessions[sess] = struct{}{}
	}
	if !newerServerStats(stats, state.stats) {
		if joined {
			sess.stats, sess.history, sess.monitorStatus = state.stats, state.history, message
		}
		return joined
	}
	// Disconnect takes a private snapshot before bounded history compaction can
	// mutate the shared backing array, even if a close was already observed.
	a.pruneServerStatus(state)
	state.stats = retainServerInventory(stats, state.stats)
	state.history.add(state.stats)
	for target := range state.sessions {
		target.stats, target.history, target.monitorStatus = state.stats, state.history, message
	}
	return true
}

func newerServerStats(next, current *monitor.Stats) bool {
	// Legacy/synthetic snapshots may omit the timestamp. Their local display
	// remains compatible, while real samples always advance monotonically.
	return current == nil || next.Time.IsZero() || current.Time.IsZero() || next.Time.After(current.Time)
}

func retainServerInventory(next, current *monitor.Stats) *monitor.Stats {
	if current != nil && current.Inventory != nil && (next.Inventory == nil || current.Inventory.Time.After(next.Inventory.Time)) {
		copy := *next
		copy.Inventory = current.Inventory
		return &copy
	}
	return next
}

// Action results update shared data, while the action's busy state, permission
// and target continue to belong to the exact originating session.
func (a *app) updateServerInventory(sess *session, inventory *monitor.Inventory) {
	if sess == nil || sess.closed || !a.hasSession(sess) || sess.stats == nil || inventory == nil {
		return
	}
	stats := sess.stats
	if stats.Inventory != nil && !inventory.Time.After(stats.Inventory.Time) {
		return
	}
	copy := *stats
	copy.Inventory = inventory
	if state := sess.statusSource; state != nil {
		if a.serverStatuses[state.sourceID] != state {
			return
		}
		a.pruneServerStatus(state)
		state.stats = &copy
		for target := range state.sessions {
			target.stats = state.stats
		}
	} else {
		sess.stats = &copy
	}
}

func (a *app) pruneServerStatus(state *serverStatusState) {
	for sess := range state.sessions {
		if sess.closed || !a.hasSession(sess) {
			a.detachServerStatus(sess)
		}
	}
}

func (a *app) detachServerStatus(sess *session) {
	state := sess.statusSource
	if state == nil {
		return
	}
	// Points' network entries are immutable. Only the compacting points array
	// needs copying, once, when a tab stops following the live collector.
	sess.history.points = append([]statusPoint(nil), sess.history.points...)
	sess.statusSource = nil
	delete(state.sessions, sess)
	if len(state.sessions) == 0 {
		delete(a.serverStatuses, state.sourceID)
	}
}

func (a *app) clearServerStatuses() {
	for _, sess := range a.sessions {
		sess.statusSource = nil
	}
	for _, state := range a.serverStatuses {
		state.sessions = nil
	}
	a.serverStatuses = nil
}
