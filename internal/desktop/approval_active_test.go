package desktop

import (
	"testing"
	"time"

	"github.com/egoist/mygo/ui"
	"sshclient/internal/approval"
	"sshclient/internal/auditlog"
	"sshclient/internal/vault"
)

func TestActiveGrantIdleEditAndRevokeUseOwnerRPCAndPersistAudit(t *testing.T) {
	h := newConnectionFlowHarness(t)
	if err := h.a.client.SaveConnection(h.ctx, vault.ProfileChange{Profile: h.fixture.Profile()}, h.server.Vault); err != nil {
		t.Fatal(err)
	}
	r, delegation, err := h.a.client.RequestSession(h.ctx, "fixture", "offline audit actor", "read", time.Hour, 15*time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	defer delegation.Close()
	if err := h.a.client.Decide(h.ctx, r.ID, true, false, h.pass); err != nil {
		t.Fatal(err)
	}
	approved, err := h.server.Approvals.Get(r.ID)
	if err != nil {
		t.Fatal(err)
	}
	h.a.requests = []approval.Request{approved}
	h.a.approvalTab = 1
	h.a.identities = offlineIdentities()
	h.ui = ui.NewTester(h.a.approvalView, 1100, 800)
	h.click("调整闲置期限")
	if err := h.ui.ChooseMenuItem("闲置不撤销"); err != nil {
		t.Fatal(err)
	}
	h.wait(func() bool { return !h.a.busy })
	current, err := h.server.Approvals.Get(r.ID)
	if err != nil || current.Activity == nil || current.Activity.IdleSeconds != 0 || current.Expires != approved.Expires {
		t.Fatal("UI changed total deadline or did not apply idle rule", err, h.a.status)
	}
	page, err := h.a.client.History(h.ctx, auditlog.Query{Related: r.ID}, h.server.Vault)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, e := range page.Events {
		if e.Event == "grant_policy_changed" && e.PreviousIdle != nil && *e.PreviousIdle == 900 && e.IdleSeconds == 0 {
			found = true
		}
	}
	if !found {
		t.Fatal("idle edit was not auditable")
	}
	h.click("撤销授权：" + r.ID)
	h.wait(func() bool { return !h.a.busy })
	current, err = h.server.Approvals.Get(r.ID)
	if err != nil || current.Status != "revoked" {
		t.Fatal("UI did not revoke current authority", err)
	}
}

func TestLiveMediumReviewSwitchInActiveAndAuditDetails(t *testing.T) {
	h := newConnectionFlowHarness(t)
	if err := h.a.client.SaveConnection(h.ctx, vault.ProfileChange{Profile: h.fixture.Profile()}, h.server.Vault); err != nil {
		t.Fatal(err)
	}
	r, delegation, err := h.a.client.RequestSession(h.ctx, "fixture", "audit switch fixture", "guarded", time.Hour, 15*time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	defer delegation.Close()
	if err := h.a.client.Decide(h.ctx, r.ID, true, false, h.pass); err != nil {
		t.Fatal(err)
	}
	approved, err := h.server.Approvals.Get(r.ID)
	if err != nil {
		t.Fatal(err)
	}
	h.a.requests = []approval.Request{approved}
	h.a.approvalTab, h.a.identities = 1, offlineIdentities()
	h.ui = ui.NewTester(h.a.approvalView, 1100, 800)
	if h.ui.HasText(mediumReviewOffNotice) {
		t.Fatal("the note appeared while per-operation review was enabled")
	}
	h.click("中风险文件操作确认开关")
	h.wait(func() bool { return !h.a.busy && h.ui.HasText(mediumReviewOffNotice) })
	current, err := h.server.Approvals.Get(r.ID)
	if err != nil || current.ConfirmsMediumReview() {
		t.Fatal("live toggle stayed on", err, h.a.status)
	}
	page, err := h.a.client.History(h.ctx, auditlog.Query{Related: r.ID}, h.server.Vault)
	if err != nil {
		t.Fatal(err)
	}
	var record auditlog.Event
	for _, event := range page.Events {
		if event.Event == "grant_policy_changed" {
			record = event
			break
		}
	}
	if record.MediumReview == nil || *record.MediumReview || record.PreviousMediumReview == nil || !*record.PreviousMediumReview || record.Generation != r.Generation() {
		t.Fatal("audit did not record the rule change", record)
	}
	h.ui = ui.NewTester(func(c *ui.Context) { h.a.approvalHistoryDetail(c, record) }, 450, 800)
	if !h.ui.HasText("记录时确认中风险文件操作") || !h.ui.HasText("当前授权 · 确认中风险文件操作") {
		t.Fatal("history and current controls are missing", h.ui.Texts())
	}
	h.click("中风险文件操作确认开关")
	h.wait(func() bool { return !h.a.busy && !h.ui.HasText(mediumReviewOffNotice) })
	current, err = h.server.Approvals.Get(r.ID)
	if err != nil || !current.ConfirmsMediumReview() || *record.MediumReview {
		t.Fatal("re-enabling review rewrote historical state or did not take effect", err, h.a.status)
	}
	record.Generation = "older-generation"
	h.ui.Frame()
	if !h.ui.HasText("记录时确认中风险文件操作") || h.ui.HasText("当前授权 · 确认中风险文件操作") {
		t.Fatal("old generation appeared editable", h.ui.Texts())
	}
}
