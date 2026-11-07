package desktop

import (
	"context"
	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"
	"testing"
)

func TestSettingsWindowLifecyclePreservesWorkspaceAndCancelsSecurity(t *testing.T) {
	a := fileFollowDemo()
	a.ctx = context.Background()
	sessions, layout, selected := a.sessions, a.fileLayout, a.selected
	var windows []*fakeToolsWindow
	a.newSettingsWindow = func(o mygo.WindowOptions, closed func()) toolsWindow {
		w := &fakeToolsWindow{options: o, onClosed: closed}
		windows = append(windows, w)
		return w
	}
	a.openSettingsWindow(settingsSecurity)
	if len(windows) != 1 || !windows[0].options.Hidden || windows[0].shown != 1 || a.page != 1 || !a.settingsOpen {
		t.Fatal("settings did not open independently")
	}
	a.ownerPass = "must clear"
	canceled := 0
	a.authCancel = func() { canceled++ }
	a.openSettingsWindow(settingsWorkspace)
	if len(windows) != 1 || a.ownerPass != "" || canceled != 1 {
		t.Fatal("switching settings lost window or authentication cleanup")
	}
	a.openSettingsWindow(settingsApprovals)
	if !a.approvalCenterVisible() || a.sessions[0] != sessions[0] || a.fileLayout != layout || a.selected != selected {
		t.Fatal("settings changed working target")
	}
	old := windows[0]
	a.closeSettingsWindow()
	a.openSettingsWindow(settingsWorkspace)
	old.finishPendingUpdates()
	if len(windows) != 2 || a.settingsWin != windows[1] || !a.settingsOpen {
		t.Fatal("late settings close destroyed replacement")
	}
	a.lock()
	if a.settingsWin != nil || a.settingsOpen {
		t.Fatal("locked settings retained unlocked content")
	}
}

func TestSettingsDeferredCreationAndPromptOwnership(t *testing.T) {
	var queued []func()
	a := &app{online: true, ctx: context.Background(), dispatch: func(fn func()) { queued = append(queued, fn) }}
	made := 0
	a.newSettingsWindow = func(o mygo.WindowOptions, closed func()) toolsWindow {
		made++
		return &fakeToolsWindow{options: o, onClosed: closed}
	}
	a.openSettingsWindow(settingsWorkspace)
	if made != 0 || len(queued) != 1 {
		t.Fatal("native creation happened during rendering")
	}
	a.closeSettingsWindow()
	queued[0]()
	if made != 0 {
		t.Fatal("late creation opened closed settings")
	}
	a.renderingSettingsWindow = true
	a.ask("confirm", "target", "message", "", "", "accept", func(string) {})
	a.renderingSettingsWindow = false
	a.settingsOpen = true
	main := ui.NewTester(func(c *ui.Context) { a.promptForHost(c, dialogHostMain) }, 700, 500)
	settings := ui.NewTester(func(c *ui.Context) { a.promptForHost(c, dialogHostSettings) }, 700, 500)
	if main.HasText("message") || !settings.HasText("message") {
		t.Fatal("settings prompt shown in wrong window")
	}
}

func TestQueuedEditorCloseNeverClosesSSHAfterFocusChanges(t *testing.T) {
	a := fileFollowDemo()
	a.installEditor(localEndpoint, "/synthetic/document.conf", "unchanged", "")
	w := &fakeToolsWindow{focused: true, onClosed: func() {}}
	a.editorWin = w
	origin := a.captureWindowMenuOrigin()
	w.focused = false
	sessions := append([]*session(nil), a.sessions...)
	action := menuAction{shortcut: "Cmd+W", run: func() { t.Fatal("queued editor close fell through to main action") }}
	a.runHostedWindowMenuAction(action, origin)
	if a.editPath != "" || len(a.sessions) != len(sessions) || a.sessions[0] != sessions[0] {
		t.Fatal("editor close changed SSH target")
	}
	a.installEditor(localEndpoint, "/synthetic/replacement.conf", "new", "")
	a.runHostedWindowMenuAction(action, origin)
	if a.editPath != "/synthetic/replacement.conf" {
		t.Fatal("stale close touched a replacement document")
	}
}

func TestFileWorkspaceTabKeepsPaneStateAndResumesDock(t *testing.T) {
	a := fileFollowDemo()
	a.ensureFileLayout()
	layout, pane := a.fileLayout, a.primaryFilePane
	browser := pane.browser()
	a.openFileTab()
	if !a.filesTabSelected || !a.filesTabOpen || a.fileLayout != layout || pane.browser() != browser {
		t.Fatal("file tab replaced file state")
	}
	a.showFileDock()
	if a.filesTabOpen || a.filesTabSelected || a.dock != toolFiles || a.fileLayout != layout || pane.browser() != browser {
		t.Fatal("redock did not preserve files")
	}
}
