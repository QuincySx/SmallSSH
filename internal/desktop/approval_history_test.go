package desktop

import (
	"context"
	"fmt"
	"io"
	"strings"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"
	"sshclient/internal/accesspolicy"
	"sshclient/internal/approval"
	"sshclient/internal/auditlog"
	"sshclient/internal/broker"
	"sshclient/internal/vault"
)

func historyUIFixture() *app {
	return &app{approvalTab: 2, approvalHistory: approvalHistoryState{loaded: true, next: "older-page", events: []auditlog.Event{
		{Time: time.Date(2026, 10, 24, 8, 1, 0, 0, time.UTC), Host: "server", HostName: "Example server", Client: "Assistant", Kind: "exec", Event: "denied", Reason: "uncontrolled_operation", Command: "systemctl restart nginx", RequestID: "generated-request"},
		{Time: time.Date(2026, 10, 24, 8, 0, 0, 0, time.UTC), Host: "old", Kind: "put", Event: "denied", Reason: "scope_denied", Path: "/generated/target.conf"},
	}}}
}

func TestApprovalHistoryUsesPersistentEventsAndSanitizesMetadata(t *testing.T) {
	a := historyUIFixture()
	a.requests = []approval.Request{{ID: "ephemeral", Status: "denied", Operation: approval.Operation{Client: "ephemeral-only-client"}}}
	a.approvalHistory.events[0].Client = "Assistant\n\u202E"
	tt := ui.NewTester(a.approvalView, 1100, 760)
	for _, text := range []string{"访问历史", "Example server", `自报 · Assistant\n\u202e`, "未记录", "已拒绝 · 保护模式不开放此操作", "已拒绝 · 超出授权范围", "更早记录"} {
		if !tt.HasText(text) {
			t.Fatal("persisted audit fact missing", text, tt.Texts())
		}
	}
	if tt.HasText("ephemeral-only-client") || strings.Contains(strings.Join(tt.Texts(), ""), "\u202E") || a.startup.activations != 0 || a.authSequence != 0 {
		t.Fatal("history used ephemeral requests, unescaped controls, or activated a window")
	}
}

func TestApprovalHistoryApplicantTrustUsesOwnerProofFlag(t *testing.T) {
	if historyApplicant(auditlog.Event{Client: "所有者"}) != "自报 · 所有者" || historyApplicant(auditlog.Event{OwnerIssued: true, Client: "anything"}) != "本机所有者" || historyApplicant(auditlog.Event{}) != "未记录" {
		t.Fatal("history trusted a self-reported caller or invented a legacy identity")
	}
}

func TestApprovalHistoryLoadingErrorAndEmptyAreDistinct(t *testing.T) {
	for _, item := range []struct {
		state approvalHistoryState
		want  string
	}{{approvalHistoryState{loading: true}, "正在读取历史…"}, {approvalHistoryState{loaded: true, err: "无法读取历史，请重试"}, "无法读取历史，请重试"}, {approvalHistoryState{loaded: true}, "暂无访问历史"}} {
		a := &app{approvalTab: 2, approvalHistory: item.state}
		tt := ui.NewTester(a.approvalView, 560, 760)
		if !tt.HasText(item.want) || item.want != "暂无访问历史" && tt.HasText("暂无访问历史") {
			t.Fatal("history state was confused", item.want, tt.Texts())
		}
	}
}

func TestApprovalHistoryEmptyPageWithCursorKeepsOlderNavigation(t *testing.T) {
	a := &app{approvalTab: 2, approvalHistory: approvalHistoryState{loaded: true, next: "older-valid-events"}}
	tt := ui.NewTester(a.approvalView, 560, 760)
	button, ok := tt.Find("更早记录")
	if !ok || button.Y < 0 || button.Y+button.H > 760 || tt.HasText("暂无访问历史") || !tt.HasText("本页 0 条") {
		t.Fatal("an empty scanned page hid later valid events", button, tt.Texts())
	}
}

func TestApprovalHistoryNarrowAndCollapsedKeepControlsReachable(t *testing.T) {
	for _, collapsed := range []bool{false, true} {
		a := historyUIFixture()
		a.sidebarHidden = collapsed
		tt := ui.NewTester(a.approvalView, 560, 760)
		for _, label := range []string{"刷新历史", "更早记录", "访问历史表格", "Example server", "自报 · Assistant", "执行命令", "已拒绝 · 保护模式不开放此操作", "已拒绝 · 超出授权范围"} {
			bounds, ok := tt.Find(label)
			if !ok || bounds.X < 0 || bounds.Y < 0 || bounds.X+bounds.W > 561 || bounds.Y+bounds.H > 761 {
				t.Fatal("history navigation or scrollable table escaped the viewport", collapsed, label, bounds)
			}
		}
		if a.approvalHistory.next != "older-page" || len(a.approvalHistory.events) != 2 {
			t.Fatal("layout changed loaded page")
		}
	}
}

func TestApprovalHistoryClearCancelsAndInvalidatesPendingCallbacks(t *testing.T) {
	a := historyUIFixture()
	cancelled := false
	a.approvalHistory.sequence = 8
	a.approvalHistory.cancel = func() { cancelled = true }
	a.clearApprovalHistory()
	if !cancelled || a.approvalHistory.sequence != 9 || a.approvalHistory.events != nil || a.approvalHistory.next != "" || a.approvalHistory.loaded {
		t.Fatal("history survived lock cleanup")
	}
	a.approvalHistory.loading = true
	a.loadApprovalHistory(true)
	if a.approvalHistory.sequence != 9 {
		t.Fatal("parallel history read started")
	}
	a.approvalHistory.loading = false
	a.approvalHistory.lastAttempt = time.Now()
	a.loadApprovalHistory(false)
	if a.approvalHistory.sequence != 9 {
		t.Fatal("history cooldown was bypassed")
	}
}

func TestApprovalHistoryLoadsSilentDenialsPaginatesAndSurvivesRestart(t *testing.T) {
	h := newConnectionFlowHarness(t)
	p := h.fixture.Profile()
	p.Policy = accesspolicy.Policy{}
	if err := h.a.client.SaveConnection(h.ctx, vault.ProfileChange{Profile: p}, h.server.Vault); err != nil {
		t.Fatal(err)
	}
	for range 55 {
		if _, _, err := h.a.client.Request(h.ctx, approval.Operation{Host: p.ID, Kind: "exec", Command: "printf isolated-fixture", Client: "Fixture assistant"}, time.Minute); err == nil {
			t.Fatal("protected arbitrary command reached approval")
		}
	}
	h.server.Close()
	var err error
	h.server, err = broker.Open(h.dir, h.pass)
	if err != nil {
		t.Fatal(err)
	}
	h.resetApp()
	h.a.page, h.a.approvalTab = 3, 2
	h.ui = ui.NewTester(h.a.approvalView, 1100, 760)
	h.wait(func() bool { return h.a.approvalHistory.loaded && !h.a.approvalHistory.loading })
	state := &h.a.approvalHistory
	if state.err != "" || len(state.events) != 50 || state.next == "" || h.a.ownerPass != "" {
		t.Fatal("persistent first page missing or asked for another password", len(state.events), state.err)
	}
	found := false
	for _, event := range state.events {
		if event.Client == "Fixture assistant" && event.Event == "denied" && event.Reason == "uncontrolled_operation" {
			found = true
		}
	}
	if !found || !h.ui.HasText("已拒绝 · 保护模式不开放此操作") {
		t.Fatal("silent policy denial did not survive broker and app restart")
	}
	viewport, ok := h.ui.Find("访问历史表格")
	if !ok {
		t.Fatal("history scroll viewport is missing")
	}
	h.ui.Scroll(viewport.X+viewport.W/2, viewport.Y+viewport.H/2, 0, 600)
	firstBefore, _ := state.table.Visible()
	if firstBefore == 0 {
		t.Fatal("pointer wheel did not move the history viewport")
	}
	state.lastAttempt = time.Now().Add(-approvalHistoryCooldown)
	h.a.loadApprovalHistory(false)
	h.wait(func() bool { return !state.loading })
	firstAfter, _ := state.table.Visible()
	if state.err != "" || firstAfter != firstBefore {
		t.Fatal("same-page automatic refresh moved the user's history viewport", firstBefore, firstAfter, state.err)
	}
	next := state.next
	state.lastAttempt = time.Now().Add(-approvalHistoryCooldown)
	h.click("更早记录")
	h.wait(func() bool { return !state.loading })
	if state.err != "" || state.before != next || len(state.events) == 0 || len(state.events) > 50 {
		t.Fatal("older page was not bounded or did not use its cursor", state.before, state.err, len(state.events))
	}
	state.lastAttempt = time.Now().Add(-approvalHistoryCooldown)
	h.click("刷新历史")
	h.wait(func() bool { return !state.loading })
	if state.before != "" || len(state.events) != 50 || h.a.startup.activations != 0 || h.a.authSequence != 0 {
		t.Fatal("refresh did not return to newest page without foreground activity")
	}
	state.lastAttempt = time.Now().Add(-approvalHistoryCooldown)
	h.a.loadApprovalHistory(false)
	var lateUpdate func()
	select {
	case lateUpdate = <-h.updates:
	case <-time.After(5 * time.Second):
		t.Fatal("history completion was not posted")
	}
	h.a.generation++
	h.a.clearApprovalHistory()
	lateUpdate()
	if len(h.a.approvalHistory.events) != 0 || h.a.approvalHistory.loaded || h.a.approvalHistory.loading {
		t.Fatal("late callback repopulated a cleared generation")
	}
	h.a.client.Dir = h.dir + "/offline-fixture"
	h.a.loadApprovalHistory(false)
	h.wait(func() bool { return h.a.approvalHistory.loaded && !h.a.approvalHistory.loading })
	if h.a.approvalHistory.err == "" || !h.ui.HasText("无法读取历史，请重试") || h.ui.HasText("暂无访问历史") {
		t.Fatal("failed persistent read looked like empty history")
	}
}

func TestApprovalHistoryFreshUnlockVisibleInFullWorkspace(t *testing.T) {
	for _, width := range []int{560, 1100, 1280} {
		t.Run(fmt.Sprintf("width-%d", width), func(t *testing.T) {
			h := newConnectionFlowHarness(t)
			h.resetApp()
			h.a.page = 3
			h.ui = ui.NewTester(h.a.view, width, 800)
			h.click("授权分类：历史")
			h.wait(func() bool { return h.a.approvalHistory.loaded && !h.a.approvalHistory.loading })
			state := &h.a.approvalHistory
			if state.err != "" || len(state.events) != 1 || state.events[0].Event != "unlocked" {
				t.Fatal("fresh unlocked history missing", len(state.events), state.err)
			}
			for text, minWidth := range map[string]float32{"访问历史": 50, "刷新历史": 48, "已解锁": 30, "本页 1 条": 42} {
				bounds, ok := h.ui.Find(text)
				if !ok || bounds.W < minWidth || bounds.H < 12 || bounds.X < 0 || bounds.Y < 0 || bounds.X+bounds.W > float32(width+1) || bounds.Y+bounds.H > 801 {
					t.Fatal("full workspace clipped fresh history", text, bounds, h.ui.Texts())
				}
			}
			if h.ui.HasText("暂无访问历史") || h.a.startup.activations != 0 {
				t.Fatal("fresh history was empty or activated a window")
			}
		})
	}
}

func TestApprovalHistoryOwnerOperationAppearsInFullWorkspace(t *testing.T) {
	h := newConnectionFlowHarness(t)
	p := h.fixture.Profile()
	if err := h.a.client.SaveConnection(h.ctx, vault.ProfileChange{Profile: p}, h.server.Vault); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(h.ctx, 10*time.Second)
	defer cancel()
	stream, err := h.a.client.StartWithOwner(ctx, approval.Operation{Host: p.ID, Kind: "exec", Command: "printf isolated-history-check"}, h.server.Vault)
	if err != nil {
		t.Fatal(err)
	}
	defer stream.Close()
	if _, _, err = stream.Copy(io.Discard, io.Discard); err != nil {
		stream.Close()
		t.Fatal(err)
	}
	stream.Close()
	h.resetApp()
	h.a.page = 3
	h.ui = ui.NewTester(h.a.view, 1100, 800)
	h.click("授权分类：历史")
	h.wait(func() bool { return h.a.approvalHistory.loaded && !h.a.approvalHistory.loading })
	state := &h.a.approvalHistory
	if state.err != "" {
		t.Fatal(state.err)
	}
	found := false
	for _, e := range state.events {
		found = found || e.Kind == "exec" && e.Event == "started" && e.OwnerIssued
	}
	if !found || !h.ui.HasText("本机所有者") || !h.ui.HasText("执行命令") || !h.ui.HasText("已开始") || h.ui.HasText("暂无访问历史") {
		t.Fatal("normal owner operation did not reach visible history", h.ui.Texts())
	}
	for _, text := range []string{"本机所有者", "执行命令", "已开始"} {
		bounds, ok := h.ui.Find(text)
		if !ok || bounds.W < 30 || bounds.H < 12 {
			t.Fatal("owner history text was not readable", text, bounds)
		}
	}
}

func TestApprovalHistoryAutomaticReadsLeaveManualNavigationAvailable(t *testing.T) {
	h := newConnectionFlowHarness(t)
	h.resetApp()
	h.a.page, h.a.approvalTab = 3, 2
	h.ui = ui.NewTester(h.a.view, 1100, 800)
	h.wait(func() bool { return h.a.approvalHistory.loaded && !h.a.approvalHistory.loading })
	state := &h.a.approvalHistory
	for _, label := range []string{"刷新历史", "更早记录"} {
		state.next = "fixture-next-page"
		state.lastAttempt = time.Now().Add(-approvalHistoryCooldown - time.Second)
		lastAttempt := state.lastAttempt
		h.a.refreshApprovalHistoryIfDue()
		h.ui.Frame()
		if state.loading || !state.lastAttempt.Equal(lastAttempt) {
			t.Fatal("automatic poll consumed the manual navigation interval")
		}
		h.click(label)
		if !state.loading {
			t.Fatal("manual history navigation stayed disabled", label)
		}
		h.wait(func() bool { return !state.loading })
	}
	state.lastAttempt = time.Now().Add(-approvalHistoryRefreshInterval - time.Second)
	h.a.refreshApprovalHistoryIfDue()
	if !state.loading {
		t.Fatal("due newest history did not refresh")
	}
	h.wait(func() bool { return !state.loading })
	state.before = "fixture-older-page"
	state.lastAttempt = time.Now().Add(-approvalHistoryRefreshInterval - time.Second)
	lastAttempt := state.lastAttempt
	h.a.refreshApprovalHistoryIfDue()
	if state.loading || !state.lastAttempt.Equal(lastAttempt) || state.before != "fixture-older-page" {
		t.Fatal("automatic refresh replaced the page the user was reading")
	}
}

// The record has to answer "what did it do". A row that keeps only its time and
// result cannot be reviewed: the command, or the path, is the record.
func TestApprovalHistoryShowsWhatRanAndExplainsTheChosenRow(t *testing.T) {
	for _, width := range []int{1100, 560} {
		a := historyUIFixture()
		tt := ui.NewTester(a.approvalView, width, 760)
		for _, want := range []string{"systemctl restart nginx", "/generated/target.conf"} {
			if !tt.HasText(want) {
				t.Fatalf("width %d hid the operation content %q", width, want)
			}
		}
	}
	detail := historyUIFixture()
	detail.approvalHistory.detail = 0
	tt := ui.NewTester(detail.approvalView, 1100, 760)
	for _, want := range []string{"详情", "完整命令", "systemctl restart nginx", "请求 ID", "generated-request"} {
		if !tt.HasText(want) {
			t.Fatal("chosen record was not explained", want, tt.Texts())
		}
	}
	if pager, ok := tt.Find("更早记录"); !ok || pager.Y+pager.H > 761 {
		t.Fatal("history detail pushed the pager out of the viewport", pager)
	}
	narrow := historyUIFixture()
	narrow.approvalHistory.detail = 0
	nt := ui.NewTester(narrow.approvalView, 560, 760)
	if !nt.HasText("完整命令") {
		t.Fatal("the narrow layout hid the chosen command")
	}
	if pager, ok := nt.Find("更早记录"); !ok || pager.Y+pager.H > 761 {
		t.Fatal("narrow history detail pushed the pager out of the viewport", pager)
	}
	// A stale choice past the end of a reloaded page is dropped, not explained.
	stale := historyUIFixture()
	stale.approvalHistory.detail = 7
	st := ui.NewTester(stale.approvalView, 1100, 760)
	if st.HasText("完整命令") || stale.approvalHistory.detail != -1 {
		t.Fatal("a stale selection survived the page")
	}
}
