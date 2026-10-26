//go:build preview_qa

package desktop

import (
	"image/png"
	"os"
	"path/filepath"
	"testing"

	"github.com/egoist/mygo/ui"
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
	for _, variant := range []string{"read", "full", "full-dark", "full-narrow", "command", "renewal", "renewal-narrow", "password", "password-only", "busy", "error"} {
		t.Run(variant, func(t *testing.T) {
			a, r := reviewFixture("full")
			if variant == "read" {
				a, r = reviewFixture("read")
			}
			if variant == "command" {
				r.Operation.Kind = "exec"
				r.Operation.Command = "docker compose -p website restart --timeout 10 web"
			}
			width := 420
			if variant == "renewal" || variant == "renewal-narrow" {
				r.Renewal = true
			}
			if variant == "full-narrow" || variant == "renewal-narrow" {
				width = 380
			}
			a.authStatus = systemauth.Status{Enabled: true, Available: true, Name: "Touch ID"}
			state := &approvalPromptState{}
			if variant == "password" {
				state.passwordMode = true
			}
			if variant == "password-only" {
				a.authStatus = systemauth.Status{}
			}
			if variant == "busy" {
				state.busy = true
				state.cancel = func() {}
			}
			if variant == "error" {
				state.status = "系统验证已取消，可重试或使用保险库主密码。"
			}
			tt := ui.NewTester(func(c *ui.Context) { a.approvalPrompt(c, r, state, func(bool, bool) {}, func() {}) }, width, approvalWindowHeight)
			tt.SetDark(variant == "full-dark" || variant == "renewal-narrow")
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
