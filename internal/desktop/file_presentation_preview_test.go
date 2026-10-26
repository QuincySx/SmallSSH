//go:build preview_qa && terminal_integration

package desktop

import (
	"image/png"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"
	"sshclient/internal/sshconn"
)

// Cached, synthetic paths exercise the real file pane without disk/SSH IO.
func filePresentationDemo() *app {
	a := fileFollowDemo()
	p := a.primaryFilePane
	p.endpoint = "a"
	b := p.browser()
	modified := time.Date(2026, 10, 21, 9, 30, 0, 0, time.Local)
	for _, directory := range []struct {
		path  string
		files []sshconn.File
	}{
		{"/", []sshconn.File{{Name: "etc", Directory: true}, {Name: "srv", Directory: true}, {Name: "var", Directory: true}}},
		{"/srv", []sshconn.File{{Name: "backups", Directory: true}, {Name: "site", Directory: true}}},
		{"/srv/site", []sshconn.File{
			{Name: "assets", Directory: true, Modified: modified},
			{Name: "logs", Directory: true, Modified: modified},
			{Name: ".env.example", Size: 382, Modified: modified},
			{Name: "compose.yaml", Size: 1536, Modified: modified},
			{Name: "index.html", Size: 6144, Modified: modified},
			{Name: "nginx.conf", Size: 876, Modified: modified},
			{Name: "README.md", Size: 2048, Modified: modified},
			{Name: "release.tar.gz", Size: 2949120, Modified: modified},
		}},
		{"/srv/site/assets", []sshconn.File{{Name: "icons", Directory: true}}},
		{"/srv/site/logs", nil},
	} {
		a.applyDirectory(b, false, directory.path, sortedFiles(directory.files), b.seq, false)
	}
	a.navigateDirectory(p, "/srv/site")
	b.selected = b.filesByName["index.html"]
	b.chosen.Add("index.html")
	a.toolsDetached = true
	a.dock = toolFiles
	return a
}

func TestFilePresentationSnapshotsWithoutWindows(t *testing.T) {
	if os.Getenv("SSH_CLIENT_PREVIEW_QA") != "1" {
		t.Skip("opt-in offline preview")
	}
	dir := os.Getenv("SSH_CLIENT_UI_SNAPSHOT_DIR")
	if !filepath.IsAbs(dir) {
		t.Fatal("absolute output path required")
	}
	if err := os.MkdirAll(dir, 0700); err != nil {
		t.Fatal(err)
	}
	for _, variant := range []string{"light", "dark", "compact", "right"} {
		t.Run(variant, func(t *testing.T) {
			a := filePresentationDemo()
			view, width, height := a.detachedToolsView, 960, 500
			if variant == "compact" || variant == "right" {
				a.toolsDetached = false
				view = a.fileArea
				if variant == "compact" {
					width, height = 1000, 228
				} else {
					width, height = 420, 500
				}
			}
			tt := ui.NewTester(view, width, height)
			tt.SetDark(variant == "dark")
			for _, label := range []string{"目录树", "文件列表", "目录路径", "刷新目录", "文件窗口菜单", "工具面板布局"} {
				r, ok := tt.Find(label)
				if !ok || r.W <= 0 || r.H <= 0 || r.X < 0 || r.Y < 0 || r.X+r.W > float32(width)+1 || r.Y+r.H > float32(height)+1 {
					t.Fatal("file control clipped", variant, label, r)
				}
			}
			if !tt.HasText("8 项 · 已选 1 项") {
				t.Fatal("file status missing", tt.Texts())
			}
			f, err := os.Create(filepath.Join(dir, "files-"+variant+".png"))
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
