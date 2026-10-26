//go:build preview_qa

package desktop

import (
	"context"
	"image/png"
	"os"
	"path/filepath"
	"testing"

	"github.com/egoist/mygo/ui"
	"sshclient/internal/systemauth"
)

func TestVaultLandingSnapshotsWithoutWindows(t *testing.T) {
	if os.Getenv("SSH_CLIENT_PREVIEW_QA") != "1" {
		t.Skip("opt-in offline snapshots")
	}
	dir := os.Getenv("SSH_CLIENT_UI_SNAPSHOT_DIR")
	if !filepath.IsAbs(dir) {
		t.Fatal("absolute snapshot directory required")
	}
	if err := os.MkdirAll(dir, 0700); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"light", "dark", "password", "password-dark", "password-only", "create", "error", "busy", "small", "password-small"} {
		a := &app{ctx: context.Background(), create: name == "create"}
		if name != "password-only" {
			a.authStatus = systemauth.Status{Enabled: true, Available: true, Name: "Touch ID"}
		}
		state := a.ensureUnlockState()
		if name == "password" || name == "password-dark" || name == "password-small" || name == "error" {
			state.passwordMode = true
		}
		if name == "error" {
			a.unlockError = "主密码不正确，请重新输入。"
		}
		if name == "busy" {
			a.busy, state.busy, state.systemAttempt = true, true, true
		}
		width, height := 1280, 900
		if name == "small" || name == "password-small" {
			width, height = 1000, 760
		}
		tt := ui.NewTester(a.view, width, height)
		tt.SetDark(name == "dark" || name == "password-dark")
		path := filepath.Join(dir, "vault-"+name+".png")
		f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0600)
		if err != nil {
			t.Fatal(err)
		}
		err = png.Encode(f, tt.Image())
		closeErr := f.Close()
		if err != nil || closeErr != nil {
			t.Fatal(err, closeErr)
		}
		t.Log(path)
	}
}
