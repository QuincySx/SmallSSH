package desktop

import (
	"context"
	"testing"
	"time"

	"sshclient/internal/approval"
	"sshclient/internal/monitor"
)

func sharedStatusApp() (*app, *session, *session) {
	first := &session{host: "alias-one", name: "Same label", target: approval.Operation{Endpoint: "user@127.0.0.1:22", HostFingerprint: "SHA256:verified"}}
	second := &session{host: "alias-two", name: "Same label", target: first.target}
	a := &app{ctx: context.Background(), online: true, sessions: []*session{first, second}, dispatch: func(fn func()) { fn() }}
	return a, first, second
}

func sharedStats(at time.Time, cpu float64) *monitor.Stats {
	return &monitor.Stats{SourceID: "broker-source-1", Time: at, CPU: cpu}
}

func TestSharedServerStatusUsesOneSnapshotAndHistoryPerTrustedSource(t *testing.T) {
	a, first, second := sharedStatusApp()
	at := time.Now()
	a.updateServerStatus(first, a.generation, sharedStats(at, 10), "自动更新")
	a.updateServerStatus(second, a.generation, sharedStats(at, 10), "自动更新")
	if len(a.serverStatuses) != 1 || first.stats != second.stats || first.statusSource != second.statusSource {
		t.Fatal("trusted broker source did not share canonical state")
	}
	if len(first.history.points) != 1 || &first.history.points[0] != &second.history.points[0] {
		t.Fatal("duplicate per-terminal delivery duplicated the history")
	}
	current := first.stats
	a.updateServerStatus(second, a.generation, sharedStats(at, 99), "duplicate")
	a.updateServerStatus(first, a.generation, sharedStats(at.Add(-time.Second), 88), "old")
	if first.stats != current || second.stats != current || first.monitorStatus != "自动更新" || len(first.history.points) != 1 {
		t.Fatal("duplicate or out-of-order delivery replaced the canonical sample")
	}
	a.updateServerStatus(second, a.generation, sharedStats(at.Add(2*time.Second), 20), "自动更新")
	if first.stats != second.stats || first.stats.CPU != 20 || len(first.history.points) != 2 || &first.history.points[0] != &second.history.points[0] {
		t.Fatal("a sibling did not follow the one growing history")
	}
	third := &session{host: first.host, name: first.name}
	a.sessions = append(a.sessions, third)
	other := sharedStats(at, 50)
	other.SourceID = "another-verified-source"
	a.updateServerStatus(third, a.generation, other, "自动更新")
	if third.stats == first.stats || third.statusSource == first.statusSource || len(a.serverStatuses) != 2 {
		t.Fatal("display names or host aliases merged distinct sources")
	}
	a.updateServerStatus(first, a.generation, other, "wrong source")
	if first.stats.SourceID != "broker-source-1" || first.monitorStatus != "自动更新" {
		t.Fatal("queued stats rebound an existing stream to a different collector")
	}
}

func TestLegacyServerStatusRemainsSessionLocal(t *testing.T) {
	a, first, second := sharedStatusApp()
	at := time.Now()
	a.updateServerStatus(first, a.generation, &monitor.Stats{Time: at, CPU: 10}, "自动更新")
	a.updateServerStatus(second, a.generation, &monitor.Stats{Time: at, CPU: 30}, "自动更新")
	if len(a.serverStatuses) != 0 || first.stats == second.stats || &first.history.points[0] == &second.history.points[0] {
		t.Fatal("source-free synthetic sessions acquired shared ownership")
	}
	a.updateServerStatus(first, a.generation, &monitor.Stats{Time: at.Add(-time.Second), CPU: 99}, "old")
	if first.stats.CPU != 10 || second.stats.CPU != 30 {
		t.Fatal("a legacy callback changed another session or accepted older stats")
	}
}

func TestSharedInventoryPropagatesAndSurvivesLateMonitorDelivery(t *testing.T) {
	a, first, second := sharedStatusApp()
	at := time.Now()
	initial := sharedStats(at, 10)
	initial.Inventory = &monitor.Inventory{Time: at, Manager: "before"}
	a.updateServerStatus(first, a.generation, initial, "自动更新")
	a.updateServerStatus(second, a.generation, initial, "自动更新")
	first.management = newManagementUI()
	first.management.busy = true
	fresh := &monitor.Inventory{Time: at.Add(time.Minute), Manager: "after"}
	a.updateServerInventory(first, fresh)
	if first.stats != second.stats || second.stats.Inventory != fresh || second.management != nil || !first.management.busy {
		t.Fatal("action result failed to propagate data or shared session-owned actions")
	}
	late := sharedStats(at.Add(2*time.Second), 20)
	late.Inventory = &monitor.Inventory{Time: at.Add(-time.Minute), Manager: "old"}
	a.updateServerStatus(second, a.generation, late, "自动更新")
	a.updateServerInventory(second, &monitor.Inventory{Time: at, Manager: "late action result"})
	if first.stats != second.stats || first.stats.Inventory != fresh || first.stats.CPU != 20 || len(first.history.points) != 2 {
		t.Fatal("late inventory overwrote the newer post-action inventory")
	}
	third := &session{host: "late alias"}
	a.sessions = append(a.sessions, third)
	a.updateServerStatus(third, a.generation, initial, "自动更新")
	if third.stats != first.stats || third.stats.Inventory != fresh || len(third.history.points) != 2 || &third.history.points[0] != &first.history.points[0] {
		t.Fatal("late subscriber did not adopt the canonical latest state")
	}
	withoutInventory := sharedStats(at.Add(4*time.Second), 30)
	a.updateServerStatus(third, a.generation, withoutInventory, "自动更新")
	if first.stats.Inventory != fresh {
		t.Fatal("a partial snapshot cleared the post-action inventory")
	}
}

func TestSharedDisconnectFreezesHistoryWhileSiblingContinues(t *testing.T) {
	a, first, second := sharedStatusApp()
	at := time.Now()
	for n := 0; n < statusHistoryLimit; n++ {
		a.updateServerStatus(first, a.generation, sharedStats(at.Add(time.Duration(n)*2*time.Second), float64(n)), "自动更新")
	}
	a.updateServerStatus(second, a.generation, first.stats, "自动更新")
	frozenStats := first.stats
	a.detachServerStatus(first)
	first.closed = true
	frozenStart := first.history.points[0].at
	if &first.history.points[0] == &second.history.points[0] || first.statusSource != nil || len(a.serverStatuses) != 1 {
		t.Fatal("disconnect did not snapshot history and release only its own reference")
	}
	privatePoint := &first.history.points[0]
	a.detachServerStatus(first)
	if privatePoint != &first.history.points[0] {
		t.Fatal("repeated disconnect copied the frozen history again")
	}
	for n := statusHistoryLimit; n < statusHistoryLimit+10; n++ {
		a.updateServerStatus(second, a.generation, sharedStats(at.Add(time.Duration(n)*2*time.Second), float64(n)), "自动更新")
	}
	a.updateServerStatus(first, a.generation, second.stats, "late")
	a.updateServerInventory(first, &monitor.Inventory{Time: at.Add(time.Hour)})
	if first.stats != frozenStats || first.history.points[0].at != frozenStart || first.history.points[len(first.history.points)-1].at != frozenStats.Time || !second.history.points[0].at.After(frozenStart) {
		t.Fatal("live history compaction or a queued update mutated the closed tab")
	}
	a.closeSessionAt(1)
	if len(a.serverStatuses) != 0 || second.statusSource != nil || !second.closed {
		t.Fatal("last live tab retained active cache ownership")
	}
}

func TestSharedStatusLockRejectsQueuedCallbacksAndReleasesSources(t *testing.T) {
	a, first, second := sharedStatusApp()
	at := time.Now()
	a.updateServerStatus(first, a.generation, sharedStats(at, 10), "自动更新")
	a.updateServerStatus(second, a.generation, first.stats, "自动更新")
	state := first.statusSource
	var queued func()
	a.dispatch = func(fn func()) { queued = fn }
	a.updateServerStatus(first, a.generation, sharedStats(at.Add(time.Second), 99), "late")
	a.lock()
	queued()
	if len(a.serverStatuses) != 0 || len(state.sessions) != 0 || len(a.sessions) != 0 || first.statusSource != nil || second.statusSource != nil || first.stats.CPU != 10 {
		t.Fatal("lock retained cache ownership or accepted an obsolete queued callback")
	}
}

func TestSharedStatusActionConfirmationRetainsClickedSession(t *testing.T) {
	a, first, second := sharedStatusApp()
	at := time.Now()
	a.updateServerStatus(first, a.generation, sharedStats(at, 10), "自动更新")
	a.updateServerStatus(second, a.generation, first.stats, "自动更新")
	a.active = 0
	a.confirmServerAction("Test", "target", "message", "true", false)
	confirm := a.prompt.confirm
	if confirm == nil {
		t.Fatal("action confirmation missing")
	}
	a.active = 1
	a.detachServerStatus(first)
	first.closed = true
	confirm("")
	if a.status != tr(nil, "原连接已关闭，请重新选择操作目标") || second.management != nil || second.closed {
		t.Fatal("shared data redirected a closed originating session's action to its sibling")
	}
}

func TestSharedStatusReconnectBindsNewSourceAndRejectsOldSessionDelivery(t *testing.T) {
	a, original, _ := sharedStatusApp()
	a.sessions = []*session{original}
	at := time.Now()
	a.updateServerStatus(original, a.generation, sharedStats(at, 10), "自动更新")
	var queued func()
	a.dispatch = func(fn func()) { queued = fn }
	a.updateServerStatus(original, a.generation, sharedStats(at.Add(time.Second), 99), "late")
	a.detachServerStatus(original)
	original.closed = true
	if len(a.serverStatuses) != 0 {
		t.Fatal("last disconnected tab retained its collector")
	}
	added := &session{host: original.host, target: original.target}
	a.sessions = append(a.sessions, added)
	a.dispatch = func(fn func()) { fn() }
	reconnected := sharedStats(at.Add(2*time.Second), 20)
	reconnected.SourceID = "new-collector-lifetime"
	a.updateServerStatus(added, a.generation, reconnected, "自动更新")
	a.replaceClosedTerminal(original, added)
	queued()
	if a.hasSession(original) || !a.hasSession(added) || original.stats.CPU != 10 || added.stats != reconnected || len(a.serverStatuses) != 1 || len(added.history.points) != 1 || added.statusSource.sourceID != "new-collector-lifetime" {
		t.Fatal("reconnect reused an old source/history or accepted an obsolete stream callback")
	}
}
