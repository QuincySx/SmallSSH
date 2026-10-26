package desktop

import (
	"crypto/ed25519"
	"encoding/base64"
	"strings"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"
	"sshclient/internal/approval"
	"sshclient/internal/systemauth"
	"sshclient/internal/vault"
)

func reviewFixture(permission string) (*app, approval.Request) {
	key := ed25519.NewKeyFromSeed(make([]byte, ed25519.SeedSize)).Public().(ed25519.PublicKey)
	a := &app{connections: []vault.Summary{{ID: "fixture", Name: "个人服务器"}}}
	r := approval.Request{ID: "offline-review-fixture", Status: "pending", Expires: time.Date(2026, 10, 21, 12, 5, 0, 0, time.UTC), Operation: approval.Operation{Host: "fixture", Endpoint: "deploy@192.0.2.10:22", HostFingerprint: "SHA256:OFFLINE_FIXTURE", Client: "我的 AI 助手", Kind: "grant", Grant: approval.GrantScope{PublicKey: base64.RawURLEncoding.EncodeToString(key), Permission: permission, LifetimeSeconds: 7200, IdleSeconds: 900}}}
	return a, r
}

func TestApprovalPromptKeepsFourFactsAndDecisionsVisible(t *testing.T) {
	for _, permission := range []string{"read", "full"} {
		for _, width := range []int{380, 420} {
			for _, dark := range []bool{false, true} {
				for _, height := range []int{540, 560} {
					a, r := reviewFixture(permission)
					a.authStatus = systemauth.Status{Enabled: true, Available: true, Name: "Touch ID"}
					state := &approvalPromptState{}
					tt := ui.NewTester(func(c *ui.Context) { a.approvalPrompt(c, r, state, func(bool, bool) {}, func() {}) }, width, height)
					tt.SetDark(dark)
					for _, label := range []string{"应用图标", "我的 AI 助手", "个人服务器", "deploy@192.0.2.10:22", "最长 2 小时 · 闲置 15 分钟 后失效", r.Operation.Grant.PairingCode(), "不允许", "授权方式：触控 ID", "取消"} {
						bounds, ok := tt.Find(label)
						if !ok || bounds.X < 0 || bounds.Y < 0 || bounds.X+bounds.W > float32(width)+1 || bounds.Y+bounds.H > float32(height) {
							t.Fatal("key decision is clipped", permission, width, dark, label, bounds)
						}
					}
					methodTexts := 0
					for _, text := range tt.Texts() {
						if text == "授权方式：触控 ID" {
							methodTexts++
						}
					}
					// Tester.Texts includes the accessible button name and the
					// single child Text. A second painted label makes three.
					if methodTexts != 2 {
						t.Fatal("authentication method label was duplicated", methodTexts, tt.Texts())
					}
					if tt.HasText(r.Operation.HostFingerprint) || tt.HasText(r.ID) {
						t.Fatal("protocol diagnostics cluttered the initial decision")
					}
					if !tt.HasText("名称由申请端提供，请核对配对码") {
						t.Fatal("caller label was presented as a verified identity")
					}
					if permission == "read" && !tt.HasText("可读取此账户有权访问的所有路径") || permission == "full" && !tt.HasText("可使用此 SSH 账户的全部权限") {
						t.Fatal("scope boundary was hidden")
					}
					for _, label := range []string{"使用 Touch ID", "使用密码", "使用主密码…", "保险库主密码"} {
						if _, ok := tt.Find(label); ok {
							t.Fatal("review page duplicated or confused OS verification", label)
						}
					}
				}
			}
		}
	}
}

func TestApprovalPromptDoesNotApproveWithAnEmptyPasswordOrWhileBusy(t *testing.T) {
	a, r := reviewFixture("full")
	state := &approvalPromptState{}
	var allowed, denied, system, postponed int
	tt := ui.NewTester(func(c *ui.Context) {
		a.approvalPrompt(c, r, state, func(allow, osAuth bool) {
			if allow {
				allowed++
			} else {
				denied++
			}
			if osAuth {
				system++
			}
		}, func() { postponed++ })
	}, 420, 560)
	if err := tt.Click("允许"); err != nil {
		t.Fatal(err)
	}
	if allowed != 0 {
		t.Fatal("empty password approved an external client")
	}
	if err := tt.Click("保险库主密码"); err != nil {
		t.Fatal(err)
	}
	tt.Type("offline-synthetic-pass")
	if tt.HasText("offline-synthetic-pass") {
		t.Fatal("password was visible")
	}
	if err := tt.Click("允许"); err != nil {
		t.Fatal(err)
	}
	if allowed != 1 || system != 0 {
		t.Fatal("password decision route missing")
	}
	state.busy = true
	tt.Frame()
	if err := tt.Click("允许"); err != nil {
		t.Fatal(err)
	}
	if allowed != 1 || denied != 0 || postponed != 0 {
		t.Fatal("busy prompt accepted a second approval")
	}
	state.busy = false
	state.password = ""
	a.authStatus = systemauth.Status{Enabled: true, Available: true, Name: "Touch ID"}
	tt.Frame()
	if err := tt.Click("授权方式：触控 ID"); err != nil {
		t.Fatal(err)
	}
	if err := tt.Click("触控 ID"); err != nil {
		t.Fatal(err)
	}
	if allowed != 2 || system != 1 || !state.passwordMode {
		t.Fatal("direct system authentication did not preserve password mode")
	}
	if err := tt.Click("不允许"); err != nil {
		t.Fatal(err)
	}
	tt.Key(0, ui.KeyEscape)
	if denied != 1 || postponed != 1 {
		t.Fatal("non-approval actions missing")
	}
}

func TestApprovalPromptSwitchesAuthenticationWithoutRetainingPassword(t *testing.T) {
	for _, enter := range []bool{false, true} {
		a, r := reviewFixture("read")
		a.authStatus = systemauth.Status{Enabled: true, Available: true, Name: "Touch ID"}
		state := &approvalPromptState{}
		var passwordAttempts, systemAttempts, cancelled int
		tt := ui.NewTester(func(c *ui.Context) {
			a.approvalPrompt(c, r, state, func(allow, system bool) {
				if allow && system {
					systemAttempts++
				} else if allow {
					passwordAttempts++
				}
			}, func() {})
		}, 380, 540)
		if _, ok := tt.Find("保险库主密码"); ok || systemAttempts != 0 {
			t.Fatal("render started authentication or revealed password")
		}
		state.busy, state.cancel = true, func() { cancelled++ }
		tt.Frame()
		if enter {
			tt.Key(0, ui.KeyEnter)
		} else if err := tt.Click("授权方式：触控 ID"); err != nil {
			t.Fatal(err)
		}
		if systemAttempts != 0 || passwordAttempts != 0 || !state.passwordMode || state.busy || cancelled != 1 {
			t.Fatal("default action did not expand password and cancel system attempt", enter, cancelled)
		}
		for _, label := range []string{"保险库主密码", "触控 ID", "允许", r.Operation.Grant.PairingCode(), "可读取此账户有权访问的所有路径"} {
			b, ok := tt.Find(label)
			if !ok || b.Y < 0 || b.Y+b.H > 540 {
				t.Fatal("fallback hid a decision or pairing code", label, b)
			}
		}
		tt.Type("synthetic-fallback-password")
		tt.Key(0, ui.KeyEnter)
		if passwordAttempts != 1 || systemAttempts != 0 {
			t.Fatal("password Enter used the wrong route")
		}
		if err := tt.Click("触控 ID"); err != nil {
			t.Fatal(err)
		}
		if systemAttempts != 1 || !state.passwordMode {
			t.Fatal("fingerprint button did not directly verify in password mode")
		}
		if _, ok := tt.Find("返回系统验证"); ok {
			t.Fatal("two-step system verification returned")
		}
		state.busy, state.cancel = true, func() { cancelled++ }
		tt.Frame()
		tt.Key(0, ui.KeyEscape)
		if cancelled != 2 || state.busy || !state.passwordMode {
			t.Fatal("Escape lost expanded password mode")
		}
	}
}

func TestApprovalPromptKeepsFallbackAvailableWithoutBiometrics(t *testing.T) {
	for _, enabled := range []bool{false, true} {
		a, r := reviewFixture("full")
		a.authStatus = systemauth.Status{Enabled: enabled, Available: false, Name: "Touch ID"}
		state := &approvalPromptState{}
		tt := ui.NewTester(func(c *ui.Context) { a.approvalPrompt(c, r, state, func(bool, bool) {}, func() {}) }, 380, 540)
		for _, label := range []string{"保险库主密码", "允许", "不允许", r.Operation.Grant.PairingCode()} {
			b, ok := tt.Find(label)
			if !ok || b.Y < 0 || b.Y+b.H > 540 {
				t.Fatal("unavailable OS verification blocked password fallback", label, b)
			}
		}
		if _, ok := tt.Find("使用 Touch ID"); ok {
			t.Fatal("unavailable authentication was advertised")
		}
	}
}

func TestOneTimeReviewKeepsExactCommandAndDoesNotInventPairingCode(t *testing.T) {
	a, r := reviewFixture("read")
	r.Operation.Kind = "exec"
	r.Operation.Grant = approval.GrantScope{}
	r.Operation.Client = "Trusted\nAI\u202e"
	r.Operation.Command = "printf 'safe'\nrm -f -- /srv/example"
	state := &approvalPromptState{}
	tt := ui.NewTester(func(c *ui.Context) { a.approvalPrompt(c, r, state, func(bool, bool) {}, func() {}) }, 420, 560)
	if !tt.HasText("名称由申请端提供，未经身份验证") || !tt.HasText("Trusted\\nAI\\u202e") || tt.HasText("配对码") {
		t.Fatal("one-time requester identity was misleading", tt.Texts())
	}
	command, ok := tt.Find("printf 'safe'\\nrm -f -- /srv/example")
	footer, _ := tt.Find("保险库主密码")
	if !ok || command.W <= 0 || command.H <= 0 || command.Y+command.H > footer.Y {
		t.Fatal("exact command hidden or overlapped authentication", command, footer)
	}
	r.Operation.Command = strings.Repeat("printf 'long-command'; ", 1200)
	tt.Frame()
	for _, label := range []string{"保险库主密码", "不允许", "允许"} {
		bounds, ok := tt.Find(label)
		if !ok || bounds.Y+bounds.H > 560 {
			t.Fatal("long request pushed decision offscreen", label, bounds)
		}
	}
}

func TestNarrowApprovalCenterSelectsExactRequestAndClearsPassword(t *testing.T) {
	a, first := reviewFixture("read")
	second := first
	second.ID = "second-request-with-same-claimed-name"
	second.Operation.Endpoint = "deploy@192.0.2.20:2222"
	a.requests = []approval.Request{first, second}
	a.selectedRequest = first.ID
	a.ownerPass = "synthetic-prior-confirmation"
	tt := ui.NewTester(a.approvalView, 560, 760)
	if err := tt.Click("选择授权请求"); err != nil {
		t.Fatal(err)
	}
	if err := tt.Click("2. 我的 AI 助手 · 客户端会话授权"); err != nil {
		t.Fatal(err)
	}
	if a.selectedRequest != second.ID || a.ownerPass != "" || !tt.HasText(second.Operation.Endpoint) || tt.HasText(first.Operation.Endpoint) {
		t.Fatal("request selection reused a password or ambiguous caller label", a.selectedRequest, tt.Texts())
	}
	for _, label := range []string{"保险库主密码", "不允许", "允许"} {
		bounds, ok := tt.Find(label)
		if !ok || bounds.X < 0 || bounds.X+bounds.W > 560 || bounds.Y+bounds.H > 760 {
			t.Fatal("narrow center clipped action", label, bounds)
		}
	}
}

func TestApprovalCenterDropsFallbackPasswordOnRenewalGenerationChange(t *testing.T) {
	a, r := reviewFixture("full")
	a.authStatus = systemauth.Status{Enabled: true, Available: true, Name: "Touch ID"}
	r.Nonce = "original-generation"
	a.sidebarHidden = true
	a.requests = []approval.Request{r}
	tt := ui.NewTester(a.approvalView, 560, 760)
	if _, ok := tt.Find("保险库主密码"); ok {
		t.Fatal("approval center required choosing an authentication method")
	}
	if err := tt.Click("授权方式：触控 ID"); err != nil {
		t.Fatal(err)
	}
	if err := tt.Click("保险库主密码"); err != nil {
		t.Fatal(err)
	}
	tt.Type("synthetic-review-password")
	if a.ownerPass == "" || !a.approvalPasswordMode {
		t.Fatal("fallback input did not reach approval-center state")
	}
	a.requests[0].Nonce = "renewed-generation"
	a.requests[0].Renewal = true
	tt.Frame()
	if a.ownerPass != "" || a.approvalPasswordMode {
		t.Fatal("renewal generation retained an earlier authentication choice or password")
	}
	if _, ok := tt.Find("保险库主密码"); ok {
		t.Fatal("renewal generation did not restore OS verification")
	}
	for _, label := range []string{"授权方式：触控 ID", "不允许", "取消"} {
		if _, ok := tt.Find(label); !ok {
			t.Fatal("approval center does not use the same consent flow as the popup", label)
		}
	}
}

func TestSystemApprovalReasonNamesAccessAndServerWithoutClaimedIdentity(t *testing.T) {
	_, r := reviewFixture("full")
	r.Operation.Client = "Verified Vendor Claim\nspoof"
	if got := approvalAuthReason(r); got != "允许完整控制 · deploy@192.0.2.10:22" {
		t.Fatal("OS prompt did not identify access and server", got)
	}
	r.Operation.Endpoint = strings.Repeat("x", 300) + "\nspoof"
	if got := approvalAuthReason(r); len([]rune(got)) > 120 || strings.Contains(got, "\n") {
		t.Fatal("unbounded OS prompt", got)
	}
}
