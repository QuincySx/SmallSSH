//go:build preview_qa

package desktop

import (
	"image/png"
	"os"
	"path/filepath"
	"testing"

	"github.com/egoist/mygo/ui"
	"sshclient/internal/approval"
	"sshclient/internal/client"
)

func TestInternationalizationSnapshotsWithoutWindows(t *testing.T) {
	if os.Getenv("SSH_CLIENT_PREVIEW_QA") != "1" {
		t.Skip("opt-in offline localization snapshots")
	}
	dir := os.Getenv("SSH_CLIENT_UI_SNAPSHOT_DIR")
	if !filepath.IsAbs(dir) {
		t.Fatal("absolute snapshot directory required")
	}
	if err := os.MkdirAll(dir, 0700); err != nil {
		t.Fatal(err)
	}
	for _, variant := range []struct {
		name, locale  string
		page          int
		compact, dark bool
	}{
		{"english-workbench", "en", 1, false, true},
		{"english-workbench-rail", "en", 1, true, true},
		{"english-grants", "en", 3, true, false},
		{"english-settings", "en", 4, false, false},
		{"chinese-settings-rail", "zh-Hans", 4, true, true},
		{"pseudo-settings", "en-XA", 4, false, false},
	} {
		t.Run(variant.name, func(t *testing.T) {
			t.Setenv("MYGO_LOCALE", variant.locale)
			a := fileFollowDemo()
			a.dir = t.TempDir()
			a.client = client.Client{Dir: a.dir}
			a.page, a.sidebarHidden = variant.page, variant.compact
			a.settingsSection = settingsWorkspace
			if variant.page == 3 {
				_, request := reviewFixture("full")
				a.requests, a.selectedRequest = []approval.Request{request}, request.ID
			}
			tt := ui.NewTester(a.view, 1000, 760)
			tt.SetDark(variant.dark)
			tt.SetLocale(variant.locale)
			path := filepath.Join(dir, variant.name+".png")
			file, err := os.OpenFile(path, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0600)
			if err != nil {
				t.Fatal(err)
			}
			err = png.Encode(file, tt.Image())
			closeErr := file.Close()
			if err != nil || closeErr != nil {
				t.Fatal(err, closeErr)
			}
			t.Log(path)
		})
	}
}
