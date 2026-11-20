package desktop

import (
	"os"
	"slices"
	"testing"

	"github.com/egoist/mygo/ui"
	"sshclient/internal/transfer"
)

func TestCanvasTileAndAggregatePreserveAllViewOwnersAndSources(t *testing.T) {
	a := fileFollowDemo()
	sessions := slices.Clone(a.sessions)
	for _, kind := range []int{toolFiles, toolCommands, toolDiagnostics, toolTransfers} {
		a.openWorkspaceTool(kind)
	}
	files, status, tasks := a.workspaceTools[toolFiles], a.workspaceTools[toolDiagnostics], a.workspaceTools[toolTransfers]
	pane, browser := a.primaryFilePane, a.primaryFilePane.browser()
	source := status.source
	for range 2 {
		a.setWorkspaceCanvas(true)
		if len(a.terminalGroups()) != len(sessions)+4 || !a.workspaceCanvas || a.activeTerminalGroup.selectedTool != tasks || !a.monitorWanted(source) {
			t.Fatal("canvas did not show the retained views and status source")
		}
		for _, group := range a.terminalGroups() {
			if len(a.workspaceTabs(group)) != 1 {
				t.Fatal("a tiled card owns hidden tabs")
			}
		}
		a.setWorkspaceCanvas(false)
		if len(a.terminalGroups()) != 1 || a.workspaceCanvas || a.activeTerminalGroup.selectedTool != tasks || a.monitorWanted(source) {
			t.Fatal("aggregation changed the selected tool or polled a covered status tab")
		}
		if !slices.Equal(a.sessions, sessions) || a.workspaceTools[toolFiles] != files || a.primaryFilePane != pane || pane.browser() != browser || status.source != source {
			t.Fatal("presentation changed a connection, file browser or server target")
		}
	}
}

func TestCanvasApplicationSwitchesAreIndependentAndDoNotCloseSSH(t *testing.T) {
	a := fileFollowDemo()
	a.workspaceCanvas, a.toolsHidden = true, true
	tt := ui.NewTester(a.view, 1440, 960)
	for _, title := range []string{"文件", "命令", "服务器状态"} {
		if err := tt.Click("画布功能：" + title); err != nil {
			t.Fatal(err)
		}
	}
	if len(a.workspaceTools) != 3 || len(a.terminalGroups()) != 4 {
		t.Fatal("an application switch replaced another canvas card")
	}
	if _, found := tt.Find("画布功能：任务"); found {
		t.Fatal("canvas retained a duplicate global task entry")
	}
	status := a.workspaceTools[toolDiagnostics]
	if err := tt.Click("画布功能：文件"); err != nil {
		t.Fatal(err)
	}
	if a.workspaceTools[toolFiles] != nil || a.workspaceTools[toolDiagnostics] != status || len(a.sessions) != 2 || a.sessions[0].closed {
		t.Fatal("closing one application affected a peer or SSH")
	}
}

func TestCanvasTaskQueueHasOnePresentationAcrossPopupAndExistingCard(t *testing.T) {
	a := fileFollowDemo()
	a.workspaceCanvas = true
	a.jobs = []transfer.Job{{ID: "fixture-task", Remote: "/example/file.txt", State: "done", Size: 100, Bytes: 100}}
	a.showTool(toolTransfers)
	tt := ui.NewTester(a.view, 1280, 900)
	if !a.transfersOpen || !tt.HasText("任务中心面板") || a.dock != toolFiles {
		t.Fatal("canvas task entry did not use the global popup")
	}
	if slices.Contains(tt.Texts(), "任务") {
		t.Fatal("canvas retained a duplicate task tool tab")
	}
	if err := tt.Click("任务中心"); err != nil {
		t.Fatal(err)
	}
	if a.transfersOpen {
		t.Fatal("global queue button did not close the popup")
	}
	a.openWorkspaceTool(toolTransfers)
	tt.Frame()
	task := a.workspaceTools[toolTransfers]
	a.transfersOpen = true
	tt.Frame()
	if a.transfersOpen {
		t.Fatal("a queue popup opened over its canvas owner")
	}
	if err := tt.Click("任务中心"); err != nil {
		t.Fatal(err)
	}
	if a.transfersOpen || a.activeTerminalGroup.selectedTool != task {
		t.Fatal("global queue button did not focus the existing canvas owner")
	}
	a.dockWorkspaceTool(task)
	tt.Frame()
	if a.workspaceTools[toolTransfers] != nil || !a.transfersOpen || a.dock != toolFiles || len(a.jobs) != 1 {
		t.Fatal("returning an existing card did not preserve the global queue")
	}
}

func TestCanvasPresentationAndAllowedDockSidePersist(t *testing.T) {
	for _, canvas := range []bool{true, false} {
		a := workspaceModelFixture(1)
		a.dir = t.TempDir()
		if err := os.Chmod(a.dir, 0700); err != nil {
			t.Fatal(err)
		}
		a.workspaceCanvas = canvas
		a.setToolsDockSide("left")
		if err := a.saveLayout(); err != nil {
			t.Fatal(err)
		}
		b := &app{dir: a.dir}
		b.loadLayout()
		if b.workspaceCanvas != canvas || b.savedToolsDockSide() != "bottom" {
			t.Fatal("presentation or dock migration did not persist")
		}
	}
}

func TestCanvasTerminalFolderActionExplicitlySelectsItsServer(t *testing.T) {
	a := fileFollowDemo()
	a.setWorkspaceCanvas(true)
	a.openCanvasFiles(a.sessions[0], a.sessions[0].group)
	files := a.workspaceTools[toolFiles]
	pane := a.activeFilePane
	oldBrowser := pane.browser()
	a.activateTerminal(a.sessions[1], false)
	a.syncFollowingFilePanes()
	if pane.endpoint != a.sessions[0].host {
		t.Fatal("an incidental terminal focus retargeted the file card")
	}
	a.openCanvasFiles(a.sessions[1], a.sessions[1].group)
	if a.workspaceTools[toolFiles] != files || files.source != a.sessions[1] || pane.endpoint != a.sessions[1].host || pane.states[a.sessions[0].host] != oldBrowser {
		t.Fatal("the explicit folder action did not show its server or discarded the previous browser")
	}
}

func TestCanvasFolderActionKeepsThePinnedLocalWindow(t *testing.T) {
	a := fileFollowDemo()
	a.setWorkspaceCanvas(true)
	a.openCanvasFiles(a.sessions[0], a.sessions[0].group)
	server := a.activeFilePane
	local := a.showLocalFileWindow(server)
	if local == nil || a.activeFilePane != local || local.follow {
		t.Fatal("showing local files did not select a pinned local window")
	}
	a.openCanvasFiles(a.sessions[1], a.sessions[1].group)
	a.syncFollowingFilePanes()
	if local.endpoint != localEndpoint || local.follow || !slices.Contains(a.fileLayout.panes(), local) {
		t.Fatal("the folder action turned the side-by-side local window into a server window")
	}
	if server.endpoint != a.sessions[1].host || a.activeFilePane != server {
		t.Fatal("the folder action did not show its server in the server window", server.endpoint)
	}
}

func TestCanvasToolEntriesReturnToTheWorkspacePage(t *testing.T) {
	a := fileFollowDemo()
	a.workspaceCanvas = true
	a.openWorkspaceTool(toolTransfers)
	a.openWorkspaceTool(toolFiles)
	tasks, files := a.workspaceTools[toolTransfers], a.workspaceTools[toolFiles]
	a.page = 0
	a.showTransfers()
	if a.page != 1 || a.transfersOpen || a.activeTerminalGroup.selectedTool != tasks {
		t.Fatal("the transfer entry selected a hidden canvas card")
	}
	a.page = 0
	a.showTool(toolFiles)
	if a.page != 1 || a.activeTerminalGroup.selectedTool != files {
		t.Fatal("the tool entry selected a hidden canvas card")
	}
}

func TestCanvasLayoutResetRetilesCardsWithTheToolAreaHidden(t *testing.T) {
	a := fileFollowDemo()
	a.setWorkspaceCanvas(true)
	a.openWorkspaceTool(toolCommands)
	a.mergeTerminalViews()
	a.sidebarHidden, a.toolsRight, a.fileExpanded = true, true, true
	a.resetWorkspaceLayout()
	if !a.workspaceCanvas || !a.toolsHidden || a.toolsRight || a.sidebarHidden || a.fileExpanded {
		t.Fatal("reset left the canvas with a non-default presentation")
	}
	if len(a.terminalGroups()) != len(a.sessions)+1 {
		t.Fatal("reset did not tile the grouped canvas cards", len(a.terminalGroups()))
	}
	for _, group := range a.terminalGroups() {
		if len(a.workspaceTabs(group)) != 1 {
			t.Fatal("a reset canvas card still owns hidden tabs")
		}
	}
}

func TestContainerTerminalTitlesStayDistinct(t *testing.T) {
	a := &app{sessions: []*session{{name: "容器 · web"}}}
	first := &session{}
	a.sessions = append(a.sessions, first)
	first.name = a.containerTerminalName("web", first)
	second := &session{}
	a.sessions = append(a.sessions, second)
	second.name = a.containerTerminalName("web", second)
	if first.name != "容器 · web (2)" || second.name != "容器 · web (3)" {
		t.Fatal("repeated container placeholders share a title", first.name, second.name)
	}
	attached := &session{}
	a.sessions = append(a.sessions, attached)
	if got := a.containerTerminalName("web", attached, first); got != first.name {
		t.Fatal("a connected terminal did not keep its placeholder's title", got)
	}
}
