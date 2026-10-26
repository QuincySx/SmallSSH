package desktop

import (
	"context"
	"testing"

	"github.com/egoist/mygo/ui"
	"sshclient/internal/vault"
)

func fileFollowDemo() *app {
	a := &app{ctx: context.Background(), online: true, page: 1, selected: "a", groups: map[string]*bool{}, sidebarWidth: 190, terminalShare: 65,
		connections: []vault.Summary{{ID: "a", Name: "连接 A", Host: "192.0.2.1"}, {ID: "b", Name: "连接 B", Host: "192.0.2.2"}, {ID: "c", Name: "连接 C", Host: "192.0.2.3"}},
		sessions:    []*session{{host: "a", name: "终端 A"}, {host: "b", name: "终端 B"}},
	}
	a.ensurePanes()
	for _, p := range []*filePane{a.localFilePane, a.primaryFilePane} {
		original := p.endpoint
		for _, endpoint := range []string{"a", "b", "c", localEndpoint} {
			p.endpoint = endpoint
			b := p.browser()
			if endpoint != localEndpoint {
				b.dir = "/srv/" + endpoint
				b.address = b.dir
			}
			b.cache[b.dir] = nil
		}
		p.endpoint = original
	}
	return a
}

func TestFilesFollowActiveTabWithoutFollowingSidebarSelection(t *testing.T) {
	a := fileFollowDemo()
	tt := ui.NewTester(a.view, 1280, 900)
	if !a.primaryFilePane.follow || a.primaryFilePane.endpoint != "a" {
		t.Fatal("default file window did not follow active tab")
	}
	if err := tt.Click("连接 B"); err != nil {
		t.Fatal(err)
	}
	if a.selected != "b" || a.active != 0 || a.primaryFilePane.endpoint != "a" || len(a.connecting) != 0 {
		t.Fatal("sidebar click switched terminal or file connection", a.selected, a.active, a.primaryFilePane.endpoint)
	}
	if err := tt.Click("终端 B"); err != nil {
		t.Fatal(err)
	}
	if a.primaryFilePane.endpoint != "b" || a.primaryFilePane.browser().dir != "/srv/b" || a.selected != "b" {
		t.Fatal("file window did not follow exact tab")
	}
	old := a.primaryFilePane.browser()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	old.loading = map[string]*directoryLoad{"/old": {ctx: ctx, cancel: cancel}}
	if err := tt.Click("终端 A"); err != nil {
		t.Fatal(err)
	}
	if a.primaryFilePane.endpoint != "a" || a.primaryFilePane.browser().dir != "/srv/a" || ctx.Err() == nil || a.selected != "b" {
		t.Fatal("switch lost location or kept obsolete work alive")
	}
	if err := tt.Click("连接 C"); err != nil {
		t.Fatal(err)
	}
	if err := tt.Click("终端 B"); err != nil {
		t.Fatal(err)
	}
	if a.active != 1 || a.primaryFilePane.endpoint != "b" || a.selected != "c" || len(a.sessions) != 2 {
		t.Fatal("terminal focus changed sidebar selection or missed following files", a.active, a.primaryFilePane.endpoint, a.selected)
	}
}

func TestFileLocationPickerPinsAndRestoresFollowing(t *testing.T) {
	a := fileFollowDemo()
	tt := ui.NewTester(a.view, 1280, 900)
	if err := tt.Click("文件位置"); err != nil {
		t.Fatal(err)
	}
	tt.Key(0, ui.KeyEnd)
	tt.Key(0, ui.KeyEnter)
	tt.Frame()
	if a.primaryFilePane.follow || a.primaryFilePane.endpoint != "c" {
		t.Fatal("manual server choice did not pin file window", a.primaryFilePane.endpoint, a.primaryFilePane.follow, tt.Texts())
	}
	if err := tt.Click("终端 B"); err != nil {
		t.Fatal(err)
	}
	if a.primaryFilePane.endpoint != "c" || a.primaryFilePane.browser().dir != "/srv/c" {
		t.Fatal("terminal replaced manually pinned server")
	}
	if err := tt.Click("文件位置"); err != nil {
		t.Fatal(err)
	}
	tt.Key(0, ui.KeyHome)
	tt.Key(0, ui.KeyEnter)
	tt.Frame()
	if !a.primaryFilePane.follow || a.primaryFilePane.endpoint != "b" {
		t.Fatal("follow choice did not restore current tab", a.primaryFilePane.endpoint, a.primaryFilePane.follow, tt.Texts())
	}
	if err := tt.Click("终端 A"); err != nil {
		t.Fatal(err)
	}
	if a.primaryFilePane.endpoint != "a" {
		t.Fatal("restored following did not stay enabled")
	}
	if a.localFilePane.endpoint != localEndpoint || a.localFilePane.follow {
		t.Fatal("second independent file window was changed")
	}
}

func TestFilesFallbackAfterClosingTabsAndKeepPinnedLocation(t *testing.T) {
	a := fileFollowDemo()
	a.closeSessionAt(0)
	if a.primaryFilePane.endpoint != "b" {
		t.Fatal("files did not follow remaining tab")
	}
	a.closeSessionAt(0)
	if a.primaryFilePane.endpoint != localEndpoint || !a.primaryFilePane.follow {
		t.Fatal("no active tab retained old remote target")
	}
	a.chooseFileEndpoint(a.primaryFilePane, "c")
	a.syncFollowingFilePanes()
	if a.primaryFilePane.endpoint != "c" || a.primaryFilePane.follow {
		t.Fatal("pinned target lost when all terminals closed")
	}
}

func TestFileFollowingResumesWhenReturningFromConnectionForm(t *testing.T) {
	a := fileFollowDemo()
	// A terminal can finish connecting while a separate connection form stays
	// open. Returning to the workspace must derive the actual active tab.
	a.page = 0
	a.active = 1
	a.selected = "c"
	a.page = 1
	ui.NewTester(a.view, 1280, 900)
	if a.primaryFilePane.endpoint != "b" || a.primaryFilePane.browser().dir != "/srv/b" || a.selected != "c" {
		t.Fatal("returning followed sidebar selection or missed the active terminal", a.primaryFilePane.endpoint, a.selected)
	}
}
