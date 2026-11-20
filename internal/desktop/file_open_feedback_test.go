package desktop

import (
	"context"
	"testing"

	"github.com/egoist/mygo/ui"
	"sshclient/internal/sshconn"
)

func TestFileListKeepsOpenFeedbackBesideOriginAndCanShowExistingWindow(t *testing.T) {
	a := &app{}
	a.ensureFileLayout()
	p := a.primaryFilePane
	p.endpoint, p.follow, p.treeHidden = "web", false, true
	b := p.browser()
	a.applyDirectory(b, false, "/srv", []sshconn.File{{Name: "settings.conf"}}, b.seq, true)
	windows := fakeEditorFactory(a)
	a.installEditor("web", "/srv/settings.conf", "", "")
	a.editorRead = &fileReadState{endpoint: "web", name: a.editPath, loading: true, cancel: func() {}, editor: a.edit}
	tt := ui.NewTester(func(c *ui.Context) { a.browserPane(c, p) }, 800, 400)
	if !tt.HasText("正在打开 · settings.conf") {
		t.Fatal("the originating list has no immediate feedback", tt.Texts())
	}
	if _, ok := tt.Find("正在打开文件"); !ok {
		t.Fatal("file opening has no spinner")
	}
	if err := tt.Click("显示文件窗口"); err != nil {
		t.Fatal(err)
	}
	if len(*windows) != 1 || (*windows)[0].shown != 2 {
		t.Fatal("show action created another document instead of showing its window")
	}
	a.editorRead.loading, a.editorRead.error = false, "仅编辑不含 NUL 的 UTF-8 文本，请下载该文件"
	tt.Frame()
	if !tt.HasText("打开失败 · settings.conf") {
		t.Fatal("a failed read silently disappeared from the list", tt.Texts())
	}
	ed := ui.NewTester(a.editorWindowView, 720, 480)
	if !ed.HasText("文件暂未打开") || !ed.HasText(a.editorRead.error) {
		t.Fatal("file window lost the reason for failure", ed.Texts())
	}
	a.editorRead = nil
	tt.Frame()
	if !tt.HasText("已打开 · settings.conf") {
		t.Fatal("completion was not visible beside its origin", tt.Texts())
	}
	a.setFileEndpoint(p, "another-server", false)
	tt.Frame()
	if tt.HasText("已打开 · settings.conf") {
		t.Fatal("feedback leaked into another server's listing")
	}
}

func TestDirectoryNavigationImmediatelyShowsLoadingAtDestination(t *testing.T) {
	// Queue work without running it: this renders the state before disk/SSH
	// completion and creates no native window or network request.
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	a := &app{ctx: ctx, dispatch: func(func()) {}, directorySlots: make(chan struct{}, 1)}
	a.directorySlots <- struct{}{}
	a.ensureFileLayout()
	p := a.localFilePane
	p.treeHidden = true
	b := p.browser()
	a.applyDirectory(b, true, "/synthetic", []sshconn.File{{Name: "next", Directory: true}, {Name: "old.txt"}}, b.seq, true)
	b.chosen.Add("old.txt")
	a.openEntries(p, b.dir, []sshconn.File{{Name: "next", Directory: true}})
	if b.dir != "/synthetic/next" || b.address != b.dir || b.chosen.Len() != 0 || b.loading[b.dir] == nil {
		t.Fatal("navigation kept the old location or selection", b.dir, b.address)
	}
	tt := ui.NewTester(func(c *ui.Context) { a.browserPane(c, p) }, 800, 400)
	if !tt.HasText("正在读取文件夹…") || tt.HasText("old.txt") {
		t.Fatal("directory navigation looks idle or still shows actionable old rows", tt.Texts())
	}
	cancelPaneLoads(p)
}

func TestDirectoryLoadingKeepsTheListKeyboardFocus(t *testing.T) {
	for _, moved := range []bool{false, true} {
		ctx, cancel := context.WithCancel(context.Background())
		a := &app{ctx: ctx, dispatch: func(func()) {}, directorySlots: make(chan struct{}, 1)}
		a.directorySlots <- struct{}{} // Hold the listing until the test delivers it.
		a.ensureFileLayout()
		p := a.localFilePane
		b := p.browser()
		a.applyDirectory(b, true, "/synthetic", []sshconn.File{{Name: "next", Directory: true}, {Name: "deeper", Directory: true}}, b.seq, true)
		tt := ui.NewTester(func(c *ui.Context) {
			ui.Column(c).Fill().Children(func() {
				ui.Box(c).Size(80, 24).Shrink(0).Label("其他控件").Focusable()
				a.fileList(c, p, b)
			})
		}, 640, 400)
		if err := tt.Click("next"); err != nil {
			t.Fatal(err)
		}
		tt.Key(0, ui.KeyEnter)
		tt.Frame()
		if b.dir != "/synthetic/next" || !tt.HasText("正在读取文件夹…") {
			t.Fatal("Enter did not start loading the uncached folder", b.dir, tt.Texts())
		}
		if moved {
			if err := tt.Click("其他控件"); err != nil {
				t.Fatal(err)
			}
		}
		cancelPaneLoads(p)
		a.applyDirectory(b, true, b.dir, []sshconn.File{{Name: "inner.txt"}}, b.seq, true)
		tt.Frame()
		tt.Key(ui.Alt, ui.KeyUp)
		tt.Frame()
		if moved && b.dir != "/synthetic/next" {
			t.Fatal("the loaded list took the focus back from another control", b.dir)
		}
		if !moved && b.dir != "/synthetic" {
			t.Fatal("the list lost its keyboard focus while the folder loaded", b.dir)
		}
		cancelPaneLoads(p)
		cancel()
	}
}

func TestDirectoryLoadingPlaceholderKeepsTheParentKey(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	a := &app{ctx: ctx, dispatch: func(func()) {}, directorySlots: make(chan struct{}, 1)}
	a.directorySlots <- struct{}{}
	a.ensureFileLayout()
	p := a.localFilePane
	b := p.browser()
	a.applyDirectory(b, true, "/synthetic", []sshconn.File{{Name: "slow", Directory: true}}, b.seq, true)
	tt := ui.NewTester(func(c *ui.Context) { a.fileList(c, p, b) }, 640, 400)
	if err := tt.Click("slow"); err != nil {
		t.Fatal(err)
	}
	tt.Key(0, ui.KeyEnter)
	tt.Frame()
	tt.Key(ui.Alt, ui.KeyUp)
	tt.Frame()
	if b.dir != "/synthetic" || !tt.HasText("slow") {
		t.Fatal("Alt+Up did not leave a folder that is still loading", b.dir, tt.Texts())
	}
	cancelPaneLoads(p)
}
