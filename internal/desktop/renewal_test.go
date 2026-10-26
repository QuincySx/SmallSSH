package desktop

import (
	"testing"
	"time"

	"github.com/egoist/mygo/ui"
	"sshclient/internal/approval"
	"sshclient/internal/systemauth"
)

func TestRenewalGenerationNotifiesOnceWithSameClientAndSessionID(t *testing.T) {
	a, r, now := popupFixture()
	r.Nonce = "original-generation"
	a.requests = []approval.Request{r}
	if _, ok := a.nextApprovalPopup(now); !ok {
		t.Fatal("initial grant did not notify")
	}
	renewal := r
	renewal.Renewal = true
	renewal.Nonce = "renewed-generation"
	renewal.Expires = now.Add(20 * time.Minute)
	a.requests = []approval.Request{renewal}
	if got, ok := a.nextApprovalPopup(now.Add(16 * time.Minute)); !ok || got.Nonce != renewal.Nonce {
		t.Fatal("renewal was confused with an already displayed approval")
	}
	if _, ok := a.nextApprovalPopup(now.Add(17 * time.Minute)); ok {
		t.Fatal("same renewal prompted repeatedly")
	}
}

func TestRenewalApprovalMakesExpiredStateAndOriginalScopeClear(t *testing.T) {
	for _, permission := range []string{"read", "full"} {
		a, r := reviewFixture(permission)
		r.Renewal = true
		a.authStatus = systemauth.Status{Enabled: true, Available: true, Name: "Touch ID"}
		state := &approvalPromptState{}
		tt := ui.NewTester(func(c *ui.Context) { a.approvalPrompt(c, r, state, func(bool, bool) {}, func() {}) }, 380, 540)
		button := "续期只读访问"
		if permission == "full" {
			button = "续期完整控制"
		}
		for _, label := range []string{"会话已过期，允许续期？", "个人服务器", "deploy@192.0.2.10:22", "最长 2 小时 · 闲置 15 分钟 后失效", r.Operation.Grant.PairingCode(), "授权方式：触控 ID"} {
			bounds, ok := tt.Find(label)
			if !ok || bounds.X < 0 || bounds.X+bounds.W > 380 || bounds.Y+bounds.H > 540 {
				t.Fatal("renewal control missing or clipped", permission, label, bounds)
			}
		}
		if reason := approvalAuthReason(r); reason != button+" · deploy@192.0.2.10:22" {
			t.Fatal("OS prompt did not say renewal", reason)
		}
	}
}
