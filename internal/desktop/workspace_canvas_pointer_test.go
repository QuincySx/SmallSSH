//go:build terminal_integration

package desktop

import (
	"fmt"
	"github.com/egoist/mygo/ui"
	"testing"
)

func TestCanvasHeaderDragAggregatesAndSplitsWithoutRetargetingInput(t *testing.T) {
	a, recorders := terminalSplitFixture(t, 2)
	a.toolsHidden = true
	a.setWorkspaceCanvas(true)
	tt := ui.NewTester(a.view, 1280, 900)
	target, _ := tt.Find("画布面板：Shell b")
	dragTerminalTab(t, tt, "画布面板：Shell a", target.X+target.W/2, target.Y+target.H/2)
	if len(a.terminalGroups()) != 1 || len(a.workspaceTabs(a.singleTerminalGroup)) != 2 || !a.workspaceCanvas {
		t.Fatal("canvas header drop did not form a tab group")
	}
	if err := tt.Click("Shell a"); err != nil {
		t.Fatal(err)
	}
	tt.Type("grouped input")
	expectTerminalInput(t, recorders[0], "grouped input")
	expectTerminalInput(t, recorders[1], "")
	group, _ := tt.Find(fmt.Sprintf("终端组%d", a.singleTerminalGroup.id))
	dragTerminalTab(t, tt, "Shell a", group.X+group.W/2, group.Y+group.H-8)
	if len(a.terminalGroups()) != 2 || a.sessions[0].group == a.sessions[1].group {
		t.Fatal("grouped tab did not split back into a card")
	}
	tt.Type("split input")
	expectTerminalInput(t, recorders[0], "split input")
	expectTerminalInput(t, recorders[1], "")
}

func TestCanvasNewContainerSplitKeepsTargetAndPendingCard(t *testing.T) {
	a, fixture, updates := dockerUIFixture(t)
	a.workspaceCanvas = true
	a.openContainerTerminal(a.sessions[0], fixture.Target.Target, "web", "sh", nil)
	pumpDocker(t, updates, func() bool { return len(a.sessions) == 3 })
	source := a.sessions[2]
	a.activateTerminal(source, false)
	a.newCanvasTerminal(source, source.group, "bottom")
	if len(a.sessions) != 4 || a.sessions[3].connectionState == "" || a.sessions[3].target.Kind != "docker-terminal" || len(a.terminalGroups()) != 2 {
		t.Fatal("container split did not reserve its own pending card")
	}
	a.setWorkspaceCanvas(false)
	pumpDocker(t, updates, func() bool { return len(a.sessions) == 4 && a.sessions[3].term != nil })
	added := a.sessions[3]
	if a.workspaceCanvas || len(a.terminalGroups()) != 1 || added.target.Kind != "docker-terminal" || added.target.Docker.Target.ID != source.target.Docker.Target.ID {
		t.Fatal("completion changed presentation or connected to the host shell")
	}
}

func TestCanvasContainerSplitOutlivesItsOriginalCard(t *testing.T) {
	a, fixture, updates := dockerUIFixture(t)
	a.workspaceCanvas = true
	a.openContainerTerminal(a.sessions[0], fixture.Target.Target, "web", "sh", nil)
	pumpDocker(t, updates, func() bool { return len(a.sessions) == 3 })
	source := a.sessions[2]
	a.newCanvasTerminal(source, source.group, "right")
	pending := a.sessions[len(a.sessions)-1]
	a.closeSessionAt(a.sessionIndex(source))
	pumpDocker(t, updates, func() bool { return !a.hasSession(pending) })
	added := a.sessions[len(a.sessions)-1]
	if len(a.sessions) != 3 || added.term == nil || added.target.Kind != "docker-terminal" || added.target.Docker.Target.ID != fixture.Target.Target.ID {
		t.Fatal("closing the origin stranded or retargeted its reserved container card")
	}
}
