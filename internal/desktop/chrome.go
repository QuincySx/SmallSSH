package desktop

import (
	"fmt"
	"github.com/egoist/mygo"
	"runtime"
	"slices"
)

type menuAction struct {
	label, shortcut string
	run             func()
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
	closeKey := "Cmd+W"
	if runtime.GOOS != "darwin" {
		closeKey = "Ctrl+Shift+W"
	}
	return []menuGroup{
		{"connection", "连接", []menuAction{
			{"新建连接", "CmdOrCtrl+Shift+N", a.newConnection},
			{"连接终端", "CmdOrCtrl+Shift+O", a.openShell},
			{"新建终端标签", "", func() {
				if source := a.currentTerminal(); source != nil {
					a.newTerminalTab(source)
				} else {
					a.openShellMode(true)
				}
			}},
			{"编辑当前连接", "", a.editConnection},
			{"服务器备注", "", func() { a.openServerNotes(a.selected) }},
			{"关闭当前标签", closeKey, func() {
				if a.editShown {
					a.closeEditor()
				} else {
					a.closeSession()
				}
			}},
		}},
		{"view", "视图", []menuAction{
			{"恢复默认布局", "", func() {
				a.page = 1
				a.resetWorkspaceLayout()
			}},
			{"终端工作台", "", func() { a.page = 1; a.fileExpanded = false }},
			{"新建终端到另一组", "", func() { a.newSplitTerminal(a.currentTerminal()) }},
			{"合并全部终端分组", "", a.mergeTerminalViews},
			{"显示 / 收起工具面板", "CmdOrCtrl+Shift+J", func() { a.page = 1; a.setToolsHidden(!a.toolsHidden) }},
			{"文件", "CmdOrCtrl+Shift+1", func() { a.showTool(toolFiles) }},
			{"传输队列", "CmdOrCtrl+Shift+2", func() { a.showTool(toolTransfers) }},
			{"常用命令", "CmdOrCtrl+Shift+3", func() { a.showTool(toolCommands) }},
			{"服务器状态", "CmdOrCtrl+Shift+4", func() { a.showTool(toolDiagnostics) }},
			{"工具面板放到底部", "", func() { a.setToolsDock(false) }},
			{"工具面板放到右侧", "", func() { a.setToolsDock(true) }},
			{"工具面板独立窗口", "", a.detachTools},
			{"放大", zoomIn, func() { a.zoomBy(1) }},
			{"缩小", zoomOut, func() { a.zoomBy(-1) }},
			{"实际大小", zoomActual, a.zoomActual},
		}},
		{"security", "安全", []menuAction{
			{"授权中心", "", func() { a.page = 3 }},
			{"设置", "", func() { a.page = 4 }},
			{"立即锁定", "CmdOrCtrl+Shift+L", a.confirmLock},
		}},
	}
}

func (a *app) resetWorkspaceLayout() {
	a.sidebarHidden = false
	a.setToolsDock(false)
	a.mergeTerminalViews()
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
	p := a.form
	if a.details == nil {
		return p.Name != "" || p.Host != "" || p.User != "" || p.Group != "" || p.Password != "" || a.keyFile != "" || a.port != "22" || p.Policy.Disabled || p.Policy.Unrestricted || len(p.Policy.ProtectedPaths) > 0
	}
	d := a.details
	return p.Name != d.Name || p.Host != d.Host || p.User != d.User || p.Group != d.Group || p.Auth != d.Auth || p.Fingerprint != d.Fingerprint || a.port != fmt.Sprint(d.Port) || a.connection.replace || p.Policy.Disabled != d.Policy.Disabled || p.Policy.Unrestricted != d.Policy.Unrestricted || !slices.Equal(p.Policy.ProtectedPaths, d.Policy.ProtectedPaths)
}

func (a *app) installMenu() {
	items := []*mygo.MenuItem{{Role: mygo.RoleAppMenu}}
	for _, group := range a.menuGroups() {
		children := []*mygo.MenuItem{}
		for _, action := range group.actions {
			children = append(children, &mygo.MenuItem{Label: tr(nil, action.label), Accelerator: action.shortcut, Click: func(_ *mygo.MenuItem, _ *mygo.Window) {
				fromTools, sequence := a.toolsWindowFocused(), a.toolsWindowSequence
				a.postUI(func() {
					a.runWindowMenuAction(action, fromTools, sequence)
				})
			}})
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
