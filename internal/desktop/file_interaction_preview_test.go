//go:build preview_qa

package desktop

import (
	"context"
	"fmt"
	"image/png"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/egoist/mygo/ui"
	"sshclient/internal/desktop/components"
	"sshclient/internal/sshconn"
	"sshclient/internal/transfer"
)

func TestFileInteractionSnapshotsWithoutWindows(t *testing.T) {
	if os.Getenv("SSH_CLIENT_PREVIEW_QA") != "1" {
		t.Skip("opt-in offline snapshots")
	}
	dir := os.Getenv("SSH_CLIENT_UI_SNAPSHOT_DIR")
	base, _ := filepath.Abs("../../build")
	rel, err := filepath.Rel(base, dir)
	if err != nil || !filepath.IsAbs(dir) || rel == ".." || strings.HasPrefix(rel, "../") {
		t.Fatal("output must be inside build")
	}
	if err := os.MkdirAll(dir, 0700); err != nil {
		t.Fatal(err)
	}
	for _, dark := range []bool{false, true} {
		for _, width := range []int{560, 1100} {
			for _, state := range []string{"dual", "opening", "error", "context", "directory", "move", "sorted", "overwrite"} {
				name := fmt.Sprintf("%s-%d-%t", state, width, dark)
				t.Run(name, func(t *testing.T) {
					a := &app{ctx: context.Background(), fileExpanded: true}
					a.ensureFileLayout()
					p := a.primaryFilePane
					p.endpoint, p.follow = "web", false
					b := p.browser()
					files := []sshconn.File{{Name: "assets", Directory: true}, {Name: "compose.yaml", Size: 1536}, {Name: "nginx.conf", Size: 876}, {Name: "README.md", Size: 2048}}
					a.applyDirectory(b, false, "/srv/site", files, b.seq, true)
					if state == "dual" {
						local := a.localFilePane.browser()
						a.applyDirectory(local, true, "/Users/demo", []sshconn.File{{Name: "Downloads", Directory: true}, {Name: "notes.txt", Size: 123}}, local.seq, true)
						a.addLocalFileWindow(p)
					}
					if state == "opening" || state == "error" {
						fakeEditorFactory(a)
						a.installEditor("web", "/srv/site/nginx.conf", "", "")
						a.editorRead = &fileReadState{endpoint: "web", name: a.editPath, loading: state == "opening", error: "仅编辑不含 NUL 的 UTF-8 文本，请下载该文件", cancel: func() {}, editor: a.edit}
					}
					if state == "directory" {
						b.showDirectory(false, "/srv/site/assets")
						b.loading = map[string]*directoryLoad{b.dir: {ctx: context.Background(), cancel: func() {}}}
					}
					if state == "move" {
						a.online, p.treeHidden = true, true
					}
					if state == "overwrite" {
						a.online = true
						job := transfer.Job{Host: "web", Local: "/Users/demo/compose.yaml", Remote: "/srv/site/compose.yaml", State: "error"}
						a.jobs = []transfer.Job{job}
						a.uploadConflicts = []uploadConflict{{job: job, originalHash: "synthetic digest"}}
					}
					tt := ui.NewTester(func(c *ui.Context) {
						components.ApplyTheme(c, false)
						ui.Column(c).Fill().Background(c.Theme().Background).Children(func() { a.fileWindows(c, a.fileLayout) })
						a.promptForHost(c, dialogHostMain)
					}, width, 520)
					tt.SetDark(dark)
					if state == "context" {
						if err := tt.RightClick("compose.yaml"); err != nil {
							t.Fatal(err)
						}
						if _, ok := tt.Find("文件操作菜单"); !ok {
							t.Fatal("context menu was not visible")
						}
					}
					if state == "move" {
						source, sourceOK := tt.Find("compose.yaml")
						target, targetOK := tt.Find("assets")
						if !sourceOK || !targetOK {
							t.Fatal("drag source or folder missing")
						}
						x, y := source.X+40, source.Y+source.H/2
						tt.Press(x, y)
						tt.Move(x+12, y)
						tt.Move(target.X+140, target.Y+target.H/2)
					}
					if state == "sorted" {
						if err := tt.Click("大小"); err != nil {
							t.Fatal(err)
						}
						tt.Frame()
					}
					if state == "overwrite" && !tt.HasText("覆盖远程文件？") {
						t.Fatal("upload conflict confirmation was not visible")
					}
					f, err := os.OpenFile(filepath.Join(dir, name+".png"), os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0600)
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
}
