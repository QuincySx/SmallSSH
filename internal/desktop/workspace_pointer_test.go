//go:build terminal_integration

package desktop

import (
	"fmt"
	"slices"
	"testing"

	"github.com/egoist/mygo/ui"
)

func TestWorkspacePointerDragsBuildSixPeerPanesWithoutReplacingContent(t *testing.T) {
	a, recorders := terminalSplitFixture(t, 4)
	a.toolsHidden = true
	a.openWorkspaceTool(toolFiles)
	files := a.workspaceTools[toolFiles]
	pane, browser := a.primaryFilePane, a.primaryFilePane.browser()
	a.openWorkspaceTool(toolDiagnostics)
	status := a.workspaceTools[toolDiagnostics]
	source := files.source
	sessions := slices.Clone(a.sessions)
	tt := ui.NewTester(a.view, 1440, 960)
	tabLabel := func(label string) string {
		if a.workspaceCanvas || len(a.terminalGroups()) > 1 {
			return "顶部标签：" + label
		}
		return label
	}
	split := func(label string, group *terminalGroup, edge string, count int) {
		t.Helper()
		r, ok := tt.Find(fmt.Sprintf("终端组%d", group.id))
		if !ok {
			t.Fatal("missing destination group", group.id)
		}
		x, y := r.X+r.W/2, r.Y+r.H/2
		switch edge {
		case "left":
			x = r.X + 6
		case "right":
			x = r.X + r.W - 6
		case "top":
			y = r.Y + 6
		case "bottom":
			y = r.Y + r.H - 6
		}
		dragTerminalTab(t, tt, tabLabel(label), x, y)
		tt.Frame()
		if len(a.terminalGroups()) != count {
			t.Fatalf("%s %s: expected %d panes, got %d", label, edge, count, len(a.terminalGroups()))
		}
	}
	split("Shell d", sessions[0].group, "right", 2)
	split("文件", sessions[0].group, "bottom", 3)
	split("Shell c", sessions[3].group, "bottom", 4)
	split("服务器状态", sessions[0].group, "top", 5)
	split("Shell b", sessions[2].group, "right", 6)
	for _, g := range a.terminalGroups() {
		r, ok := tt.Find(fmt.Sprintf("终端组%d", g.id))
		if !ok || r.W < 100 || r.H < 100 {
			t.Fatal("nested pane disappeared", g.id, r)
		}
	}
	if !slices.Equal(a.sessions, sessions) || files.source != source || status.source != source || a.primaryFilePane != pane || pane.browser() != browser {
		t.Fatal("a layout gesture replaced a connection, browser or captured server")
	}
	tt.Type("six panes input")
	expectTerminalInput(t, recorders[1], "six panes input")
	for _, i := range []int{0, 2, 3} {
		expectTerminalInput(t, recorders[i], "")
	}
	r, _ := tt.Find(fmt.Sprintf("终端组%d", sessions[0].group.id))
	dragTerminalTab(t, tt, tabLabel("文件"), r.X+r.W/2, r.Y+r.H/2)
	if len(a.terminalGroups()) != 5 || files.group != sessions[0].group {
		t.Fatal("moving the last tool did not collapse its empty pane")
	}
	a.mergeTerminalViews()
	tt.Frame()
	if len(a.terminalGroups()) != 1 || len(a.workspaceTabs(a.singleTerminalGroup)) != 6 {
		t.Fatal("merging lost peer tabs")
	}
	if !slices.Equal(a.sessions, sessions) || pane.browser() != browser || files.source != source {
		t.Fatal("merging replaced preserved content")
	}
	a.setToolsHidden(false)
	tt.Frame()
	dock, ok := tt.Find("工具面板")
	if !ok {
		t.Fatal("no dock to receive the independent file view")
	}
	dragTerminalTab(t, tt, tabLabel("文件"), dock.X+dock.W/2, dock.Y+dock.H/2)
	if a.workspaceTools[toolFiles] != nil || a.dock != toolFiles || a.primaryFilePane != pane || pane.browser() != browser {
		t.Fatal("dragging a file peer back to the dock lost its content")
	}
}

func TestWorkspaceClosingAnotherPaneDoesNotTrapFocusOnATool(t *testing.T) {
	a, recorders := terminalSplitFixture(t, 3)
	a.toolsHidden = true
	a.openWorkspaceTool(toolCommands)
	commands := a.workspaceTools[toolCommands]
	a.dropWorkspaceTab(a.workspaceTabPayload(workspaceTab{tool: commands}), a.sessions[0].group, workspaceTab{}, "right")
	a.dropWorkspaceTab(a.workspaceTabPayload(workspaceTab{session: a.sessions[2]}), a.sessions[0].group, workspaceTab{}, "bottom")
	a.activateWorkspaceTab(commands.group, workspaceTab{tool: commands})
	tt := ui.NewTester(a.view, 1280, 900)
	a.closeSessionAt(2)
	tt.Frame()
	if a.contentFocus {
		t.Fatal("closing an unrelated terminal requested focus behind a tool")
	}
	r, _ := tt.Find(fmt.Sprintf("终端组%d", a.sessions[0].group.id))
	tt.ClickAt(r.X+r.W/2, r.Y+r.H/2)
	tt.Type("focus restored")
	expectTerminalInput(t, recorders[0], "focus restored")
	expectTerminalInput(t, recorders[1], "")
}
