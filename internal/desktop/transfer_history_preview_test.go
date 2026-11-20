//go:build preview_qa

package desktop

import (
	"image/png"
	"os"
	"path/filepath"
	"testing"

	"github.com/egoist/mygo/ui"
	"sshclient/internal/transfer"
)

func TestTransferHistorySnapshotsWithoutWindows(t *testing.T) {
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
		name                                  string
		history, dark, empty, english, narrow bool
	}{
		{name: "current-light"}, {name: "history-light", history: true}, {name: "history-dark", history: true, dark: true},
		{name: "history-empty", history: true, empty: true}, {name: "history-english", history: true, english: true},
		{name: "current-narrow", narrow: true},
	} {
		t.Run(variant.name, func(t *testing.T) {
			a := statusDemo()
			a.transfers = transfer.New(t.TempDir())
			a.jobs = []transfer.Job{
				{ID: "complete1", Host: "demo", Remote: "/srv/site/nginx.conf", State: "done", Size: 2048, Bytes: 2048, Direction: "download"},
				{ID: "complete2", Host: "demo", Remote: "/srv/site/assets.zip", State: "done", Size: 8 << 20, Bytes: 8 << 20, Direction: "download"},
				{ID: "active", Host: "demo", Remote: "/srv/site/backup.zip", State: "running", Size: 20 << 20, Bytes: 8 << 20, Speed: 1 << 20, Direction: "download"},
				{ID: "paused", Host: "demo", Remote: "/srv/site/archive.zip", State: "paused", Size: 10 << 20, Bytes: 2 << 20, Direction: "download"},
				{ID: "failed", Host: "demo", Remote: "/srv/site/config.yaml", State: "error", Error: "连接已断开，可重试", Direction: "download"},
			}
			if variant.empty {
				a.jobs = nil
			}
			width := 1280
			if variant.narrow {
				width = 560
			}
			tt := ui.NewTester(a.view, width, 760)
			tt.SetTitleBar(ui.TitleBar{Height: 48, Left: 78})
			tt.SetDark(variant.dark)
			if err := tt.Click("任务中心"); err != nil {
				t.Fatal(err)
			}
			if variant.history {
				if err := tt.Click("历史记录"); err != nil {
					t.Fatal(err)
				}
			}
			if variant.english {
				tt.SetLocale("en")
			}
			tt.Frame()
			if _, ok := tt.Find("终端分栏"); ok {
				t.Fatal("removed title bar icon remains")
			}
			f, err := os.OpenFile(filepath.Join(dir, "transfers-"+variant.name+".png"), os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0600)
			if err != nil {
				t.Fatal(err)
			}
			err = png.Encode(f, tt.Image())
			closeErr := f.Close()
			if err != nil || closeErr != nil {
				t.Fatal(err, closeErr)
			}
		})
	}
}
