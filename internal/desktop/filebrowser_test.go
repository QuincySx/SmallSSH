package desktop

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/egoist/mygo/ui"
	"github.com/egoist/mygo/ui/editor"
	"sshclient/internal/sshconn"
	"sshclient/internal/vault"
)

func TestTwoBrowsersKeepIndependentLocationsAndIgnoreStaleNavigation(t *testing.T) {
	a := &app{}
	a.ensurePanes()
	left, right := a.localFilePane.browser(), a.primaryFilePane.browser()
	left.dir = "/local/project"
	right.seq = 2
	a.applyDirectory(right, false, "/srv/new", []sshconn.File{{Name: "new.txt"}}, 2, true)
	right.chosen.Add("new.txt")
	a.applyDirectory(right, false, "/old", []sshconn.File{{Name: "old.txt"}}, 1, true)
	if left.dir != "/local/project" || right.dir != "/srv/new" || !right.chosen.Has("new.txt") {
		t.Fatal("a stale request or the other pane changed the active location")
	}
	right.chosen.Clear()
	right.selected = 0
	if len(chosenFiles(right)) != 0 {
		t.Fatal("focus was treated as a selection after deselecting")
	}
	children := treeChildren(right, false, "/srv")
	if len(children) != 1 || children[0] != "/srv/new" {
		t.Fatal(children)
	}
	a.primaryFilePane.endpoint = "other"
	a.primaryFilePane.browser().dir = "/other"
	a.primaryFilePane.endpoint = localEndpoint
	if a.primaryFilePane.browser().dir != "/srv/new" {
		t.Fatal("lost per-server directory")
	}
}
func TestLocalEditingDetectsChangesAndNeverClobbersSaveAs(t *testing.T) {
	dir := t.TempDir()
	name := filepath.Join(dir, "config.txt")
	original := []byte("before\n")
	os.WriteFile(name, original, 0600)
	if e := saveLocalText(name, []byte("after\n"), textHash(original), false); e != nil {
		t.Fatal(e)
	}
	if e := saveLocalText(name, []byte("clobber"), textHash(original), false); e == nil {
		t.Fatal("external changes overwritten")
	}
	if e := saveLocalText(name, []byte("clobber"), "", true); e == nil {
		t.Fatal("save-as overwrote an existing file")
	}
	copy := filepath.Join(dir, "copy.txt")
	if e := saveLocalText(copy, []byte("new\n"), "", true); e != nil {
		t.Fatal(e)
	}
	got, _ := os.ReadFile(name)
	if string(got) != "after\n" {
		t.Fatal(string(got))
	}
	if e := mutateLocal("remove", dir, "", 0); e == nil {
		t.Fatal("nonempty directory was recursively deleted")
	}
}

func TestFileOperationSnapshotsParentAndUpdatesTree(t *testing.T) {
	a := &app{ctx: context.Background(), online: true, selected: "web", sessions: []*session{{host: "web"}}}
	a.ensurePanes()
	p := a.primaryFilePane
	b := p.browser()
	b.dir = "/srv/site"
	b.cache[b.dir] = []sshconn.File{{Name: "config.txt"}}
	b.chosen.Add("config.txt")
	a.fileOperation(p, "mkdir")
	if a.prompt.target != "web · /srv/site" {
		t.Fatal("new folder targeted a selected file", a.prompt.target)
	}
	updateMutationCache(b, false, b.dir, "mkdir", "/srv/site/new-folder", "", 0)
	if children := treeChildren(b, false, b.dir); len(children) != 1 || children[0] != "/srv/site/new-folder" {
		t.Fatal(children)
	}
	updateMutationCache(b, false, b.dir, "rename", "/srv/site/config.txt", "/srv/site/renamed.txt", 0)
	for _, f := range b.cache[b.dir] {
		if f.Name == "config.txt" {
			t.Fatal("old name remained")
		}
	}
}
func TestTerminalFirstLayoutAndCommandFillDoNotExecute(t *testing.T) {
	a := &app{ctx: context.Background(), online: true, page: 1, selected: "web", groups: map[string]*bool{}, sidebarWidth: 210, terminalShare: 72, edit: &editor.State{Buffer: editor.NewBuffer("")}, sessions: []*session{{host: "web", name: "Web"}, {host: "other", name: "Other"}}, connections: []vault.Summary{{ID: "web", Name: "Web", Host: "127.0.0.1", User: "user"}}}
	tt := ui.NewTester(a.view, 1280, 900)
	for _, label := range []string{"目录路径", "文件窗口菜单", "工具面板布局"} {
		if _, ok := tt.Find(label); !ok {
			t.Fatal(label, tt.Texts())
		}
	}
	if err := tt.Click("文件窗口菜单"); err != nil {
		t.Fatal(err)
	}
	if err := tt.ChooseMenuItem("向右拆分"); err != nil {
		t.Fatal(err)
	}
	for _, label := range []string{"窗口1目录路径", "窗口2目录路径"} {
		if _, ok := tt.Find(label); !ok {
			t.Fatal(label)
		}
	}
	a.fillSnippet(vault.Snippet{Body: "uptime"})
	if a.sessions[0].draft != "uptime" || a.sessions[1].draft != "" {
		t.Fatal("command was sent to the wrong session")
	}
	a.showTool(toolCommands)
	tt.Frame()
	if !tt.HasText("新建常用命令") {
		t.Fatal(tt.Texts())
	}
	a.showTool(toolFiles)
	a.fileExpanded = true
	tt.Frame()
	if tt.HasText("终端区域") {
		t.Fatal("tool panel did not expand")
	}
	a.fileExpanded = false
	tt.SetSize(1000, 760)
	tt.SetDark(true)
	if !tt.HasText("窗口1目录路径") {
		t.Fatal("file pane lost at minimum size")
	}
}
