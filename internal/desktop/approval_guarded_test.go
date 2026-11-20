package desktop

import (
	"testing"

	"github.com/egoist/mygo/ui"
	"sshclient/internal/approval"
	"sshclient/internal/systemauth"
)

func TestGuardedApprovalDescribesRiskRulesAndKeepsDecisionsVisible(t *testing.T) {
	a, r := reviewFixture("guarded")
	a.authStatus = systemauth.Status{Enabled: true, Available: true, Name: "Touch ID"}
	for _, dark := range []bool{false, true} {
		tt := ui.NewTester(func(c *ui.Context) { a.approvalPrompt(c, r, &approvalPromptState{}, func(bool, bool) {}, func() {}) }, 380, approvalWindowHeight)
		tt.SetDark(dark)
		for _, text := range []string{"限时受控", "低风险放行 · 中风险确认 · 高风险拒绝", "2 小时", r.Operation.Grant.PairingCode(), "拒绝", "触控 ID"} {
			r, ok := tt.Find(text)
			if !ok || r.H < 11 || r.X < 0 || r.X+r.W > 380 || r.Y+r.H > float32(approvalWindowHeight) {
				t.Fatal(text, r)
			}
		}
		if approvalActionLabel(r) != "允许限时受控" {
			t.Fatal("OS reason confused a guarded session with full access", approvalActionLabel(r))
		}
	}
}

func TestMaintenanceApprovalShowsVerifiedCallerAndBackupSource(t *testing.T) {
	a, r := reviewFixture(approval.MaintenancePermission)
	r.Operation.Grant.ReadRoot = "/srv/backups/archive.enc"
	r.Operation.Grant.LifetimeSeconds = 3600
	r.Peer.Bundle = "test.smallssh.client"
	a.authStatus = systemauth.Status{Enabled: true, Available: true, Name: "Touch ID"}
	tt := ui.NewTester(func(c *ui.Context) { a.approvalPrompt(c, r, &approvalPromptState{}, func(bool, bool) {}, func() {}) }, 380, approvalWindowHeight)
	for _, label := range []string{"test.smallssh.client", "系统检查 · 备份下载 · APT 升级预览", "/srv/backups/archive.enc", r.Operation.Grant.PairingCode(), "拒绝", "触控 ID"} {
		bounds, ok := tt.Find(label)
		if !ok || bounds.X < 0 || bounds.X+bounds.W > 380 || bounds.Y+bounds.H > float32(approvalWindowHeight) {
			t.Fatal(label, bounds)
		}
	}
}

func TestGuardedMediumFileReviewDraftDefaultsOnForEachGrant(t *testing.T) {
	a, r := reviewFixture("guarded")
	a.authStatus = systemauth.Status{Enabled: true, Available: true, Name: "Touch ID"}
	state := &approvalPromptState{}
	tt := ui.NewTester(func(c *ui.Context) { a.approvalPrompt(c, r, state, func(bool, bool) {}, func() {}) }, 380, approvalWindowHeight)
	const label = "确认中风险文件操作"
	if state.skipMediumReview || !tt.HasText(label) || tt.HasText(mediumReviewOffNotice) {
		t.Fatal("review was not visible and enabled by default")
	}
	if err := tt.Click("中风险文件操作确认开关"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	notice, visible := tt.Find(mediumReviewOffNotice)
	toggle, _ := tt.Find("中风险文件操作确认开关")
	deny, _ := tt.Find("拒绝")
	if !state.skipMediumReview || !visible || notice.Y < toggle.Y+toggle.H || notice.Y+notice.H > deny.Y {
		t.Fatal("opt-out notice is missing or outside the approval controls", notice, toggle, deny)
	}
	other := &approvalPromptState{}
	a.initializeApprovalGrant(r, other)
	if !other.skipMediumReview {
		t.Fatal("the other approval surface lost this grant's draft")
	}
	if err := tt.Click("中风险文件操作确认开关"); err != nil {
		t.Fatal(err)
	}
	if state.skipMediumReview || tt.HasText(mediumReviewOffNotice) {
		t.Fatal("re-enabling review left the opt-out notice visible")
	}
	r.Nonce = "fresh"
	a.initializeApprovalGrant(r, other)
	if other.skipMediumReview {
		t.Fatal("new grant inherited the opt-out")
	}
}

func TestOperationReviewShowsActualCommandOrPathBeforeConsent(t *testing.T) {
	for _, kind := range []string{"exec", "remove"} {
		a, r := reviewFixture("guarded")
		r.ParentID = "guarded-parent"
		r.Operation.Kind, r.Operation.Grant = kind, approval.GrantScope{}
		value := "systemctl restart nginx"
		if kind == "exec" {
			r.Operation.Command = value
		} else {
			value = "/srv/project/remove-me"
			r.Operation.Path = value
		}
		a.authStatus = systemauth.Status{Enabled: true, Available: true, Name: "Touch ID"}
		tt := ui.NewTester(func(c *ui.Context) { a.approvalPrompt(c, r, &approvalPromptState{}, func(bool, bool) {}, func() {}) }, 380, approvalWindowHeight)
		decision, ok := tt.Find("触控 ID")
		if !ok {
			t.Fatal("single-use consent missing")
		}
		bounds, ok := tt.Find(value)
		if !ok || bounds.H < 11 || bounds.X < 0 || bounds.X+bounds.W > 380 || bounds.Y+bounds.H > decision.Y {
			t.Fatal("operation hidden or clipped before approval", kind, bounds, decision)
		}
		if tt.HasText("允许操作") || tt.HasText("允许本次操作") || tt.HasText("授权时长") || tt.HasText("会话闲置时长") {
			t.Fatal("operation review offered wider session authority", tt.Texts())
		}
	}
}
