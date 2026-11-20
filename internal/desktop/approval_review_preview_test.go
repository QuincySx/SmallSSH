//go:build preview_qa

package desktop

import (
	"image/png"
	"os"
	"path/filepath"
	"testing"

	"github.com/egoist/mygo/ui"
	"sshclient/internal/approval"
	"sshclient/internal/systemauth"
)

func TestApprovalPromptSnapshotsWithoutWindows(t *testing.T) {
	if os.Getenv("SSH_CLIENT_PREVIEW_QA") != "1" {
		t.Skip("opt-in offline rendering")
	}
	dir := os.Getenv("SSH_CLIENT_UI_SNAPSHOT_DIR")
	if !filepath.IsAbs(dir) {
		t.Fatal("absolute output directory required")
	}
	if err := os.MkdirAll(dir, 0700); err != nil {
		t.Fatal(err)
	}
	for _, variant := range []string{"read", "protected-read", "scoped", "scoped-details", "full", "full-protected", "details", "peer", "long-session", "no-idle-renewal", "full-dark", "full-narrow", "full-english", "command", "renewal", "renewal-narrow", "password", "password-dark", "password-waiting", "password-only", "busy", "error", "guarded", "guarded-dark", "guarded-scoped", "guarded-medium-auto", "operation-review", "operation-review-dark", "operation-review-english", "operation-review-write", "maintenance"} {
		t.Run(variant, func(t *testing.T) {
			if variant == "full-english" || variant == "operation-review-english" {
				t.Setenv("MYGO_LOCALE", "en")
			}
			a, r := reviewFixture("full")
			if variant == "maintenance" {
				r.Operation.Grant.Permission = approval.MaintenancePermission
				r.Operation.Grant.ReadRoot = "/srv/backups/archive.enc"
				r.Operation.Grant.LifetimeSeconds = 3600
				r.Peer.Bundle = "test.smallssh.client"
			}
			if variant == "read" || variant == "protected-read" {
				a, r = reviewFixture("read")
			}
			if variant == "protected-read" {
				r.Operation.PathProtection = true
			}
			if variant == "scoped" || variant == "scoped-details" {
				r.Operation.Grant.ReadRoot, r.Operation.Grant.WriteRoot = "/srv/project", "/srv/project"
				r.Operation.PathProtection = true
			}
			if variant == "full-protected" {
				r.Operation.PathProtection = true
			}
			if variant == "command" {
				r.Operation.Kind = "exec"
				r.Operation.Command = "docker compose -p website restart --timeout 10 web"
			}
			if variant == "guarded" || variant == "guarded-dark" || variant == "guarded-scoped" || variant == "guarded-medium-auto" {
				r.Operation.Grant.Permission = "guarded"
				if variant == "guarded-scoped" {
					r.Operation.Grant.ReadRoot, r.Operation.Grant.WriteRoot = "/srv/project", "/srv/project"
				}
			}
			if variant == "operation-review" || variant == "operation-review-dark" || variant == "operation-review-english" || variant == "operation-review-write" {
				r.ParentID = "offline-guarded-session"
				r.Operation.Kind, r.Operation.Grant = "exec", approval.GrantScope{}
				r.Operation.Command = "systemctl restart nginx"
				if variant == "operation-review-write" {
					r.Operation.Kind, r.Operation.Command, r.Operation.Path = "replace", "", "/srv/project/config.yaml"
					r.Operation.ContentHash = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
					r.Operation.OriginalHash = "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
				}
			}
			width := 420
			if variant == "renewal" || variant == "renewal-narrow" {
				r.Renewal = true
			}
			if variant == "full-narrow" || variant == "renewal-narrow" || (variant == "full-english" || variant == "operation-review-english") {
				width = 380
			}
			a.authStatus = systemauth.Status{Enabled: true, Available: true, Name: "Touch ID"}
			state := &approvalPromptState{}
			if variant == "guarded-medium-auto" {
				a.initializeApprovalGrant(r, state)
				state.skipMediumReview = true
			}
			if variant == "long-session" {
				state.lifetime = 24 * 3600
			}
			if variant == "no-idle-renewal" {
				r.Renewal = true
				r.Operation.Grant.LifetimeSeconds = approval.MaxGrantLifetime
				r.Operation.Grant.IdleSeconds = approval.MaxGrantLifetime
			}
			if variant == "password" || variant == "password-dark" || variant == "password-waiting" {
				state.passwordMode = true
			}
			if variant == "password-only" {
				a.authStatus = systemauth.Status{}
			}
			if variant == "busy" || variant == "password-waiting" {
				state.busy = true
				state.systemAttempt = true
				state.cancel = func() {}
			}
			if variant == "error" {
				state.status = "系统验证已取消，可重试或使用保险库主密码。"
			}
			tt := ui.NewTester(func(c *ui.Context) { a.approvalPrompt(c, r, state, func(bool, bool) {}, func() {}) }, width, approvalWindowHeight)
			tt.SetDark(variant == "full-dark" || variant == "renewal-narrow" || variant == "guarded-dark" || variant == "operation-review-dark" || variant == "password-dark")
			if variant == "full-english" || variant == "operation-review-english" {
				tt.SetLocale("en")
				labels := []string{"Server actions", "Deny", "15 minutes", "Use Password", "Touch ID"}
				if variant == "operation-review-english" {
					labels = []string{"Server actions", "Deny", "Review operation · Review this command", "systemctl restart nginx", "Use Password", "Touch ID"}
				}
				for _, label := range labels {
					r, ok := tt.Find(label)
					if !ok || r.X < 0 || r.X+r.W > float32(width) || r.Y < 0 || r.Y+r.H > float32(approvalWindowHeight) {
						t.Fatal("English approval control clipped", label, r)
					}
				}
				password, _ := tt.Find("Use Password")
				fingerprint, _ := tt.Find("Touch ID")
				if fingerprint.W != 40 || fingerprint.H != 40 {
					t.Fatal("English approval shrank the Touch ID slot", fingerprint)
				}
				deny, _ := tt.Find("Deny")
				if password.Y+password.H <= deny.Y || deny.Y+deny.H <= password.Y {
					t.Fatal("English authentication and decisions wrapped onto separate rows", password, deny)
				}
			}
			if variant == "peer" {
				r.Peer = approval.Peer{PID: 48815, Path: "/Applications/Orca.app/Contents/MacOS/Orca", Bundle: "com.stablyai.orca"}
				state = &approvalPromptState{}
				tt = ui.NewTester(func(c *ui.Context) { a.approvalPrompt(c, r, state, func(bool, bool) {}, func() {}) }, width, approvalWindowHeight)
				if err := tt.Click("详情"); err != nil {
					t.Fatal(err)
				}
			}
			if variant == "details" || variant == "scoped-details" {
				if err := tt.Click("详情"); err != nil {
					t.Fatal(err)
				}
			}
			f, err := os.Create(filepath.Join(dir, "approval-"+variant+".png"))
			if err != nil {
				t.Fatal(err)
			}
			err = png.Encode(f, tt.Image())
			closed := f.Close()
			if err != nil || closed != nil {
				t.Fatal(err, closed)
			}
		})
	}
}
