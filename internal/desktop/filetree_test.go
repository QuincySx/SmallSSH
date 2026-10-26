package desktop

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/egoist/mygo/ui"
	"sshclient/internal/sshconn"
	"sshclient/internal/transfer"
)

func TestLargeTreeVirtualizesRowsAndReusesIndexOnScroll(t *testing.T) {
	a := &app{}
	a.ensurePanes()
	p := a.primaryFilePane
	p.endpoint = "fixture"
	b := p.browser()
	fs := make([]sshconn.File, 100000)
	for i := range fs {
		fs[i] = sshconn.File{Name: fmt.Sprintf("directory-%06d", i), Directory: true}
	}
	a.applyDirectory(b, false, "/", fs, b.seq, true)
	tt := ui.NewTester(func(c *ui.Context) { a.directoryTree(c, p, b) }, 240, 600)
	count := 0
	for _, label := range tt.Texts() {
		if strings.HasPrefix(label, "directory-") {
			count++
		}
	}
	if count < 5 || count > 100 {
		t.Fatal("not a populated virtual window", count)
	}
	builds := b.tree.builds
	tt.Scroll(120, 300, 0, 10000)
	tt.Frame()
	if b.tree.builds != builds {
		t.Fatal("scroll rebuilt the tree")
	}
	if tt.HasText("directory-000000") {
		t.Fatal("scroll did not advance the visible window")
	}
	b.setExpanded("/", false)
	tt.Frame()
	if len(b.tree.rows) != 1 {
		t.Fatal("collapsed descendants were rendered", len(b.tree.rows))
	}
}

func TestTreeKeepsMixedCaseAncestorUniqueAndIndexesMovedFolder(t *testing.T) {
	a := &app{}
	a.ensurePanes()
	b := a.primaryFilePane.browser()
	a.applyDirectory(b, false, "/", sortedFiles([]sshconn.File{{Name: "Z", Directory: true}, {Name: "a", Directory: true}}), b.seq, true)
	a.applyDirectory(b, false, "/Z", nil, b.seq, true)
	children := b.directoryChildren("/")
	if len(children) != 2 {
		t.Fatal("duplicate inferred ancestor", children)
	}
	b.cache["/target"] = nil
	updateMutationCache(b, false, "/", "rename", "/a", "/target/moved", 0)
	if children = b.directoryChildren("/target"); len(children) != 1 || children[0] != "/target/moved" {
		t.Fatal(children)
	}
}

func TestTreeClickNavigatesAndDisclosureOnlyExpands(t *testing.T) {
	a := &app{}
	a.ensurePanes()
	p := a.primaryFilePane
	p.endpoint = "web"
	b := p.browser()
	a.applyDirectory(b, false, "/", []sshconn.File{{Name: "folder", Directory: true}}, b.seq, true)
	b.cache["/folder"] = []sshconn.File{{Name: "child", Directory: true}}
	tt := ui.NewTester(func(c *ui.Context) { a.directoryTree(c, p, b) }, 300, 500)
	if err := tt.Click("展开或折叠 /folder"); err != nil {
		t.Fatal(err)
	}
	if b.dir != "/" || !isExpanded(b, "/folder") {
		t.Fatal("disclosure changed active directory")
	}
	if err := tt.Click("folder"); err != nil {
		t.Fatal(err)
	}
	if b.dir != "/folder" {
		t.Fatal("tree row did not navigate", b.dir)
	}
}

func TestDefaultSingleFileWindowAndOptionalSecondRetainLocations(t *testing.T) {
	a := &app{}
	a.ensurePanes()
	a.primaryFilePane.endpoint = "remote"
	b := a.primaryFilePane.browser()
	a.applyDirectory(b, false, "/srv/site", nil, b.seq, true)
	tt := ui.NewTester(a.fileArea, 1000, 500)
	if _, ok := tt.Find("目录路径"); !ok {
		t.Fatal(tt.Texts())
	}
	if len(a.fileLayout.panes()) != 1 {
		t.Fatal("second pane opened by default")
	}
	if err := tt.Click("文件窗口菜单"); err != nil {
		t.Fatal(err)
	}
	if err := tt.ChooseMenuItem("添加本机窗口"); err != nil {
		t.Fatal(err)
	}
	if _, ok := tt.Find("窗口2目录路径"); !ok {
		t.Fatal(tt.Texts())
	}
	a.localFilePane.browser().dir = "/independent"
	if err := tt.Click("窗口2文件窗口菜单"); err != nil {
		t.Fatal(err)
	}
	if err := tt.ChooseMenuItem("关闭窗口"); err != nil {
		t.Fatal(err)
	}
	if b.dir != "/srv/site" {
		t.Fatal("closing secondary pane navigated primary")
	}
	if err := tt.Click("文件窗口菜单"); err != nil {
		t.Fatal(err)
	}
	if err := tt.ChooseMenuItem("添加本机窗口"); err != nil {
		t.Fatal(err)
	}
	if a.localFilePane.browser().dir != "/independent" {
		t.Fatal("reopening lost location")
	}
}

func TestContextMenuTargetsClickedFileAndBlankSpaceTargetsDirectory(t *testing.T) {
	a := &app{}
	a.ensurePanes()
	p := a.primaryFilePane
	p.endpoint = "web"
	b := p.browser()
	a.applyDirectory(b, false, "/srv", []sshconn.File{{Name: "first.txt"}, {Name: "second.txt"}}, b.seq, true)
	b.chosen.Add("first.txt")
	tt := ui.NewTester(func(c *ui.Context) { a.browserPane(c, p) }, 700, 500)
	if err := tt.Click("second.txt"); err != nil {
		t.Fatal(err)
	}
	if !b.chosen.Has("second.txt") {
		t.Fatal("file row did not select on click")
	}
	if err := tt.ClickWith(ui.Cmd, "first.txt"); err != nil {
		t.Fatal(err)
	}
	if b.chosen.Len() != 2 {
		t.Fatal("modified click lost multi-selection", b.chosen.Len())
	}
	b.chosen.Clear()
	b.chosen.Add("first.txt")
	if err := tt.RightClick("second.txt"); err != nil {
		t.Fatal(err)
	}
	if err := tt.ChooseMenuItem("重命名 / 移动"); err != nil {
		t.Fatal(err, tt.Menu())
	}
	if a.prompt.target != "web · /srv/second.txt" {
		t.Fatal(a.prompt.target)
	}
	a.prompt = promptState{}
	r, _ := tt.Find("文件列表")
	tt.RightClickAt(r.X+r.W/2, r.Y+r.H-10)
	if err := tt.ChooseMenuItem("新建文件夹"); err != nil {
		t.Fatal(err, tt.Menu())
	}
	if a.prompt.target != "web · /srv" {
		t.Fatal(a.prompt.target)
	}
}

func TestCrossPaneDropSnapshotsBothLocations(t *testing.T) {
	a := &app{}
	a.ensurePanes()
	left, right := a.localFilePane.browser(), a.primaryFilePane.browser()
	a.primaryFilePane.endpoint = "web"
	right = a.primaryFilePane.browser()
	a.applyDirectory(left, true, "/local", []sshconn.File{{Name: "upload.txt"}}, left.seq, true)
	a.applyDirectory(right, false, "/srv", nil, right.seq, true)
	a.splitFilePane(a.primaryFilePane, false, true)
	tt := ui.NewTester(a.fileArea, 1000, 500)
	s, _ := tt.Find("upload.txt")
	d, _ := tt.Find("窗口1文件列表")
	x, y := s.X+s.W/2, s.Y+s.H/2
	tt.Press(x, y)
	tt.Move(x+12, y)
	tt.Move(d.X+d.W/2, d.Y+d.H/2)
	tt.Release(d.X+d.W/2, d.Y+d.H/2)
	if !a.prompt.open || !strings.Contains(a.prompt.target, "/local") || !strings.Contains(a.prompt.target, "/srv") {
		t.Fatal("drop did not offer the captured transfer", a.prompt)
	}
	a.primaryFilePane.endpoint = "other"
	if !strings.Contains(a.prompt.target, "web") || strings.Contains(a.prompt.target, "other") {
		t.Fatal("target changed after confirmation opened")
	}
}

func TestCachedNavigationAndLockCancelPendingLoads(t *testing.T) {
	a := &app{}
	a.ensurePanes()
	p := a.primaryFilePane
	b := p.browser()
	ctx, cancel := context.WithCancel(context.Background())
	b.loading = map[string]*directoryLoad{"/old": {ctx: ctx, cancel: cancel, navigate: true}}
	b.cache["/new"] = nil
	a.navigateDirectory(p, "/new")
	if ctx.Err() == nil || len(b.loading) != 0 || b.dir != "/new" {
		t.Fatal("cached navigation left an obsolete request")
	}
	ctx, cancel = context.WithCancel(context.Background())
	b.loading["/tree"] = &directoryLoad{ctx: ctx, cancel: cancel}
	a.cancelDirectoryLoads()
	if ctx.Err() == nil || len(b.loading) != 0 {
		t.Fatal("lock left directory work active")
	}
}

func TestDoubleClickOpensFolderAndCompletedTransferAppearsOnce(t *testing.T) {
	a := &app{}
	a.ensurePanes()
	p := a.primaryFilePane
	p.endpoint = "web"
	b := p.browser()
	a.applyDirectory(b, false, "/", []sshconn.File{{Name: "folder", Directory: true}}, b.seq, true)
	b.cache["/folder"] = nil
	tt := ui.NewTester(func(c *ui.Context) { a.fileList(c, p, b) }, 500, 400)
	tt.Click("folder")
	tt.Click("folder")
	if b.dir != "/folder" {
		t.Fatal("double click did not open folder", b.dir)
	}
	j := transfer.Job{Host: "web", Direction: "upload", Remote: "/folder/new.txt", Size: 42, State: "done"}
	a.transferArrived(j)
	a.transferArrived(j)
	tt.Frame()
	if len(b.cache["/folder"]) != 1 || !tt.HasText("new.txt") {
		t.Fatal("completion did not update list without duplicates", tt.Texts())
	}
}
