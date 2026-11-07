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
	for _, title := range []string{"文件", "命令", "服务器状态", "任务"} {
		if err := tt.Click("画布功能：" + title); err != nil {
			t.Fatal(err)
		}
	}
	if len(a.workspaceTools) != 4 || len(a.terminalGroups()) != 5 {
		t.Fatal("an application switch replaced another canvas card")
	}
	task := a.workspaceTools[toolTransfers]
	if err := tt.Click("画布功能：文件"); err != nil {
		t.Fatal(err)
	}
	if a.workspaceTools[toolFiles] != nil || a.workspaceTools[toolTransfers] != task || len(a.sessions) != 2 || a.sessions[0].closed {
		t.Fatal("closing one application affected a peer or SSH")
	}
}

func TestCanvasTaskQueueHasOnePresentationAcrossDockPopupAndCard(t *testing.T) {
	a := fileFollowDemo()
	a.workspaceCanvas = true
	a.jobs = []transfer.Job{{ID: "fixture-task", Remote: "/example/file.txt", State: "done", Size: 100, Bytes: 100}}
	a.showTool(toolTransfers)
	tt := ui.NewTester(a.view, 1280, 900)
	a.transfersOpen = true // Downloads also request the global popup asynchronously.
	tt.Frame()
	if a.transfersOpen {
		t.Fatal("a queue popup opened over its dock owner")
	}
	if err := tt.Click("传输任务"); err != nil {
		t.Fatal(err)
	}
	if a.transfersOpen {
		t.Fatal("global queue button duplicated the dock")
	}
	a.openWorkspaceTool(toolTransfers)
	tt.Frame()
	task := a.workspaceTools[toolTransfers]
	a.transfersOpen = true
	tt.Frame()
	if a.transfersOpen {
		t.Fatal("a queue popup opened over its canvas owner")
	}
	a.dockWorkspaceTool(task)
	tt.Frame()
	if a.workspaceTools[toolTransfers] != nil || a.dock != toolTransfers || len(a.jobs) != 1 {
		t.Fatal("docking discarded the queue")
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
