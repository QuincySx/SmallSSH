//go:build preview_qa

package desktop

import (
	"image/png"
	"os"
	"path/filepath"
	"testing"

	"github.com/egoist/mygo/ui"
	"sshclient/internal/approval"
)

// Synthetic cached data, no windows, no system authentication or remote calls.
func TestWorkspaceNavigationSnapshotsWithoutWindows(t *testing.T) {
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
	for _, variant := range []struct {
		name                   string
		width, height          int
		page                   int
		dark, files, collapsed bool
	}{
		{"navigation-wide", 1280, 900, 1, false, false, false},
		{"navigation-narrow", 560, 760, 1, true, false, false},
		{"navigation-connection-rail", 1000, 760, 1, false, false, true},
		{"navigation-approval", 1000, 760, 3, true, false, false},
		{"navigation-approval-collapsed", 1000, 760, 3, true, false, true},
		{"navigation-approval-narrow-rail", 560, 760, 3, true, false, true},
		{"navigation-files", 1000, 760, 1, false, true, false},
	} {
		t.Run(variant.name, func(t *testing.T) {
			a := fileFollowDemo()
			a.page, a.fileExpanded = variant.page, variant.files
			a.sidebarHidden = variant.collapsed
			if variant.page == 3 {
				_, first := reviewFixture("full")
				second := first
				second.ID, second.Operation.Client = "offline-second-request", "另一个 AI 客户端"
				a.requests = []approval.Request{first, second}
				a.selectedRequest = first.ID
			}
			tt := ui.NewTester(a.view, variant.width, variant.height)
			tt.SetDark(variant.dark)
			path := filepath.Join(dir, variant.name+".png")
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
		})
	}
}
