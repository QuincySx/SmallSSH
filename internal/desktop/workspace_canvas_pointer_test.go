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

func TestCanvasRepeatedContainerSplitsKeepDistinctTitles(t *testing.T) {
	a, fixture, updates := dockerUIFixture(t)
	a.workspaceCanvas = true
	a.openContainerTerminal(a.sessions[0], fixture.Target.Target, "web", "sh", nil)
	pumpDocker(t, updates, func() bool { return len(a.sessions) == 3 })
	source := a.sessions[2]
	a.newCanvasTerminal(source, source.group, "right")
	a.newCanvasTerminal(source, source.group, "bottom")
	if len(a.sessions) != 5 || a.sessions[3].name == a.sessions[4].name || a.sessions[3].name == source.name {
		t.Fatal("repeated container splits reserved cards with the same title", a.sessions[3].name, a.sessions[4].name)
	}
	pumpDocker(t, updates, func() bool { return len(a.sessions) == 5 && a.sessions[3].term != nil && a.sessions[4].term != nil })
	seen := map[string]bool{}
	for _, s := range a.sessions {
		if seen[s.name] {
			t.Fatal("connected container terminals share a title", s.name)
		}
		seen[s.name] = true
	}
}

func TestWorkspaceTopTabsRemainAvailableInBothPresentations(t *testing.T) {
	for _, canvas := range []bool{true, false} {
		t.Run(fmt.Sprintf("canvas=%t", canvas), func(t *testing.T) {
			a, recorders := terminalSplitFixture(t, 2)
			a.openWorkspaceTool(toolFiles)
			a.setWorkspaceCanvas(canvas)
			layout := a.terminalLayout
			files := a.workspaceTools[toolFiles]
			source := files.source
			owners := []*terminalGroup{a.sessions[0].group, a.sessions[1].group, files.group}
			tt := ui.NewTester(a.view, 1440, 900)
			tt.SetTitleBar(ui.TitleBar{Height: 48, Left: 70})
			label := func(name string) string {
				if canvas {
					return "顶部标签：" + name
				}
				return name
			}
			for _, name := range []string{"Shell a", "Shell b", "文件"} {
				r, ok := tt.Find(label(name))
				if !ok || r.Y < 0 || r.Y+r.H > 48 || r.W < 20 {
					t.Fatal("top workspace tab missing or outside the title bar", name, r)
				}
			}
			for _, index := range []int{1, 0} {
				if err := tt.Click(label(a.sessions[index].name)); err != nil {
					t.Fatal(err)
				}
				tt.Type("top tab input")
				expectTerminalInput(t, recorders[index], "top tab input")
				expectTerminalInput(t, recorders[1-index], "")
				if a.currentTerminal() != a.sessions[index] || a.activeTerminalGroup != owners[index] {
					t.Fatal("top tab did not focus its existing owner")
				}
			}
			if err := tt.Click(label("文件")); err != nil {
				t.Fatal(err)
			}
			if a.activeTerminalGroup != owners[2] || owners[2].selectedTool != files || files.source != source || a.terminalLayout != layout || a.workspaceCanvas != canvas || a.sessions[0].group != owners[0] || a.sessions[1].group != owners[1] {
				t.Fatal("top selection changed presentation, layout, owner or source")
			}
			closeLabel := "关闭文件标签"
			if canvas {
				closeLabel = "关闭顶部标签：文件"
			}
			if err := tt.Click(closeLabel); err != nil {
				t.Fatal(err)
			}
			if a.workspaceTools[toolFiles] != nil || len(a.sessions) != 2 || a.sessions[0].closed || a.sessions[1].closed {
				t.Fatal("closing a top tool tab affected SSH sessions")
			}
		})
	}
}

func TestWorkspaceTopTabsRevealOverflowAcrossCanvasGroups(t *testing.T) {
	a, _ := terminalSplitFixture(t, 16)
	a.toolsHidden = true
	a.setWorkspaceCanvas(true)
	tt := ui.NewTester(a.view, 900, 760)
	tt.SetTitleBar(ui.TitleBar{Height: 48, Left: 70})
	layout := a.terminalLayout
	for _, index := range []int{15, 0, 12, 1} {
		a.activateTerminal(a.sessions[index], true)
		for range 3 {
			tt.Frame()
		}
		strip, _ := tt.Find("顶部工作区选项卡")
		close, ok := tt.Find("关闭顶部标签：" + a.sessions[index].name)
		if !ok || close.Y < 0 || close.Y+close.H > 48 || close.X < strip.X || close.X+close.W > strip.X+strip.W-30 || a.terminalLayout != layout {
			t.Fatal("overflow did not reveal the active top tab and its close control", index, strip, close)
		}
	}
}
