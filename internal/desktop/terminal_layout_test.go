//go:build terminal_integration

package desktop

import (
	"fmt"
	"slices"
	"strings"
	"testing"

	"github.com/egoist/mygo/plugins/terminal"
	"github.com/egoist/mygo/ui"
	"github.com/egoist/mygo/ui/editor"
	"sshclient/internal/monitor"
	"sshclient/internal/vault"
)

func terminalSplitFixture(t *testing.T, count int) (*app, []*terminalRecorder) {
	t.Helper()
	a := fileFollowDemo()
	a.sessions = nil
	var recorders []*terminalRecorder
	for i := range count {
		recorder := newTerminalRecorder()
		term, err := terminal.New(terminal.Options{Conn: recorder, ClipboardRead: terminal.ClipboardDeny, NoClipboardWrite: true})
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = term.Close() })
		term.Feed([]byte(fmt.Sprintf("pane-%d preserved output\r\n$ ", i)))
		host := string(rune('a' + i))
		a.sessions = append(a.sessions, &session{name: "Shell " + host, host: host, term: term, stats: &monitor.Stats{
			MemoryTotal: 512 << 20, MemoryUsed: uint64(i+1) << 20,
			Processes: []monitor.Process{{PID: i + 100, Name: "process-" + host, CPU: float64(i + 1)}},
		}})
		recorders = append(recorders, recorder)
	}
	return a, recorders
}

func TestSplitTerminalClickRoutesKeysAndSharedToolsToFocusedPane(t *testing.T) {
	a, recorders := terminalSplitFixture(t, 2)
	a.ensureTerminalGroups()
	a.splitExistingTerminal(a.sessions[1])
	a.showTool(toolDiagnostics)
	a.diagnosticTab = 1
	tt := ui.NewTester(a.view, 1280, 900)
	for _, index := range []int{1, 0, 1} {
		s := a.sessions[index]
		r, ok := tt.Find(fmt.Sprintf("终端组%d", s.group.id))
		if !ok {
			t.Fatal("terminal view missing", s.name)
		}
		tt.ClickAt(r.X+r.W/2, r.Y+80)
		tt.Type("中文 'literal' ")
		tt.Key(0, ui.KeyUp)
		tt.Key(ui.Ctrl, ui.KeyC)
		expectTerminalInput(t, recorders[index], "中文 'literal' \x1b[A\x03")
		if recorders[1-index].take() != "" {
			t.Fatal("input was broadcast to another SSH session")
		}
		if a.currentTerminal() != s || a.serverStateSession() != s || a.primaryFilePane.endpoint != s.host || !tt.HasText("process-"+s.host) {
			t.Fatal("shared tools did not follow the focused terminal", a.active, a.primaryFilePane.endpoint, tt.Texts())
		}
	}
	a.chooseFileEndpoint(a.primaryFilePane, "c")
	r, _ := tt.Find(fmt.Sprintf("终端组%d", a.sessions[0].group.id))
	tt.ClickAt(r.X+r.W/2, r.Y+80)
	if a.primaryFilePane.endpoint != "c" || a.primaryFilePane.follow || a.serverStateSession() != a.sessions[0] {
		t.Fatal("terminal focus redirected a manually pinned file window")
	}
}

func TestTerminalGroupMergeRetainsTabsConnectionsAndSharedTools(t *testing.T) {
	a, recorders := terminalSplitFixture(t, 4)
	a.ensureTerminalGroups()
	a.splitExistingTerminal(a.sessions[1])
	a.moveTerminalTab(a.sessions[2], a.terminalLayout.right, nil)
	tt := ui.NewTester(a.view, 1280, 900)
	panel, _ := tt.Find("工具面板")
	if len(a.terminalLayout.left.sessions) != 2 || len(a.terminalLayout.right.sessions) != 2 {
		t.Fatal("editor-style groups did not own separate tab sets")
	}
	if err := tt.Click("终端分栏"); err != nil {
		t.Fatal(err)
	}
	for _, forbidden := range []string{"向下拆分", "上下排列", "网格排列"} {
		if slices.Contains(tt.Menu(), forbidden) {
			t.Fatal("vertical/grid terminal action remains", tt.Menu())
		}
	}
	if err := tt.ChooseMenuItem("合并全部分组"); err != nil {
		t.Fatal(err)
	}
	tt.Type("keep typing")
	expectTerminalInput(t, recorders[2], "keep typing")
	for i, s := range a.sessions {
		if !strings.Contains(s.term.Text(), fmt.Sprintf("pane-%d preserved output", i)) {
			t.Fatal("merge lost scrollback", i)
		}
		if i != 2 && recorders[i].take() != "" {
			t.Fatal("merge redirected keyboard focus", i)
		}
		select {
		case <-s.term.Done():
			t.Fatal("merge closed a connection")
		default:
		}
	}
	if current, _ := tt.Find("工具面板"); current != panel || len(a.sessions) != 4 || a.terminalLayout != nil || len(a.singleTerminalGroup.sessions) != 4 {
		t.Fatal("merge discarded tabs or changed shared tools")
	}
}

func TestTerminalGroupsCloseOnlyChosenSessionAndCollapseEmptyGroup(t *testing.T) {
	a, recorders := terminalSplitFixture(t, 3)
	a.ensureTerminalGroups()
	first, second, third := a.sessions[0], a.sessions[1], a.sessions[2]
	a.splitExistingTerminal(third)
	tt := ui.NewTester(a.view, 1280, 900)
	if err := tt.Click("关闭标签：" + third.name); err != nil {
		t.Fatal(err)
	}
	if len(a.sessions) != 2 || a.hasSession(third) || a.terminalLayout != nil {
		t.Fatal("last tab did not collapse its empty group")
	}
	for _, s := range []*session{first, second} {
		select {
		case <-s.term.Done():
			t.Fatal("closing one tab disconnected another")
		default:
		}
	}
	tt.Type("remaining")
	expectTerminalInput(t, recorders[a.active], "remaining")
}

func TestFailedTerminalSplitLeavesExistingLayoutAndSessionIntact(t *testing.T) {
	a, recorders := terminalSplitFixture(t, 1)
	tt := ui.NewTester(a.view, 1000, 760)
	if err := tt.Click("终端分栏"); err != nil {
		t.Fatal(err)
	}
	if err := tt.ChooseMenuItem("新建终端到另一组"); err != nil {
		t.Fatal(err)
	}
	// This fixture deliberately has no owner capability or production vault.
	if len(a.sessions) != 1 || a.terminalLayout != nil || a.status == "" {
		t.Fatal("failed authentication left a phantom split")
	}
	if err := tt.Click("Terminal"); err != nil {
		t.Fatal(err)
	}
	tt.Type("still connected")
	expectTerminalInput(t, recorders[0], "still connected")
}

func TestExistingTerminalTabMovesBetweenGroupsWithoutNewConnections(t *testing.T) {
	a, _ := terminalSplitFixture(t, 3)
	tt := ui.NewTester(a.view, 1280, 900)
	if err := tt.RightClick("Shell b"); err != nil {
		t.Fatal(err)
	}
	if err := tt.ChooseMenuItem("移到右侧组"); err != nil {
		t.Fatal(err)
	}
	if a.terminalLayout == nil || len(a.terminalLayout.left.sessions) != 2 || len(a.terminalLayout.right.sessions) != 1 {
		t.Fatal("tab did not move to its own right group")
	}
	if err := tt.RightClick("Shell b"); err != nil {
		t.Fatal(err)
	}
	if err := tt.ChooseMenuItem("移到左侧组"); err != nil {
		t.Fatal(err)
	}
	if a.terminalLayout != nil || len(a.sessions) != 3 || len(a.singleTerminalGroup.sessions) != 3 {
		t.Fatal("moving the final tab did not collapse an empty group")
	}
}

func TestNewTerminalSplitAuthenticatesThroughBrokerAndCreatesSeparatePTY(t *testing.T) {
	h := newConnectionFlowHarness(t)
	p := h.fixture.Profile()
	if err := h.a.client.SaveConnection(h.ctx, vault.ProfileChange{Profile: p}, h.server.Vault); err != nil {
		t.Fatal(err)
	}
	h.a.page, h.a.selected = 1, p.ID
	h.a.connections = []vault.Summary{{ID: p.ID, Name: "Loopback", Host: p.Host, Port: p.Port, User: p.User}}
	h.ui = ui.NewTester(h.a.view, 1100, 760)
	h.a.openShell()
	h.wait(func() bool { return len(h.a.sessions) == 1 })
	first := h.a.sessions[0]
	h.a.selected = "unrelated-sidebar-server"
	if err := h.ui.Click("终端分栏"); err != nil {
		t.Fatal(err)
	}
	if err := h.ui.ChooseMenuItem("新建终端到另一组"); err != nil {
		t.Fatal(err)
	}
	h.wait(func() bool { return len(h.a.sessions) == 2 })
	second := h.a.sessions[1]
	if h.a.terminalLayout == nil || len(h.a.terminalLayout.sessions()) != 2 || first.term == second.term || first.requestID == second.requestID || h.a.currentTerminal() != second || second.host != first.host || h.a.selected != "unrelated-sidebar-server" {
		t.Fatal("new split did not own a separate authorized SSH PTY")
	}
	if h.a.pass != "" || h.a.ownerPass != "" || h.a.approvalWin != nil {
		t.Fatal("split retained an unlock password or raised an approval window")
	}
	if first.name == second.name {
		t.Fatal("same-server terminals have indistinguishable labels")
	}
	before := h.a.terminalLayout
	h.a.newSplitTerminal(second)
	h.a.activateTerminal(first, true)
	h.wait(func() bool { return len(h.a.sessions) == 3 })
	if h.a.currentTerminal() != first || h.a.terminalLayout != before {
		t.Fatal("a delayed connection stole focus or changed a later layout")
	}
	_ = first.term.Close()
	h.wait(func() bool { return first.closed })
	h.a.reconnectTerminal(first)
	h.wait(func() bool { return !h.a.hasSession(first) })
	reconnected := h.a.sessions[0]
	if len(h.a.sessions) != 3 || reconnected.name != first.name || reconnected.requestID == first.requestID || h.a.terminalLayout.sessions()[0] != reconnected || h.a.currentTerminal() != reconnected || reconnected.host != first.host || h.a.selected != "unrelated-sidebar-server" {
		t.Fatal("reconnecting one of several same-server PTYs replaced the wrong slot")
	}
	h.a.mergeTerminalViews()
	if len(h.a.sessions) != 3 || h.a.terminalLayout != nil {
		t.Fatal("merging discarded a live connection")
	}
}

func dragTerminalTab(t *testing.T, tt *ui.Tester, label string, x, y float32) {
	t.Helper()
	r, ok := tt.Find(label)
	if !ok {
		t.Fatal("missing tab", label)
	}
	sourceX, sourceY := r.X+r.W/2, r.Y+r.H/2
	tt.Press(sourceX, sourceY)
	tt.Move(sourceX+12, sourceY)
	tt.Move(x, y)
	tt.Release(x, y)
}

func TestTerminalTabDragAcrossGroupsAndReorderRetainsLocalSelection(t *testing.T) {
	a, recorders := terminalSplitFixture(t, 4)
	a.ensureTerminalGroups()
	a.splitExistingTerminal(a.sessions[1])
	tt := ui.NewTester(a.view, 1280, 900)
	left, right := a.terminalLayout.left, a.terminalLayout.right
	r, _ := tt.Find(fmt.Sprintf("终端组%d", right.id))
	dragTerminalTab(t, tt, "Shell c", r.X+r.W/2, r.Y+100)
	if !slices.Equal(right.sessions, []*session{a.sessions[1], a.sessions[2]}) || len(left.sessions) != 2 || a.activeTerminalGroup != right {
		t.Fatal("cross-group drag lost ownership or order")
	}
	tt.Type("right input")
	expectTerminalInput(t, recorders[2], "right input")
	if err := tt.Click("Shell b"); err != nil {
		t.Fatal(err)
	}
	if right.active != a.sessions[1] || left.active != a.sessions[0] {
		t.Fatal("one group changed the other group's selected tab")
	}
	r, _ = tt.Find(fmt.Sprintf("终端组%d", left.id))
	dragTerminalTab(t, tt, "Shell b", r.X+r.W/2, r.Y+100)
	before, _ := tt.Find("Shell a")
	dragTerminalTab(t, tt, "Shell b", before.X+before.W/2, before.Y+before.H/2)
	if left.sessions[0] != a.sessions[1] || len(a.sessions) != 4 || right.active != a.sessions[2] {
		t.Fatal("tab reorder replaced a connection or changed the other group")
	}
	tt.Type("moved input")
	expectTerminalInput(t, recorders[1], "moved input")
	for _, index := range []int{0, 2, 3} {
		if recorders[index].take() != "" {
			t.Fatal("drag routed input to a different PTY", index)
		}
	}
}

func TestTerminalTabDragToRightEdgeCreatesGroupAndMovingBackCollapsesIt(t *testing.T) {
	a, _ := terminalSplitFixture(t, 3)
	tt := ui.NewTester(a.view, 1280, 900)
	r, _ := tt.Find(fmt.Sprintf("终端组%d", a.singleTerminalGroup.id))
	dragTerminalTab(t, tt, "Shell c", r.X+r.W*.9, r.Y+100)
	if a.terminalLayout == nil || len(a.terminalLayout.left.sessions) != 2 || len(a.terminalLayout.right.sessions) != 1 {
		t.Fatal("right-edge drop did not create a separate tab group")
	}
	left := a.terminalLayout.left
	r, _ = tt.Find(fmt.Sprintf("终端组%d", left.id))
	dragTerminalTab(t, tt, "Shell c", r.X+r.W/2, r.Y+100)
	if a.terminalLayout != nil || len(a.singleTerminalGroup.sessions) != 3 {
		t.Fatal("last-tab move did not collapse the empty group")
	}
}

func TestEditorTabMovesBetweenTerminalGroupsWithoutLosingDocument(t *testing.T) {
	a, recorders := terminalSplitFixture(t, 2)
	a.ensureTerminalGroups()
	a.splitExistingTerminal(a.sessions[1])
	a.activateTerminal(a.sessions[0], false)
	a.installEditor(localEndpoint, "/example/notes.txt", "preserved document", "fixture-hash")
	a.activateTerminal(a.sessions[1], true)
	tt := ui.NewTester(a.view, 1280, 900)
	if _, ok := tt.Find("文件编辑器"); !ok {
		t.Fatal("inactive group's editor disappeared")
	}
	tt.Type("terminal only")
	expectTerminalInput(t, recorders[1], "terminal only")
	if a.edit.Buffer.String() != "preserved document" {
		t.Fatal("terminal input reached another group's editor")
	}
	if err := tt.Click("文件编辑器"); err != nil {
		t.Fatal(err)
	}
	if !a.editShown || a.activeTerminalGroup != a.editorTerminalGroup {
		t.Fatal("clicking an inactive editor did not activate its group")
	}
	right := a.terminalLayout.right
	r, _ := tt.Find(fmt.Sprintf("终端组%d", right.id))
	dragTerminalTab(t, tt, "编辑: notes.txt", r.X+r.W/2, r.Y+100)
	if a.editorTerminalGroup != right || a.edit.Buffer.String() != "preserved document" {
		t.Fatal("moving editor tab lost document state")
	}
	if err := tt.Click("关闭标签：编辑: notes.txt"); err != nil {
		t.Fatal(err)
	}
	tt.Type("back to shell")
	expectTerminalInput(t, recorders[1], "back to shell")
}

func TestTerminalGroupCloseAndRestoreControlsFitNarrowRightDock(t *testing.T) {
	a, _ := terminalSplitFixture(t, 2)
	a.ensureTerminalGroups()
	a.splitExistingTerminal(a.sessions[1])
	a.toolsRight = true
	a.sidebarWidth = 390
	tt := ui.NewTester(a.view, 1000, 760)
	tt.Frame()
	for _, label := range []string{"关闭标签：Shell a", "关闭标签：Shell b", "隐藏工具面板", "锁定"} {
		r, ok := tt.Find(label)
		if !ok || r.W < 24 || r.X < 0 || r.X+r.W > 1000 {
			left, _ := tt.Find(fmt.Sprintf("终端组%d", a.terminalLayout.left.id))
			right, _ := tt.Find(fmt.Sprintf("终端组%d", a.terminalLayout.right.id))
			tab, _ := tt.Find("Shell b")
			tools, _ := tt.Find("隐藏工具面板")
			t.Fatal("narrow grouped layout clipped an essential control", label, r, "groups", left, right, "tab", tab, "tools", tools)
		}
	}
}

func TestSelectingOverflowTerminalTabRevealsItsCloseWithoutChangingOtherGroup(t *testing.T) {
	a, recorders := terminalSplitFixture(t, 8)
	a.ensureTerminalGroups()
	a.splitExistingTerminal(a.sessions[7])
	a.activateTerminal(a.sessions[0], true)
	tt := ui.NewTester(a.view, 1000, 760)
	left, right := a.terminalLayout.left, a.terminalLayout.right
	a.activateTerminal(a.sessions[6], true)
	tt.Frame()
	group, _ := tt.Find(fmt.Sprintf("终端组%d", left.id))
	close, ok := tt.Find("关闭标签：Shell g")
	if !ok || close.W < 24 || close.X < group.X || close.X+close.W > group.X+group.W || right.active != a.sessions[7] {
		t.Fatal("selected tab stayed outside its group or changed the other group's tab", close, group)
	}
	tt.Type("selected overflow tab")
	expectTerminalInput(t, recorders[6], "selected overflow tab")
	expectTerminalInput(t, recorders[7], "")
}

func TestTerminalTabContextMenuTargetsClickedSessionAndKeepsLocalScope(t *testing.T) {
	a, recorders := terminalSplitFixture(t, 2)
	a.toolsHidden = true
	first, second := a.sessions[0], a.sessions[1]
	a.activateTerminal(first, true)
	a.selected = "unrelated-sidebar-server"
	tt := ui.NewTester(a.view, 1000, 760)
	if err := tt.RightClick(second.name); err != nil {
		t.Fatal(err)
	}
	for _, label := range []string{"新建终端标签", "关闭此标签", "移到右侧组"} {
		if !slices.Contains(tt.Menu(), label) {
			t.Fatal("tab menu lost a terminal action", label, tt.Menu())
		}
	}
	for _, label := range []string{"授权中心", "安全设置", "立即锁定", "文件", "向右拆分", "关闭窗口", "工具面板独立窗口"} {
		if slices.Contains(tt.Menu(), label) {
			t.Fatal("terminal tab menu contains unrelated actions", label)
		}
	}
	// This synthetic fixture has no unlocked broker. The attempt still proves
	// the target is the clicked session before the normal authorization guard.
	if err := tt.ChooseMenuItem("新建终端标签"); err != nil {
		t.Fatal(err)
	}
	if a.selected != "unrelated-sidebar-server" || a.currentTerminal() != second {
		t.Fatal("new-tab action used the sidebar or previously selected terminal")
	}
	a.activateTerminal(first, true)
	tt.Frame()
	if err := tt.RightClick(second.name); err != nil {
		t.Fatal(err)
	}
	if err := tt.ChooseMenuItem("关闭此标签"); err != nil {
		t.Fatal(err)
	}
	if a.hasSession(second) || !a.hasSession(first) || a.currentTerminal() != first {
		t.Fatal("context close acted on another terminal")
	}
	tt.Type("surviving session")
	expectTerminalInput(t, recorders[0], "surviving session")
}

func TestNewTerminalTabCompletionRespectsLaterTerminalEditorAndPageSelection(t *testing.T) {
	h := newConnectionFlowHarness(t)
	p := h.fixture.Profile()
	if err := h.a.client.SaveConnection(h.ctx, vault.ProfileChange{Profile: p}, h.server.Vault); err != nil {
		t.Fatal(err)
	}
	h.a.page, h.a.selected = 1, p.ID
	h.a.connections = []vault.Summary{{ID: p.ID, Name: "Loopback", Host: p.Host, Port: p.Port, User: p.User}}
	h.ui = ui.NewTester(h.a.view, 1100, 760)
	h.a.openShell()
	h.wait(func() bool { return len(h.a.sessions) == 1 })
	h.a.openShellMode(true)
	h.wait(func() bool { return len(h.a.sessions) == 2 })
	source, other := h.a.sessions[0], h.a.sessions[1]
	for _, selection := range []string{"source", "native-menu", "other-terminal", "editor", "security"} {
		t.Run(selection, func(t *testing.T) {
			a := h.a
			a.editPath, a.editShown, a.editorTerminalGroup = "", false, nil
			a.page = 1
			a.ensureTerminalGroups()
			a.activateTerminal(source, false)
			a.selected = "unrelated-sidebar-server"
			before := len(a.sessions)
			if selection == "native-menu" {
				var run func()
				for _, group := range a.menuGroups() {
					for _, action := range group.actions {
						if action.label == "新建终端标签" {
							run = action.run
						}
					}
				}
				if run == nil {
					t.Fatal("native new-terminal action missing")
				}
				run()
			} else {
				a.newTerminalTab(source)
			}
			// Dispatch queues all completion work until h.wait pumps it, so
			// this selection is guaranteed to precede the late SSH result.
			switch selection {
			case "other-terminal":
				a.activateTerminal(other, false)
			case "editor":
				a.edit = &editor.State{Buffer: editor.NewBuffer("preserved editor draft")}
				a.editPath, a.editorTerminalGroup = "/tmp/late-terminal-focus.txt", source.group
				a.activateGroupEditor(source.group)
			case "security":
				a.page = 4
			}
			a.contentFocus = false
			h.wait(func() bool { return len(a.sessions) == before+1 })
			added := a.sessions[before]
			if added.term == source.term || added.requestID == source.requestID || added.host != source.host || a.selected != "unrelated-sidebar-server" {
				t.Fatal("new tab did not create a separate authorized PTY for its source")
			}
			switch selection {
			case "source", "native-menu":
				if a.currentTerminal() != added || a.editShown {
					t.Fatal("new tab did not activate when the source remained selected")
				}
			case "other-terminal":
				if a.currentTerminal() != other || a.editShown {
					t.Fatal("late tab completion stole another terminal's selection")
				}
			case "editor":
				if !a.editShown || a.currentTerminal() != source || a.activeTerminalGroup != a.editorTerminalGroup || a.edit.Buffer.String() != "preserved editor draft" {
					t.Fatal("late tab completion displaced the selected editor")
				}
			case "security":
				if a.page != 4 || a.currentTerminal() != source {
					t.Fatal("late tab completion changed a later function selection")
				}
			}
		})
	}
}

func TestNewContainerTerminalCompletionPreservesSelectedEditor(t *testing.T) {
	a, fixture, updates := dockerUIFixture(t)
	source := a.sessions[0]
	a.openContainerTerminal(source, fixture.Target.Target, "web", "sh", nil)
	// The completion closure is queued through dispatch. Select an editor
	// before pumping it, without depending on server or scheduler timing.
	a.edit = &editor.State{Buffer: editor.NewBuffer("container editor draft")}
	a.editPath, a.editorTerminalGroup = "/tmp/container-editor-focus.txt", source.group
	a.activateGroupEditor(source.group)
	a.contentFocus = false
	before := len(a.sessions)
	pumpDocker(t, updates, func() bool { return len(a.sessions) == before+1 })
	if !a.editShown || a.currentTerminal() != source || a.activeTerminalGroup != a.editorTerminalGroup || a.contentFocus || a.edit.Buffer.String() != "container editor draft" {
		t.Fatal("late container completion displaced or focused over the editor")
	}
	if added := a.sessions[before]; added.target.Kind != "docker-terminal" || added.target.Docker.Target.ID != fixture.Target.Target.ID {
		t.Fatal("background container terminal did not attach to its requested target")
	}
}
