package desktop

import (
	"crypto/ed25519"
	"encoding/base64"
	"slices"
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

func TestApprovalSurfacesHaveOneAllowAndRefuseControl(t *testing.T) {
	for _, popup := range []bool{false, true} {
		for _, system := range []bool{false, true} {
			for _, password := range []bool{false, true} {
				a, r := reviewFixture("full")
				a.authStatus = systemauth.Status{Enabled: system, Available: system, Name: "Touch ID"}
				state := &approvalPromptState{passwordMode: password}
				tt := ui.NewTester(func(c *ui.Context) {
					if popup {
						a.approvalPrompt(c, r, state, func(bool, bool) {}, func() {})
					} else {
						a.approvalActions(c, r, state, func(bool, bool) {}, func() {})
					}
				}, 420, 620)
				for _, label := range []string{"拒绝", "允许操作"} {
					count := 0
					for _, text := range tt.Texts() {
						if text == label {
							count++
						}
					}
					// One button contributes its accessible label and its text.
					want := 2
					if label == "允许操作" && system && !password {
						want = 0
					}
					if count != want {
						t.Fatal("approval must have one control per decision", popup, system, password, label, count, tt.Texts())
					}
				}
				if bounds, ok := tt.Find("触控 ID"); ok != system || ok && (bounds.W != 40 || bounds.H != 40) {
					t.Fatal("system confirmation lost its fixed anchor", popup, system, password, bounds)
				}
			}
		}
	}
}

func TestApprovalPromptKeepsFourFactsAndDecisionsVisible(t *testing.T) {
	for _, permission := range []string{"read", "full"} {
		for _, width := range []int{380, 420} {
			for _, dark := range []bool{false, true} {
				for _, height := range []int{approvalWindowHeight, 620} {
					a, r := reviewFixture(permission)
					a.authStatus = systemauth.Status{Enabled: true, Available: true, Name: "Touch ID"}
					state := &approvalPromptState{}
					tt := ui.NewTester(func(c *ui.Context) { a.approvalPrompt(c, r, state, func(bool, bool) {}, func() {}) }, width, height)
					tt.SetDark(dark)
					for _, label := range []string{"服务器操作授权", "我的 AI 助手", "个人服务器 · deploy@192.0.2.10:22", "2 小时", r.Operation.Grant.PairingCode(), "拒绝"} {
						bounds, ok := tt.Find(label)
						if !ok || bounds.X < 0 || bounds.Y < 0 || bounds.X+bounds.W > float32(width)+1 || bounds.Y+bounds.H > float32(height) {
							t.Fatal("key decision is clipped", permission, width, dark, label, bounds)
						}
					}
					code, _ := tt.Find(r.Operation.Grant.PairingCode())
					deny, _ := tt.Find("拒绝")
					if code.H < 16 || code.Y+code.H > deny.Y {
						t.Fatal("pairing code lost emphasis or overlapped the decision row", code, deny)
					}
					if _, ok := tt.Find("取消"); ok {
						t.Fatal("a cancel button duplicated the refusal")
					}
					if tt.HasText(r.Operation.HostFingerprint) || tt.HasText(r.ID) {
						t.Fatal("protocol diagnostics cluttered the initial decision")
					}
					if _, ok := tt.Find("名称由申请端提供，请核对配对码"); ok {
						t.Fatal("note text was kept in the compact header")
					}
					if permission == "read" && !tt.HasText("只读访问") || permission == "full" && !tt.HasText("完整控制") {
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
	}, 420, 620)
	if err := tt.Click("允许操作"); err != nil {
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
	tt.Key(0, ui.KeyEnter)
	if allowed != 1 || system != 0 {
		t.Fatal("password decision route missing")
	}
	state.busy = true
	tt.Frame()
	tt.Key(0, ui.KeyEnter)
	if allowed != 1 || denied != 0 || postponed != 0 {
		t.Fatal("busy prompt accepted a second approval")
	}
	state.busy = false
	state.password = ""
	a.authStatus = systemauth.Status{Enabled: true, Available: true, Name: "Touch ID"}
	tt.Frame()
	if err := tt.Click("触控 ID"); err != nil {
		t.Fatal(err)
	}
	if allowed != 2 || system != 1 || state.passwordMode {
		t.Fatal("the sign-in segment did not switch back to biometrics")
	}
	if err := tt.Click("拒绝"); err != nil {
		t.Fatal(err)
	}
	tt.Key(0, ui.KeyEscape)
	if denied != 1 || postponed != 1 {
		t.Fatal("non-approval actions missing")
	}
}

func TestApprovalPromptSwitchesAuthenticationWithoutRetainingPassword(t *testing.T) {
	for _, enter := range []bool{false} {
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
		}, 380, 620)
		if _, ok := tt.Find("保险库主密码"); ok || systemAttempts != 0 {
			t.Fatal("render started authentication or revealed password")
		}
		state.busy, state.cancel = true, func() { cancelled++ }
		tt.Frame()
		if err := tt.Click("改用保险库主密码"); err != nil {
			t.Fatal(err)
		}
		if systemAttempts != 0 || passwordAttempts != 0 || !state.passwordMode || state.busy || cancelled != 1 {
			t.Fatal("default action did not expand password and cancel system attempt", enter, cancelled)
		}
		for _, label := range []string{"保险库主密码", "触控 ID", r.Operation.Grant.PairingCode(), "只读访问"} {
			b, ok := tt.Find(label)
			if !ok || b.Y < 0 || b.Y+b.H > 620 {
				t.Fatal("fallback hid a decision or pairing code", label, b)
			}
		}
		input, _ := tt.Find("保险库主密码")
		fingerprint, _ := tt.Find("触控 ID")
		eye, eyeVisible := tt.Find("保险库主密码明文显示")
		if !eyeVisible || fingerprint.W != 40 || fingerprint.H != 40 ||
			input.X+input.W > eye.X+1 || eye.X+eye.W > fingerprint.X ||
			fingerprint.Y+fingerprint.H/2 < input.Y || fingerprint.Y+fingerprint.H/2 > input.Y+input.H {
			t.Fatal("the Touch ID button is not at the end of the password field", input, eye, fingerprint)
		}
		tt.Type("synthetic-fallback-password")
		tt.Key(0, ui.KeyEnter)
		if passwordAttempts != 1 || systemAttempts != 0 {
			t.Fatal("password Enter used the wrong route")
		}
		if err := tt.Click("触控 ID"); err != nil {
			t.Fatal(err)
		}
		if systemAttempts != 1 || !state.passwordMode || state.password != "" {
			t.Fatal("the in-field action did not start system verification and clear the password")
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
		tt := ui.NewTester(func(c *ui.Context) { a.approvalPrompt(c, r, state, func(bool, bool) {}, func() {}) }, 380, 620)
		for _, label := range []string{"保险库主密码", "拒绝"} {
			b, ok := tt.Find(label)
			if !ok || b.Y < 0 || b.Y+b.H > 620 {
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
	tt := ui.NewTester(func(c *ui.Context) { a.approvalPrompt(c, r, state, func(bool, bool) {}, func() {}) }, 420, 620)
	if !tt.HasText("Trusted\\nAI\\u202e") || tt.HasText("配对码") {
		t.Fatal("one-time requester identity was misleading", tt.Texts())
	}
	command, ok := tt.Find("printf 'safe'\\nrm -f -- /srv/example")
	footer, _ := tt.Find("拒绝")
	if !ok || command.W <= 0 || command.H <= 0 || command.Y+command.H > footer.Y {
		t.Fatal("exact command hidden or overlapped authentication", command, footer)
	}
	r.Operation.Command = strings.Repeat("printf 'long-command'; ", 1200)
	tt.Frame()
	for _, label := range []string{"拒绝", "允许操作"} {
		bounds, ok := tt.Find(label)
		if !ok || bounds.Y+bounds.H > 620 {
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
	if err := tt.Click("2. 我的 AI 助手 · 服务器操作会话授权"); err != nil {
		t.Fatal(err)
	}
	if a.selectedRequest != second.ID || a.ownerPass != "" || !tt.HasText(second.Operation.Endpoint) || tt.HasText(first.Operation.Endpoint) {
		t.Fatal("request selection reused a password or ambiguous caller label", a.selectedRequest, tt.Texts())
	}
	for _, label := range []string{"拒绝", "允许操作"} {
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
	if err := tt.Click("改用保险库主密码"); err != nil {
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
	for _, label := range []string{"拒绝", "触控 ID"} {
		if _, ok := tt.Find(label); !ok {
			t.Fatal("approval center does not use the same consent flow as the popup", label)
		}
	}
}

func TestSystemApprovalReasonNamesAccessAndServerWithoutClaimedIdentity(t *testing.T) {
	_, r := reviewFixture("full")
	r.Operation.Client = "Verified Vendor Claim\nspoof"
	if got := approvalAuthReason(r); got != "服务器操作授权 · 允许完整控制 · deploy@192.0.2.10:22" {
		t.Fatal("OS prompt did not identify access and server", got)
	}
	r.Operation.Endpoint = strings.Repeat("x", 300) + "\nspoof"
	if got := approvalAuthReason(r); len([]rune(got)) > 120 || strings.Contains(got, "\n") {
		t.Fatal("unbounded OS prompt", got)
	}
}

func TestScopedRootsAndReadProtectionAreVisibleInBothReviewSurfaces(t *testing.T) {
	a, r := reviewFixture("read")
	r.Operation.PathProtection = true
	if _, _, caution := approvalPermission(r); caution != "保护路径不可读取；其余路径仍受账户权限限制" {
		t.Fatal("read protection was hidden", caution)
	}
	r.Operation.Grant.ReadRoot = "/srv/project"
	if got := approvalAuthReason(r); got != "服务器操作授权 · 允许限定目录访问 · deploy@192.0.2.10:22" {
		t.Fatal("OS prompt overstated scoped access", got)
	}
	for _, compact := range []bool{false, true} {
		tt := ui.NewTester(func(c *ui.Context) {
			if compact {
				a.compactApprovalReview(c, r)
			} else {
				requestReview(c, r)
			}
		}, 540, 1100)
		for _, text := range []string{"限定目录访问", "读取根目录", "/srv/project", "写入根目录", "不开放", "不含命令、终端、状态采样或容器日志"} {
			if !tt.HasText(text) {
				t.Fatal("scope detail missing", compact, text, tt.Texts())
			}
		}
	}
}

func TestScopedPromptKeepsRootsPairingAndDecisionVisible(t *testing.T) {
	for _, width := range []int{380, 420} {
		a, r := reviewFixture("full")
		r.Operation.Grant.ReadRoot, r.Operation.Grant.WriteRoot = "/srv/read", "/srv/write"
		a.authStatus = systemauth.Status{Enabled: true, Available: true, Name: "Touch ID"}
		tt := ui.NewTester(func(c *ui.Context) { a.approvalPrompt(c, r, &approvalPromptState{}, func(bool, bool) {}, func() {}) }, width, approvalWindowHeight)
		if err := tt.Click("详情"); err != nil {
			t.Fatal(err)
		}
		footer, ok := tt.Find("拒绝")
		if !ok {
			t.Fatal("authentication missing")
		}
		for _, label := range []string{"/srv/read", "/srv/write", r.Operation.Grant.PairingCode()} {
			b, ok := tt.Find(label)
			if !ok || b.H < 11 || b.Y < 0 || b.Y+b.H > footer.Y {
				t.Fatal("scope or pairing clipped by authentication", width, label, b, footer)
			}
		}
		for _, label := range []string{"拒绝"} {
			b, ok := tt.Find(label)
			if !ok || b.Y+b.H > float32(approvalWindowHeight) {
				t.Fatal("decision clipped", width, label, b)
			}
		}
	}
}

func TestOwnerChoosesSessionDurationInThePrompt(t *testing.T) {
	a, r := reviewFixture("full")
	a.authStatus = systemauth.Status{Enabled: true, Available: true, Name: "Touch ID"}
	r.Operation.Grant.LifetimeSeconds = 45 * 60
	state := &approvalPromptState{}
	tt := ui.NewTester(func(c *ui.Context) {
		a.approvalPrompt(c, r, state, func(bool, bool) {}, func() {})
	}, 420, 620)
	if _, ok := tt.Find("授权时长"); !ok {
		t.Fatal("duration choice missing", tt.Texts())
	}
	want := []int{15 * 60, 30 * 60, 3600, 2 * 3600, 3 * 3600, 6 * 3600, 9 * 3600, 12 * 3600, 24 * 3600}
	if got := grantLifetimeOptions(); !slices.Equal(got, want) {
		t.Fatal("duration options", got)
	}
	if state.chosenLifetime(r) != 6*3600 {
		t.Fatal("a non-standard request should start at six hours", state.chosenLifetime(r))
	}
	state.lifetime = 24 * 3600
	tt.Frame()
	if !tt.HasText("24 小时") {
		t.Fatal("the review did not follow the chosen duration", tt.Texts())
	}
	// The owner's own choice needs no second confirmation.
	if _, ok := tt.Find("允许超过 6 小时"); ok {
		t.Fatal("a 24 hour choice asked for another confirmation")
	}
	if _, ok := tt.Find("无人值守"); ok {
		t.Fatal("an extra unattended checkbox remained", tt.Texts())
	}
	if err := tt.Click("改用保险库主密码"); err != nil {
		t.Fatal(err)
	}
	state.password = "synthetic-long-session"
	tt.Frame()
	if _, ok := tt.Find("允许操作"); !ok {
		t.Fatal("password decision missing")
	}
}

func TestOneShotRequestsDoNotOfferADuration(t *testing.T) {
	a, r := reviewFixture("full")
	r.Operation.Kind = "exec"
	r.Operation.Grant = approval.GrantScope{}
	r.Operation.Command = "systemctl restart nginx"
	state := &approvalPromptState{}
	tt := ui.NewTester(func(c *ui.Context) {
		a.approvalPrompt(c, r, state, func(bool, bool) {}, func() {})
	}, 420, 620)
	if _, ok := tt.Find("授权时长"); ok {
		t.Fatal("a one-shot command offered a session duration")
	}
}
