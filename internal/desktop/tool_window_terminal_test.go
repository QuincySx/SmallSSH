//go:build terminal_integration

package desktop

import (
	"strings"
	"testing"

	"github.com/egoist/mygo/ui"
)

func TestToolsWindowPreservesLiveTerminalAndDoesNotRouteItsKeysToSSH(t *testing.T) {
	a, recorders := terminalSplitFixture(t, 1)
	windows := fakeToolsFactory(a)
	main := ui.NewTester(a.view, 1000, 760)
	sess, term := a.sessions[0], a.sessions[0].term
	if err := main.Click("Shell a"); err != nil {
		t.Fatal(err)
	}
	main.Type("before")
	expectTerminalInput(t, recorders[0], "before")
	detachFromMenu(t, main)
	tools := ui.NewTester(a.detachedToolsView, 960, 720)
	if err := tools.Click("工具面板布局"); err != nil {
		t.Fatal(err)
	}
	tools.Key(0, ui.KeyRight)
	tools.Key(0, ui.KeyEscape)
	if err := tools.Click("目录路径"); err != nil {
		t.Fatal(err)
	}
	tools.Type("draft-path")
	if got := recorders[0].take(); got != "" {
		t.Fatal("tool window input was routed to the SSH PTY", got)
	}
	if a.sessions[0] != sess || sess.term != term || !strings.Contains(term.Text(), "pane-0 preserved output") {
		t.Fatal("detaching replaced the live PTY or its scrollback")
	}
	if err := tools.Click("工具面板布局"); err != nil {
		t.Fatal(err)
	}
	if err := tools.Click("放到底部"); err != nil {
		t.Fatal(err)
	}
	(*windows)[0].finishPendingUpdates()
	main.Frame()
	r, _ := main.Find("终端区域")
	main.ClickAt(r.X+r.W/2, r.Y+70)
	main.Type("中文🙂")
	main.Key(0, ui.KeyUp)
	main.Key(ui.Ctrl, ui.KeyC)
	expectTerminalInput(t, recorders[0], "中文🙂\x1b[A\x03")
	if sess.term != term || !strings.Contains(term.Text(), "pane-0 preserved output") {
		t.Fatal("redocking lost the original terminal")
	}
}
