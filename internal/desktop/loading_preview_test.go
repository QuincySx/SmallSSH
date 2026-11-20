//go:build preview_qa

package desktop

import (
	"context"
	"fmt"
	"image/png"
	"os"
	"path/filepath"
	"testing"

	"github.com/egoist/mygo/ui"
	"sshclient/internal/desktop/components"
	"sshclient/internal/transfer"
	"sshclient/internal/vault"
)

func TestLoadingAndDirectoryTransferSnapshotsWithoutWindows(t *testing.T) {
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
	for _, variant := range []string{"server-loading", "server-error", "file-loading", "file-error", "directory-scanning", "directory-downloading"} {
		for _, width := range []int{560, 1100} {
			t.Run(variant+"-"+fmt.Sprint(width), func(t *testing.T) {
				a := &app{ctx: context.Background(), online: true, page: 1, sidebarHidden: true, toolsHidden: true, connections: []vault.Summary{{ID: "demo", Name: "演示服务器", User: "demo", Host: "192.0.2.10"}}}
				view, height := a.view, 760
				var want string
				switch variant {
				case "server-loading", "server-error":
					s := a.reserveTerminal("demo", nil, true)
					want = "正在连接服务器…"
					if variant == "server-error" {
						s.connectionState, s.connectionError, s.closed = "error", "连接超时，请检查网络后重试", true
						want = "重试连接"
					}
				case "file-loading", "file-error":
					a.installEditor("demo", "/srv/example/settings.conf", "", "")
					view = a.editorWindowView
					a.editorRead = &fileReadState{endpoint: "demo", name: a.editPath, loading: true, cancel: func() {}, editor: a.edit}
					want = "正在读取文件…"
					if variant == "file-error" {
						a.editorRead.loading, a.editorRead.error = false, "仅编辑不含 NUL 的 UTF-8 文本，请下载该文件"
						want = "下载此文件…"
					}
				case "directory-scanning", "directory-downloading":
					view, height = a.transferView, 320
					job := transfer.Job{ID: "synthetic-directory-task", Host: "demo", Direction: "download", Directory: true, Remote: "/srv/backups/project", Local: "/demo/Downloads/project", State: "running", Phase: "scanning", FilesTotal: 123}
					want = "正在扫描文件夹…"
					if variant == "directory-downloading" {
						job.Phase, job.FilesDone, job.Size, job.Bytes, job.Speed = "downloading", 42, 16<<20, 6<<20, 1024<<10
						want = "42 / 123"
					}
					a.jobs = []transfer.Job{job}
				}
				tt := ui.NewTester(func(c *ui.Context) {
					components.ApplyTheme(c, false)
					ui.Column(c).Fill().Background(c.Theme().Background).Children(func() { view(c) })
				}, width, height)
				if !tt.HasText(want) {
					t.Fatal("missing visible state", want, tt.Texts())
				}
				name := filepath.Join(dir, variant+"-"+fmt.Sprint(width)+".png")
				f, err := os.OpenFile(name, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0600)
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
}
