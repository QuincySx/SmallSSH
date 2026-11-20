package desktop

import (
	"context"
	"fmt"
	"slices"
	"testing"

	"github.com/egoist/mygo/ui"
)

func workspaceModelFixture(count int) *app {
	a := &app{ctx: context.Background(), online: true, page: 1}
	for i := 0; i < count; i++ {
		a.sessions = append(a.sessions, &session{name: fmt.Sprintf("Terminal %d", i), host: fmt.Sprint(i)})
	}
	a.ensureTerminalGroups()
	return a
}

func TestWorkspaceMenuKeepsItsOriginAndRejectsChangedOwnership(t *testing.T) {
	a := workspaceModelFixture(2)
	source := a.sessions[0]
	a.openWorkspaceTool(toolDiagnostics)
	tool := a.workspaceTools[toolDiagnostics]
	a.renderingWorkspaceTool = tool
	var selected *session
	menu := a.workspaceBoundMenu(func(*ui.Menu) { selected = a.serverStateSession() })
	a.renderingWorkspaceTool = nil
	a.activateTerminal(a.sessions[1], true)
	menu(&ui.Menu{})
	if selected != source || a.renderingWorkspaceTool != nil {
		t.Fatal("deferred menu changed target or leaked its render scope")
	}
	selected = nil
	tool.source = a.sessions[1]
	menu(&ui.Menu{})
	if selected != nil {
		t.Fatal("a menu survived replacement of its originating connection")
	}
	tool.source = source
	a.closeWorkspaceTab(workspaceTab{tool: tool})
	menu(&ui.Menu{})
	if selected != nil {
		t.Fatal("a closed view retained actionable menu callbacks")
	}
}

func TestWorkspaceRecursiveSplitsAndMergeRetainAllSixViews(t *testing.T) {
	a := workspaceModelFixture(6)
	sessions := slices.Clone(a.sessions)
	for i, edge := range []string{"right", "bottom", "left", "top", "right"} {
		groups := a.terminalGroups()
		a.dropWorkspaceTab(a.workspaceTabPayload(workspaceTab{session: sessions[i+1]}), groups[len(groups)-1], workspaceTab{}, edge)
		if len(a.terminalGroups()) != i+2 || len(a.terminalLayout.sessions()) != 6 {
			t.Fatalf("split %s lost views: groups=%d", edge, len(a.terminalGroups()))
		}
	}
	for _, source := range sessions {
		if !slices.Contains(a.terminalGroups(), source.group) || !slices.Contains(source.group.sessions, source) {
			t.Fatal("split replaced a session or its owner", source.name)
		}
	}
	a.mergeTerminalViews()
	if a.terminalLayout != nil || len(a.terminalGroups()) != 1 || len(a.singleTerminalGroup.sessions) != 6 || !slices.Equal(a.sessions, sessions) {
		t.Fatal("merge replaced, duplicated or lost sessions")
	}
}

func TestWorkspaceToolMovesBetweenPeerGroupsWithoutRetargeting(t *testing.T) {
	a := workspaceModelFixture(2)
	source := a.currentTerminal()
	a.openWorkspaceTool(toolFiles)
	file := a.workspaceTools[toolFiles]
	a.openWorkspaceTool(toolCommands)
	commands := a.workspaceTools[toolCommands]
	a.dropWorkspaceTab(a.workspaceTabPayload(workspaceTab{tool: file}), a.activeTerminalGroup, workspaceTab{}, "bottom")
	if len(a.terminalGroups()) != 2 || file.group == commands.group || file.source != source || !a.filesTabOpen {
		t.Fatal("file view did not become a peer pane")
	}
	a.activateTerminal(a.sessions[1], true)
	if file.source != source || commands.source != source || file.group.selectedTool != file {
		t.Fatal("changing terminal focus retargeted a promoted view")
	}
	a.renderingWorkspaceTool = commands
	if a.workspaceCommandTarget() != source || a.serverStateSession() != source {
		t.Fatal("promoted operations target the globally focused server")
	}
	a.renderingWorkspaceTool = nil
	a.mergeTerminalViews()
	if file.group != a.singleTerminalGroup || commands.group != a.singleTerminalGroup || len(a.singleTerminalGroup.tools) != 2 {
		t.Fatal("merging groups lost tools")
	}
	a.dockWorkspaceTool(file)
	if a.workspaceTools[toolFiles] != nil || a.filesTabOpen || a.dock != toolFiles || a.workspaceTools[toolCommands] != commands {
		t.Fatal("returning files to the dock affected another view")
	}
}

func TestWorkspaceRejectsStaleAndForeignDragsAndDoesNotSplitOnlyTab(t *testing.T) {
	a := workspaceModelFixture(1)
	target := a.singleTerminalGroup
	drag := a.workspaceTabPayload(workspaceTab{session: a.sessions[0]})
	a.dropWorkspaceTab(drag, target, workspaceTab{}, "right")
	if a.terminalLayout != nil {
		t.Fatal("one view was cloned into two panes")
	}
	for _, mutate := range []func(*terminalTabDrag){func(d *terminalTabDrag) { d.generation++ }, func(d *terminalTabDrag) { d.owner = &app{} }} {
		d := drag
		mutate(&d)
		a.dropWorkspaceTab(d, target, workspaceTab{}, "bottom")
		if a.terminalLayout != nil || len(a.sessions) != 1 {
			t.Fatal("untrusted drag changed the tree")
		}
	}
}

func TestWorkspaceDockDragChoosesSidesOrPromotesAtCenter(t *testing.T) {
	a := workspaceModelFixture(1)
	g := a.singleTerminalGroup
	drag := terminalTabDrag{owner: a, generation: a.generation, fromDock: true, dockKind: toolCommands, toolSource: a.currentTerminal()}
	for _, side := range []string{"right", "bottom"} {
		a.dropWorkspaceTab(drag, g, workspaceTab{}, side)
		if a.savedToolsDockSide() != side || a.workspaceTools[toolCommands] != nil {
			t.Fatal("edge docking unexpectedly promoted a tool", side)
		}
	}
	a.dropWorkspaceTab(drag, g, workspaceTab{}, "")
	if a.workspaceTools[toolCommands] == nil || len(a.workspaceTabs(g)) != 2 {
		t.Fatal("center drop did not promote into the common tab strip")
	}
	a.lock()
	if a.workspaceTools != nil || a.renderingWorkspaceTool != nil || len(a.sessions) != 0 {
		t.Fatal("locking retained workspace session references")
	}
}

func TestWorkspaceDropEdgesCoverFourSidesAndLeaveCenterUnsplit(t *testing.T) {
	for _, item := range []struct {
		x, y float32
		want string
	}{{5, 100, "left"}, {395, 100, "right"}, {200, 5, "top"}, {200, 195, "bottom"}, {200, 100, ""}} {
		if got := workspaceDropEdge(item.x, item.y, 400, 200); got != item.want {
			t.Errorf("drop %v,%v: %q want %q", item.x, item.y, got, item.want)
		}
	}
}

func TestWorkspaceToolCloseActionsNeverCloseItsSourceTerminal(t *testing.T) {
	for _, kind := range []int{toolFiles, toolCommands, toolDiagnostics} {
		for _, hosted := range []bool{false, true} {
			a := workspaceModelFixture(2)
			a.openWorkspaceTool(kind)
			tool := a.workspaceTools[kind]
			if hosted {
				origin := a.captureWindowMenuOrigin()
				a.activateTerminal(a.sessions[1], true)
				a.runHostedWindowMenuAction(menuAction{shortcut: "Cmd+W"}, origin)
			} else {
				for _, group := range a.menuGroups() {
					for _, action := range group.actions {
						if action.label == "关闭当前标签" {
							action.run()
						}
					}
				}
			}
			if len(a.sessions) != 2 || a.workspaceTools[kind] == tool || a.sessions[0].closed {
				t.Fatal("closing a tool tab closed or retained the wrong view", kind, hosted)
			}
		}
	}
}

func TestWorkspaceDockChangesPreserveAnActiveIndependentFileView(t *testing.T) {
	a := workspaceModelFixture(1)
	a.openWorkspaceTool(toolFiles)
	file := a.workspaceTools[toolFiles]
	for _, side := range []string{"left", "top", "right", "bottom"} {
		a.setToolsDockSide(side)
		a.showTool(toolCommands)
		a.ensureTerminalGroups()
		if file.group.selectedTool != file || !a.filesTabSelected || a.contentFocus {
			t.Fatal("a dock operation deselected its peer file view", side)
		}
	}
}

func TestWorkspaceLayoutMenuMovesTheSelectedToolRatherThanItsSource(t *testing.T) {
	a := workspaceModelFixture(1)
	a.toolsHidden = true
	a.openWorkspaceTool(toolCommands)
	tool, source := a.workspaceTools[toolCommands], a.currentTerminal()
	tt := ui.NewTester(a.view, 1280, 900)
	if err := rightClickWorkspaceTab(tt, a, workspaceTab{tool: tool}); err != nil {
		t.Fatal(err)
	}
	if err := tt.ChooseMenuItem("移动当前标签到另一组"); err != nil {
		t.Fatal(err)
	}
	if len(a.terminalGroups()) != 2 || tool.group == source.group || source.group.active != source || tool.source != source || len(a.sessions) != 1 {
		t.Fatal("the layout menu moved the underlying SSH session instead of its peer tool")
	}
}

func TestWorkspaceCardsHavePlainHeadersAndOnlyTopTabs(t *testing.T) {
	for _, canvas := range []bool{false, true} {
		t.Run(fmt.Sprintf("canvas=%t", canvas), func(t *testing.T) {
			a := workspaceModelFixture(3)
			a.toolsHidden, a.workspaceCanvas = true, canvas
			a.splitExistingTerminal(a.sessions[2])
			first, peer, other := a.sessions[0], a.sessions[1], a.sessions[2]
			a.activateTerminal(first, false)
			owner, layout := first.group, a.terminalLayout
			tt := ui.NewTester(a.view, 1280, 900)
			tt.SetTitleBar(ui.TitleBar{Height: 48, Left: 70})
			for _, source := range []*session{peer, first} {
				label := "顶部标签：" + source.name
				if err := tt.Click(label); err != nil {
					t.Fatal(err)
				}
				for _, group := range a.terminalGroups() {
					if _, ok := tt.Find(fmt.Sprintf("终端栏标题%d", group.id)); ok {
						t.Fatal("a pane rendered a nested tab strip", group.id)
					}
					selected := a.selectedWorkspaceTab(group)
					if r, ok := tt.Find("画布面板：" + a.workspaceTabTitle(nil, selected)); !ok || r.Y < 48 {
						t.Fatal("pane lost its plain current-view header", r)
					}
				}
				if first.group != owner || peer.group != owner || owner.active != source || a.activeTerminalGroup != owner || a.terminalLayout != layout || len(a.workspaceTabs(owner)) != 2 {
					t.Fatal("top navigation moved or lost a retained view")
				}
			}
			if err := tt.Click("关闭标签：" + first.name); err != nil {
				t.Fatal(err)
			}
			if len(a.sessions) != 2 || !slices.Contains(a.sessions, peer) || !slices.Contains(a.sessions, other) || owner.active != peer {
				t.Fatal("plain-header close discarded a peer view or failed to reveal it")
			}
			if _, ok := tt.Find("画布面板：" + peer.name); !ok {
				t.Fatal("plain header did not follow the remaining view")
			}
		})
	}
}

func rightClickWorkspaceTab(tt *ui.Tester, a *app, tab workspaceTab) error {
	label := a.workspaceTabTitle(nil, tab)
	if _, ok := tt.Find("顶部标签：" + label); ok {
		label = "顶部标签：" + label
	}
	return tt.RightClick(label)
}
