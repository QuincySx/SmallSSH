package desktop

import (
	"context"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"
	"sshclient/internal/monitor"
	"sshclient/internal/sshconn"
	"sshclient/internal/transfer"
)

type fakeToolsWindow struct {
	shown, hidden, closed, invalidations int
	title                                string
	focused                              bool
	onClosed                             func()
	updates                              []func()
	options                              mygo.WindowOptions
}

func (w *fakeToolsWindow) Show()                 { w.shown++; w.focused = true }
func (w *fakeToolsWindow) Hide()                 { w.hidden++; w.focused = false }
func (w *fakeToolsWindow) Close()                { w.closed++; w.onClosed() }
func (w *fakeToolsWindow) Update(fn func())      { w.updates = append(w.updates, fn) }
func (w *fakeToolsWindow) Invalidate()           { w.invalidations++ }
func (w *fakeToolsWindow) SetTitle(title string) { w.title = title }
func (w *fakeToolsWindow) IsFocused() bool       { return w.focused }
func (w *fakeToolsWindow) finishPendingUpdates() {
	for len(w.updates) > 0 {
		fn := w.updates[0]
		w.updates = w.updates[1:]
		fn()
	}
}

func fakeToolsFactory(a *app) *[]*fakeToolsWindow {
	var windows []*fakeToolsWindow
	a.newToolsWindow = func(options mygo.WindowOptions, closed func()) toolsWindow {
		w := &fakeToolsWindow{options: options, onClosed: closed, title: options.Title}
		windows = append(windows, w)
		return w
	}
	return &windows
}

func detachFromMenu(t *testing.T, tt *ui.Tester) {
	t.Helper()
	if err := tt.Click("工具面板布局"); err != nil {
		t.Fatal(err)
	}
	if err := tt.Click("独立窗口"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
}

func TestToolsWindowMovesSharedFileStateAndRedocks(t *testing.T) {
	for _, right := range []bool{false, true} {
		t.Run(fmt.Sprint(right), func(t *testing.T) {
			a := fileFollowDemo()
			a.chooseFileEndpoint(a.primaryFilePane, "c")
			p := a.primaryFilePane
			browser := p.browser()
			browser.cache[browser.dir] = []sshconn.File{{Name: "alpha.txt", Size: 123}}
			browser.chosen.Add("alpha.txt")
			a.splitFilePane(p, true, false)
			layout, sessions := a.fileLayout, a.sessions
			windows := fakeToolsFactory(a)
			main := ui.NewTester(a.view, 1000, 760)
			before, _ := main.Find("终端区域")
			detachFromMenu(t, main)
			if !a.toolsDetached || a.toolsHidden || len(*windows) != 1 {
				t.Fatal("detach did not create exactly one tool window")
			}
			w := (*windows)[0]
			if !w.options.Hidden || w.options.Parent != nil || w.shown != 1 || w.options.MinWidth < 600 || w.options.Content == nil {
				t.Fatal("detached window is not an independent, explicitly revealed window", w.options)
			}
			after, _ := main.Find("终端区域")
			if _, ok := main.Find("工具面板"); ok || after.H <= before.H+100 {
				t.Fatal("main window retained a duplicate tool view", before, after)
			}
			tools := ui.NewTester(a.detachedToolsView, 960, 720)
			if !tools.HasText("alpha.txt") || a.fileLayout != layout || p.browser() != browser || !browser.chosen.Has("alpha.txt") || p.endpoint != "c" || p.follow || a.sessions[0] != sessions[0] {
				t.Fatal("detaching replaced file data, selection or SSH sessions")
			}
			if err := tools.Click("工具面板布局"); err != nil {
				t.Fatal(err)
			}
			if !tools.Focused("独立窗口") || tools.HasText("放大工具面板") {
				t.Fatal("floating mode was not selected or offered an inapplicable pane action")
			}
			label := "放到底部"
			if right {
				label = "放到右侧"
			}
			if err := tools.Click(label); err != nil {
				t.Fatal(err)
			}
			if a.toolsDetached || a.toolsWin != nil || w.hidden != 1 || w.closed != 0 {
				t.Fatal("redock destroyed the currently rendering surface synchronously")
			}
			w.finishPendingUpdates()
			main.Frame()
			if _, ok := main.Find("工具面板"); !ok || a.toolsHidden || a.toolsRight != right || a.fileLayout != layout || p.browser() != browser || !browser.chosen.Has("alpha.txt") {
				t.Fatal("redock lost the pane, its data or the chosen dock")
			}
		})
	}
}

func TestToolsWindowCloseReopenLockAndDelayedClose(t *testing.T) {
	a := fileFollowDemo()
	windows := fakeToolsFactory(a)
	a.detachTools()
	first := (*windows)[0]
	first.Close() // Native window X closes tools, not the SSH connections.
	if !a.toolsHidden || !a.toolsDetached || a.toolsWin != nil || len(a.sessions) != 2 {
		t.Fatal("closing the native tool window lost sessions or detach preference")
	}
	a.setToolsHidden(false)
	second := (*windows)[1]
	if second.shown != 1 || a.toolsHidden || !a.toolsDetached {
		t.Fatal("tool toggle did not reopen the independent window")
	}
	a.setToolsDock(false)
	a.detachTools()
	third := (*windows)[2]
	second.finishPendingUpdates()
	if a.toolsWin != third || a.toolsHidden || !a.toolsDetached {
		t.Fatal("late close callback hid a replacement window")
	}
	a.lock()
	third.finishPendingUpdates()
	if a.toolsWin != nil || a.toolsDetached || a.online || len(a.sessions) != 0 || third.hidden != 1 {
		t.Fatal("locking retained an independent view or SSH data")
	}
	view := ui.NewTester(a.detachedToolsView, 960, 720)
	if view.HasText("目录路径") || view.HasText("终端 A") {
		t.Fatal("locked tool renderer exposed workspace contents")
	}
}

func TestHiddenStartupNeverRestoresOrOpensToolsWindow(t *testing.T) {
	a := fileFollowDemo()
	a.startup = newStartup(true)
	windows := fakeToolsFactory(a)
	a.detachTools()
	if len(*windows) != 0 || a.toolsDetached {
		t.Fatal("background startup opened an auxiliary window")
	}
	a.dir = t.TempDir()
	if err := os.Chmod(a.dir, 0700); err != nil {
		t.Fatal(err)
	}
	a.startup = newStartup(false)
	a.detachTools()
	if err := a.saveLayout(); err != nil {
		t.Fatal(err)
	}
	b := fileFollowDemo()
	b.dir, b.startup = a.dir, newStartup(true)
	b.loadLayout()
	if b.toolsDetached || b.toolsWin != nil {
		t.Fatal("restart restored a visible native tool window")
	}
}

func TestDetachedToolsFollowActiveSessionWithoutRedrawLoop(t *testing.T) {
	a := fileFollowDemo()
	for i, sess := range a.sessions {
		sess.stats = &monitor.Stats{Time: time.Now(), CPU: float64(11 + i*11), MemoryTotal: 512 << 20}
	}
	a.dock = toolDiagnostics
	windows := fakeToolsFactory(a)
	main := ui.NewTester(a.view, 1000, 760)
	detachFromMenu(t, main)
	w := (*windows)[0]
	tools := ui.NewTester(a.detachedToolsView, 960, 720)
	if !tools.HasText("11.0%") {
		t.Fatal("detached state is not showing the active server")
	}
	initial := w.invalidations
	for range 4 {
		main.Frame()
		tools.Frame()
	}
	if w.invalidations != initial {
		t.Fatal("unchanged terminal frames continually repaint the tool window", initial, w.invalidations)
	}
	if err := main.Click("终端 B"); err != nil {
		t.Fatal(err)
	}
	if w.invalidations <= initial || w.title != "SSH 工具 · 终端 B" || w.shown != 1 {
		t.Fatal("active-session change did not update quietly", w)
	}
	tools.Frame()
	if !tools.HasText("22.0%") || a.primaryFilePane.endpoint != "b" {
		t.Fatal("tools or following files remained on the previous session")
	}
	a.dispatch = func(fn func()) { fn() }
	initial = w.invalidations
	a.updateServerStatus(a.sessions[1], a.generation, &monitor.Stats{Time: time.Now(), CPU: 37, MemoryTotal: 512 << 20}, "自动更新")
	tools.Frame()
	if w.invalidations <= initial || !tools.HasText("37.0%") || w.shown != 1 {
		t.Fatal("background sampling did not update, or raised the window")
	}
}

func TestDetachedDialogsAndTransfersHaveOneOwner(t *testing.T) {
	a := managementApp()
	a.sidebarWidth, a.terminalShare, a.dock, a.diagnosticTab = 190, 72, toolDiagnostics, 1
	windows := fakeToolsFactory(a)
	main := ui.NewTester(a.view, 1000, 760)
	detachFromMenu(t, main)
	tools := ui.NewTester(a.detachedToolsView, 960, 720)
	if err := tools.Click("worker"); err != nil {
		t.Fatal(err)
	}
	if err := tools.Click("列表操作"); err != nil {
		t.Fatal(err)
	}
	if err := tools.ChooseMenuItem("结束进程…"); err != nil {
		t.Fatal(err)
	}
	tools.Frame()
	main.Frame()
	confirmation := a.prompt.title + "对话框"
	if !a.prompt.open || !a.prompt.detached || !tools.HasText(confirmation) || main.HasText(confirmation) {
		t.Fatal("tool confirmation was duplicated or opened in the wrong window")
	}
	if err := tools.Click("取消"); err != nil {
		t.Fatal(err)
	}
	if err := tools.Click("命令"); err != nil {
		t.Fatal(err)
	}
	if err := tools.Click("新建常用命令"); err != nil {
		t.Fatal(err)
	}
	if err := tools.Click("命令名称"); err != nil {
		t.Fatal(err)
	}
	tools.Type("keep this draft")
	main.Frame()
	if !a.snippetDetached || main.HasText("保存常用命令对话框") || !tools.HasText("保存常用命令对话框") {
		t.Fatal("snippet dialog has the wrong owner")
	}
	(*windows)[0].Close()
	main.Frame()
	if !main.HasText("保存常用命令对话框") || a.snippetForm.Name != "keep this draft" {
		t.Fatal("closing the native window discarded an unsaved dialog")
	}
	if err := main.Click("取消"); err != nil {
		t.Fatal(err)
	}
	a.setToolsHidden(false)
	tools = ui.NewTester(a.detachedToolsView, 960, 720)
	a.jobs = []transfer.Job{{ID: "sample", Host: "fixture", Remote: "/srv/file", State: "done", Size: 100, Bytes: 100}}
	if _, found := tools.Find("传输任务"); found {
		t.Fatal("local tools duplicated the global task entry")
	}
	if err := main.Click("传输任务"); err != nil {
		t.Fatal(err)
	}
	tools.Frame()
	if !main.HasText("传输任务面板") || tools.HasText("传输任务面板") {
		t.Fatal("global transfer queue rendered in the wrong window")
	}
	if err := main.Click("传输任务"); err != nil {
		t.Fatal(err)
	}
	tools.Frame()
	if main.HasText("传输任务面板") || tools.HasText("传输任务面板") {
		t.Fatal("closing the global transfer queue left a duplicate view")
	}
}

func TestShutdownClosesToolsWindow(t *testing.T) {
	a := fileFollowDemo()
	a.dir = t.TempDir()
	a.ctx, a.cancel = context.WithCancel(context.Background())
	windows := fakeToolsFactory(a)
	a.detachTools()
	a.shutdown()
	w := (*windows)[0]
	w.finishPendingUpdates()
	if w.hidden != 1 || w.closed != 1 || a.toolsWin != nil || a.ctx.Err() == nil {
		t.Fatal("application shutdown left an auxiliary window alive")
	}
}

func TestCloseMenuInToolsWindowDoesNotCloseAnSSHTab(t *testing.T) {
	a := fileFollowDemo()
	windows := fakeToolsFactory(a)
	a.detachTools()
	a.newConnection()
	a.form.Name = "unfinished form"
	sequence := a.toolsWindowSequence
	for _, group := range a.menuGroups() {
		for _, action := range group.actions {
			if action.label == "关闭当前标签" {
				(*windows)[0].focused = false // Focus can change before queued work runs.
				a.runWindowMenuAction(action, true, sequence)
			}
		}
	}
	(*windows)[0].finishPendingUpdates()
	if len(a.sessions) != 2 || !a.toolsHidden || a.toolsWin != nil {
		t.Fatal("close shortcut in the tool window closed an SSH connection")
	}
	if a.page != 0 || a.form.Name != "unfinished form" || a.prompt.open {
		t.Fatal("closing the tool window interrupted an unrelated connection form")
	}
}

func TestToolsWindowCreationIsDeferredAndCancelledByLockOrRedock(t *testing.T) {
	for _, cancel := range []string{"lock", "redock", "none"} {
		t.Run(cancel, func(t *testing.T) {
			a := fileFollowDemo()
			windows := fakeToolsFactory(a)
			var queued []func()
			a.dispatch = func(fn func()) { queued = append(queued, fn) }
			a.detachTools()
			a.detachTools()
			if len(*windows) != 0 || len(queued) != 1 || !a.toolsWindowPending {
				t.Fatal("window creation ran inside the caller's UI frame or was queued twice")
			}
			switch cancel {
			case "lock":
				a.lock()
			case "redock":
				a.setToolsDock(false)
			}
			queued[0]()
			if cancel == "none" {
				if len(*windows) != 1 || a.toolsWindowPending || (*windows)[0].shown != 1 {
					t.Fatal("explicit detach was not revealed after its rendering frame")
				}
			} else if len(*windows) != 0 || a.toolsWindowPending {
				t.Fatal("a cancelled creation opened a delayed window")
			}
		})
	}
}
