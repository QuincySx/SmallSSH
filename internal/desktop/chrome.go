package desktop

import (
	"fmt"
	"github.com/egoist/mygo"
	"runtime"
	"slices"
	"sshclient/internal/connectcfg"
)

type menuAction struct {
	label, shortcut string
	run             func()
	// appMenu moves the item into the macOS application menu, where Settings
	// lives by convention; other systems keep it in its own group.
	appMenu bool
}

// workspaceKeys follows each system's terminal-app conventions. On Windows
// and Linux every chord keeps Shift or uses Tab, so bare Ctrl letters still
// reach the shell; Settings and the sidebar have no chord there.
func workspaceKeys() (newTab, nextTab, previousTab, settings, sidebar string) {
	return workspaceKeysFor(runtime.GOOS)
}

func workspaceKeysFor(goos string) (newTab, nextTab, previousTab, settings, sidebar string) {
	if goos == "darwin" {
		return "Cmd+T", "Cmd+Alt+Right", "Cmd+Alt+Left", "Cmd+,", "Cmd+Ctrl+S"
	}
	return "Ctrl+Shift+T", "Ctrl+Tab", "Ctrl+Shift+Tab", "", ""
}

// shortcutTip appends the platform chord to a tooltip, as the tools toggle
// already does, so the visible button teaches its keyboard equivalent.
func shortcutTip(label, shortcut string) string {
	if shortcut == "" {
		return label
	}
	return label + " · " + shortcut
}

type menuGroup struct {
	id      string
	label   string
	actions []menuAction
}

// Native menus and the workspace navigation share the same actions. Nothing here
// intercepts terminal arrows, Ctrl+C, Ctrl+R, Tab or ordinary text input.
func (a *app) menuGroups() []menuGroup {
	zoomIn, zoomOut, zoomActual := zoomKeys()
	newTabKey, nextTabKey, previousTabKey, settingsKey, sidebarKey := workspaceKeys()
	closeKey := "Cmd+W"
	if runtime.GOOS != "darwin" {
		closeKey = "Ctrl+Shift+W"
	}
	return []menuGroup{
		{"connection", "连接", []menuAction{
			{label: "新建连接", shortcut: "CmdOrCtrl+Shift+N", run: a.newConnection},
			{label: "连接终端", shortcut: "CmdOrCtrl+Shift+O", run: a.openShell},
			{label: "新建终端标签", shortcut: newTabKey, run: func() {
				if source := a.currentTerminal(); source != nil {
					a.newTerminalTab(source)
				} else {
					a.openShellMode(true)
				}
			}},
			{label: "编辑当前连接", run: a.editConnection},
			{label: "SSH 隧道", run: func() { a.openTunnels(a.selected) }},
			{label: "服务器备注", run: func() { a.openServerNotes(a.selected) }},
			{label: "关闭当前标签", shortcut: closeKey, run: func() {
				if a.editorWindowFocused() || a.editShown {
					a.closeEditor()
				} else if g := a.activeTerminalGroup; g != nil && g.selectedTool != nil {
					a.closeWorkspaceTab(workspaceTab{tool: g.selectedTool})
				} else {
					a.closeSession()
				}
			}},
		}},
		{"view", "视图", []menuAction{
			{label: "恢复默认布局", run: func() {
				a.page = 1
				a.resetWorkspaceLayout()
			}},
			{label: "终端工作台", run: func() { a.page = 1; a.fileExpanded = false }},
			{label: "显示 / 收起连接侧栏", shortcut: sidebarKey, run: a.toggleSidebar},
			{label: "上一个标签", shortcut: previousTabKey, run: func() { a.cycleWorkspaceTab(-1) }},
			{label: "下一个标签", shortcut: nextTabKey, run: func() { a.cycleWorkspaceTab(1) }},
			{label: "新建终端到另一组", run: func() { a.newSplitTerminal(a.currentTerminal()) }},
			{label: "合并全部终端分组", run: a.mergeTerminalViews},
			{label: "显示 / 收起工具面板", shortcut: "CmdOrCtrl+Shift+J", run: func() { a.page = 1; a.setToolsHidden(!a.toolsHidden) }},
			{label: "文件", shortcut: "CmdOrCtrl+Shift+1", run: func() { a.showTool(toolFiles) }},
			{label: "传输队列", shortcut: "CmdOrCtrl+Shift+2", run: func() { a.showTool(toolTransfers) }},
			{label: "常用命令", shortcut: "CmdOrCtrl+Shift+3", run: func() { a.showTool(toolCommands) }},
			{label: "服务器状态", shortcut: "CmdOrCtrl+Shift+4", run: func() { a.showTool(toolDiagnostics) }},
			{label: "工具面板放到底部", run: func() { a.setToolsDock(false) }},
			{label: "工具面板放到右侧", run: func() { a.setToolsDock(true) }},
			{label: "工具面板独立窗口", run: a.detachTools},
			{label: "放大", shortcut: zoomIn, run: func() { a.zoomBy(1) }},
			{label: "缩小", shortcut: zoomOut, run: func() { a.zoomBy(-1) }},
			{label: "实际大小", shortcut: zoomActual, run: a.zoomActual},
		}},
		{"security", "安全", []menuAction{
			{label: "授权中心", run: func() { a.openSettingsWindow(settingsApprovals) }},
			{label: "设置…", shortcut: settingsKey, run: func() { a.openSettingsWindow(settingsWorkspace) }, appMenu: true},
			{label: "立即锁定", shortcut: "CmdOrCtrl+Shift+L", run: a.confirmLock},
		}},
	}
}

// Reset restores the default of the chosen presentation, which is its own
// setting: tab aggregation stacks the terminal above the tool area, while the
// canvas re-tiles every card and keeps the tab tool area hidden, as a first
// launch does.
func (a *app) resetWorkspaceLayout() {
	a.sidebarHidden = false
	a.setToolsDock(false)
	if a.workspaceCanvas {
		a.setToolsHidden(true)
	}
	a.setWorkspaceCanvas(a.workspaceCanvas)
	if a.workspaceNavigationPage() != 1 {
		a.contentFocus = false
	}
}

func (a *app) runMenu(run func()) {
	a.navigate(run)
}

// Capture the originating window before queuing work. Closing a tool window
// must neither close an SSH tab nor discard an unrelated connection form.
func (a *app) runWindowMenuAction(action menuAction, fromTools bool, sequence uint64) {
	if (action.shortcut == "Cmd+W" || action.shortcut == "Ctrl+Shift+W") && a.serverNotes != nil && a.serverNotes.detached == fromTools {
		a.requestCloseNotes(a.serverNotes)
		return
	}
	if (action.shortcut == "Cmd+W" || action.shortcut == "Ctrl+Shift+W") && fromTools {
		if a.toolsDetached && a.toolsWindowSequence == sequence {
			a.setToolsHidden(true)
		}
		return
	}
	a.runMenu(action.run)
}

func (a *app) navigate(run func()) {
	if !a.online {
		return
	}
	if a.serverNotes != nil {
		a.requestCloseNotes(a.serverNotes)
		if a.serverNotes != nil {
			return
		}
	}
	if a.page == 0 && a.connection.phase == "save" {
		a.connection.message = tr(nil, "正在提交加密保存，请稍候")
		return
	}
	if a.page == 0 && a.connectionHasChanges() {
		a.ask(tr(nil, "放弃未保存的连接？"), a.form.Name, tr(nil, "当前填写的连接资料尚未保存。"), "", "", tr(nil, "放弃并继续"), func(string) {
			a.cancelConnectionEditor()
			run()
		})
		return
	}
	if a.page == 0 {
		a.cancelConnectionEditor()
	}
	a.ownerPass = ""
	run()
}

func (a *app) connectionHasChanges() bool {
	if a.connection.pendingNote != nil {
		return true
	}
	p := a.form
	if a.details == nil {
		return p.Name != "" || p.Host != "" || p.User != "" || p.Group != "" || p.Password != "" || a.keyFile != "" || a.port != "22" || p.Policy.Disabled || p.Policy.Unrestricted || len(p.Policy.ProtectedPaths) > 0 || a.formNote.Manual != "" || p.Terminal != (connectcfg.Terminal{}) || p.Network != (connectcfg.Network{}) || p.Hardware != (connectcfg.Hardware{})
	}
	d := a.details
	return p.Name != d.Name || p.Host != d.Host || p.User != d.User || p.Group != d.Group || p.Auth != d.Auth || p.Fingerprint != d.Fingerprint || a.port != fmt.Sprint(d.Port) || a.connection.replace || p.Policy.Disabled != d.Policy.Disabled || p.Policy.Unrestricted != d.Policy.Unrestricted || !slices.Equal(p.Policy.ProtectedPaths, d.Policy.ProtectedPaths) || a.formNote.Manual != a.formNoteOriginal.Manual || p.Terminal != d.Terminal || p.Network != d.Network || p.Hardware != d.Hardware || a.connection.replaceProxy
}

func (a *app) installMenu() {
	appMenu := &mygo.MenuItem{Role: mygo.RoleAppMenu}
	items := []*mygo.MenuItem{appMenu}
	var appItems []*mygo.MenuItem
	for _, group := range a.menuGroups() {
		children := []*mygo.MenuItem{}
		for _, action := range group.actions {
			item := &mygo.MenuItem{Label: tr(nil, action.label), Accelerator: action.shortcut, Click: func(_ *mygo.MenuItem, _ *mygo.Window) {
				origin := a.captureWindowMenuOrigin()
				a.postUI(func() {
					a.runHostedWindowMenuAction(action, origin)
				})
			}}
			if action.appMenu && runtime.GOOS == "darwin" {
				appItems = append(appItems, item)
				continue
			}
			children = append(children, item)
		}
		items = append(items, &mygo.MenuItem{Label: tr(nil, group.label), Submenu: children})
		if group.id == "connection" {
			copyKey, pasteKey, allKey := "Cmd+C", "Cmd+V", "Cmd+A"
			if runtime.GOOS != "darwin" {
				copyKey, pasteKey, allKey = "Ctrl+Shift+C", "Ctrl+Shift+V", "Ctrl+Shift+A"
			}
			items = append(items, &mygo.MenuItem{Label: tr(nil, "编辑"), Submenu: []*mygo.MenuItem{{Role: mygo.RoleCopy, Accelerator: copyKey}, {Role: mygo.RolePaste, Accelerator: pasteKey}, {Role: mygo.RoleSelectAll, Accelerator: allKey}}})
		}
	}
	items = append(items, &mygo.MenuItem{Role: mygo.RoleWindowMenu})
	if len(appItems) > 0 {
		// The stock macOS application menu, with Settings after About.
		appMenu.Submenu = append(append([]*mygo.MenuItem{{Role: mygo.RoleAbout}, mygo.Separator()}, appItems...),
			mygo.Separator(), &mygo.MenuItem{Role: mygo.RoleServices}, mygo.Separator(),
			&mygo.MenuItem{Role: mygo.RoleHide}, &mygo.MenuItem{Role: mygo.RoleHideOthers}, &mygo.MenuItem{Role: mygo.RoleUnhide},
			mygo.Separator(), &mygo.MenuItem{Role: mygo.RoleQuit})
	}
	a.menu = mygo.NewMenu(items)
	mygo.App.SetMenu(a.menu)
}

func (a *app) confirmLock() {
	if a.editPath != "" && a.edit.Buffer.Modified() {
		a.ask(tr(nil, "锁定前处理未保存修改"), a.editPath, tr(nil, "继续锁定会放弃修改并断开会话。需要保存时请取消。"), "", "", tr(nil, "放弃并锁定"), func(string) { a.lock() })
	} else {
		a.lock()
	}
}
