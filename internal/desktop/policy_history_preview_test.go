//go:build preview_qa

package desktop

import (
	"image/png"
	"os"
	"path/filepath"
	"testing"

	"github.com/egoist/mygo/ui"
)

// Offline fixtures only: no desktop app, system authentication, server or vault.
func TestPolicyHistorySnapshotsWithoutWindows(t *testing.T) {
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
	for _, variant := range []string{"history", "history-narrow", "policy"} {
		a := historyUIFixture()
		width, height := 1100, 760
		view := a.approvalView
		if variant == "history-narrow" {
			width, a.sidebarHidden = 560, true
		}
		if variant == "policy" {
			width, height = 500, 260
			view = func(c *ui.Context) {
				ui.Column(c).Fill().Padding(24).Children(func() { a.connectionPolicyForm(c) })
			}
		}
		tt := ui.NewTester(view, width, height)
		file, err := os.OpenFile(filepath.Join(dir, variant+".png"), os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0600)
		if err != nil {
			t.Fatal(err)
		}
		err = png.Encode(file, tt.Image())
		closeErr := file.Close()
		if err != nil || closeErr != nil {
			t.Fatal(err, closeErr)
		}
	}
}
