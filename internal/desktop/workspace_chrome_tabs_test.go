package desktop

import (
	"context"
	"fmt"
	"testing"

	"github.com/egoist/mygo/ui"
)

func TestWorkspaceChromeOwnsRealTerminalTabs(t *testing.T) {
	a := &app{ctx: context.Background(), online: true, page: 1, toolsHidden: true, sessions: []*session{{name: "First", host: "a"}, {name: "Second", host: "b"}}}
	a.ensureTerminalGroups()
	a.splitExistingTerminal(a.sessions[1])
	first, second := a.sessions[0], a.sessions[1]
	left, right := a.terminalLayout.left, a.terminalLayout.right
	tt := ui.NewTester(a.view, 1280, 900)
	tt.SetTitleBar(ui.TitleBar{Height: 32, Left: 78, Right: 60})
	header, ok := tt.Find("全局导航栏")
	if !ok || header.H != 48 {
		t.Fatal("missing compact title bar", header)
	}
	// Split workspaces retain global navigation tabs and plain pane headers.
	// Selecting a top tab focuses the original pane without merging the split.
	for _, label := range []string{"显示工具面板", "设置", a.pageSidebarLabel(1)} {
		r, found := tt.Find(label)
		if !found || r.X < 78 || r.X+r.W > 1220 || r.Y < header.Y || r.Y+r.H > header.Y+header.H {
			t.Fatalf("%s outside title-bar controls: %v", label, r)
		}
	}
	if _, found := tt.Find("终端分栏"); found {
		t.Fatal("layout button still occupies the title bar")
	}
	for _, label := range []string{"工具面板放到右侧", "工具面板放到底部"} {
		if _, found := tt.Find(label); found {
			t.Fatal("dock placement remains duplicated in the title bar", label)
		}
	}
	for _, label := range []string{"顶部标签：First", "顶部标签：Second"} {
		r, found := tt.Find(label)
		if !found || r.X < header.X || r.X+r.W > header.X+header.W || r.Y < header.Y || r.Y+r.H > header.Y+header.H {
			t.Fatal("split workspace lost a global title-bar tab", label, r)
		}
	}
	for _, pane := range []struct {
		label string
		group *terminalGroup
	}{{"First", left}, {"Second", right}} {
		box, found := tt.Find(fmt.Sprintf("终端组%d", pane.group.id))
		r, ok := tt.Find("关闭标签：" + pane.label)
		if !found || !ok || r.X < box.X || r.X+r.W > box.X+box.W || r.Y < box.Y || r.Y+r.H > box.Y+box.H {
			t.Fatalf("%s stayed outside its own group header: tab=%v group=%v", pane.label, r, box)
		}
	}
	if err := tt.Click("顶部标签：First"); err != nil || a.currentTerminal() != first || left.active != first {
		t.Fatal("left group tab did not activate", err, a.currentTerminal(), left.active)
	}
	if err := tt.Click("顶部标签：Second"); err != nil || a.currentTerminal() != second || right.active != second {
		t.Fatal("right group tab did not activate", err, a.currentTerminal(), right.active)
	}
	if err := tt.RightClick("顶部标签：Second"); err != nil || len(tt.Menu()) == 0 {
		t.Fatal("terminal context menu missing", err)
	}
	if len(a.sessions) != 2 || a.terminalLayout == nil || a.sessions[0] != first || a.sessions[1] != second ||
		first.group != left || second.group != right {
		t.Fatal("rendering or selection changed terminal groups")
	}
}

func TestWorkspaceChromeFileTabKeepsTerminalTabs(t *testing.T) {
	a := &app{ctx: context.Background(), online: true, page: 1, filesTabOpen: true, sessions: []*session{{name: "First", host: "a"}}}
	first := a.sessions[0]
	tt := ui.NewTester(a.workspaceHeader, 1000, 70)
	// Promoted tools are peer tabs in the same strip, not separate chrome buttons.
	terminalClose, ok := tt.Find("关闭标签：First")
	if !ok {
		t.Fatal("terminal tab lost its close button")
	}
	file, ok := tt.Find("文件")
	if !ok {
		t.Fatal("promoted file tool is not a tab in the terminal strip", tt.Texts())
	}
	if gap := file.X - (terminalClose.X + terminalClose.W); gap < 0 || gap > 16 {
		t.Fatalf("file tab is not adjacent to the terminal tab: close=%v file=%v", terminalClose, file)
	}
	if err := tt.Click("文件"); err != nil || !a.filesTabSelected {
		t.Fatal("file tab was not selected", err)
	}
	tt.Key(0, ui.KeyLeft)
	if a.filesTabSelected || a.currentTerminal() != first || a.activeTerminalGroup.selectedTool != nil {
		t.Fatal("keyboard did not return to the terminal tab", a.filesTabSelected, a.currentTerminal())
	}
	tt.Key(0, ui.KeyRight)
	if !a.filesTabSelected || a.currentTerminal() != first {
		t.Fatal("keyboard did not select the peer file tab", a.filesTabSelected, a.currentTerminal())
	}
	if _, ok := tt.Find("First"); !ok {
		t.Fatal("file selection removed the terminal tab")
	}
	if err := tt.Click("关闭文件标签"); err != nil {
		t.Fatal("closing the file tab failed", err)
	}
	if a.filesTabOpen || a.filesTabSelected || a.workspaceTools[toolFiles] != nil || len(a.terminalGroups()[0].tools) != 0 ||
		a.activeTerminalGroup.selectedTool != nil {
		t.Fatal("closing the file tab left a tool tab behind")
	}
	if len(a.sessions) != 1 || a.sessions[0] != first || a.currentTerminal() != first {
		t.Fatal("closing the file tab disturbed the terminal session")
	}
	if _, ok := tt.Find("First"); !ok {
		t.Fatal("terminal tab did not return after the file tab closed")
	}
}

func TestWorkspacePresentationRemainsInTabContextMenu(t *testing.T) {
	a := workspaceModelFixture(2)
	a.toolsHidden = true
	second := a.sessions[1]
	tt := ui.NewTester(a.view, 1280, 900)
	for _, mode := range []struct {
		label  string
		canvas bool
	}{{"平铺为画布", true}, {"聚合为标签", false}} {
		if err := rightClickWorkspaceTab(tt, a, workspaceTab{session: second}); err != nil {
			t.Fatal(err)
		}
		if err := tt.ChooseMenuItem(mode.label); err != nil {
			t.Fatal(err)
		}
		if a.workspaceCanvas != mode.canvas || a.currentTerminal() != second || len(a.sessions) != 2 {
			t.Fatal("context menu changed sessions or selected the wrong view")
		}
		if _, ok := tt.Find("终端分栏"); ok {
			t.Fatal("presentation restored the removed icon")
		}
	}
}
