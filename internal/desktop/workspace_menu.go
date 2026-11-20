package desktop

import (
	"github.com/egoist/mygo/ui"
	"sshclient/internal/desktop/components"
)

func (a *app) workspaceNavigationPage() int {
	page := a.page
	if page == 0 {
		page = a.connection.returnPage
	}
	if page != 3 && page != 4 {
		return 1
	}
	return page
}

// The title bar owns the real terminal tabs, leaving the workbench with one navigation row.
func (a *app) workspaceHeader(c *ui.Context) {
	a.ensureTerminalGroups()
	bar, height := windowChromeMetrics(c)
	height = max(height, 48)
	sidebarWidth := components.SidebarWidth(c, a.sidebarWidth, a.sidebarHidden)
	// Keep the first tab inside the content edge. The single-group strip
	// already adds its own header inset; canvas strips start at their edge.
	// Native controls take precedence when the sidebar is collapsed.
	tabStart := components.WorkspaceInset + sidebarWidth + components.WorkspaceInset + components.WorkspaceInset
	if !a.workspaceCanvas && len(a.terminalGroups()) == 1 {
		tabStart -= components.PanelInset
	}
	spacerWidth := max(0, tabStart-bar.Left-components.WorkspaceInset-2*32)
	_, _, _, settingsKey, _ := workspaceKeys()
	// Native hit testing lets interactive descendants keep clicks and tab
	// dragging; the header catches all remaining space, including above tabs.
	components.HeaderBar(c).Height(height).Padding(0, bar.Right, 0, bar.Left).DragWindow().Key("workspace-header").Label(tr(c, "全局导航栏")).Children(func() {
		ui.Spacer(c).Width(components.WorkspaceInset).Height(height).Grow(0).Shrink(0).DragWindow()
		c.Key("workspace-sidebar-control")
		headerControl(c, height, 32, func() { a.sidebarToggle(c) })
		c.Key("workspace-new-connection")
		headerControl(c, height, 32, func() {
			if components.SVGButton(c, components.PlusIcon, tr(c, "新建连接"), shortcutTip(tr(c, "新建连接"), "Cmd/Ctrl+Shift+N")).Disabled(a.page == 0 && a.connection.phase == "save").Clicked() {
				a.navigate(a.newConnection)
			}
		})
		ui.Spacer(c).Width(spacerWidth).Height(height).Grow(0).Shrink(0).DragWindow()
		c.Key("workspace-terminal-tabs")
		ui.Row(c).Height(32).Grow(1).MinWidth(0).AlignItems(ui.Center).Children(func() {
			a.workspaceTerminalTabs(c)
			ui.Spacer(c).Width(32).Height(32).Grow(0).Shrink(0).DragWindow().Label(tr(c, "窗口拖动区域"))
		})
		c.Key("workspace-tunnel-control")
		headerControl(c, height, 32, func() { a.tunnelActivity(c) })
		c.Key("workspace-transfer-control")
		headerControl(c, height, 32, func() { a.transferActivity(c) })
		c.Key("workspace-tools-control")
		headerControl(c, height, 32, func() { a.toolsToggle(c) })
		c.Key("workspace-settings")
		headerControl(c, height, 32, func() {
			if components.SVGButton(c, components.SettingsIcon, tr(c, "设置"), shortcutTip(tr(c, "设置"), settingsKey)).Clicked() {
				a.runMenu(func() { a.openSettingsWindow(settingsWorkspace) })
			}
		})
		ui.Spacer(c).Width(components.WorkspaceInset).Height(height).Grow(0).Shrink(0).DragWindow()
	})
}

func (a *app) workspaceTerminalTabs(c *ui.Context) {
	a.ensureTerminalGroups()
	a.renderingTerminalGroups = true
	defer func() {
		a.renderingTerminalGroups = false
		if pending := a.pendingTerminalMutation; pending != nil {
			a.pendingTerminalMutation = nil
			pending()
			c.AnimationFrame()
		}
	}()
	groups := a.terminalGroups()
	if len(groups) == 1 && !a.workspaceCanvas {
		g := groups[0]
		ui.Column(c).Key(g.id).Grow(1).MinWidth(0).Height(32).Children(func() { a.terminalGroupHeader(c, g, true, true, 0) })
	} else {
		var entries []workspaceTab
		for _, group := range groups {
			entries = append(entries, a.workspaceTabs(group)...)
		}
		selected := a.selectedWorkspaceTab(a.activeTerminalGroup)
		header := ui.Row(c).Grow(1).MinWidth(0).Height(32).Label(tr(c, "顶部工作区选项卡"))
		header.Children(func() {
			width := header.Bounds().W
			reveal := selected != a.revealedWorkspaceTab || width != a.workspaceHeaderTabsWidth
			a.workspaceTabStrip(c, nil, entries, selected, workspaceTabWidth(width-34, len(entries)), 32, reveal)
			a.workspaceHeaderTabsWidth = width
		})
		a.revealedWorkspaceTab = selected
		if selected != a.selectedWorkspaceTab(a.activeTerminalGroup) {
			c.AnimationFrame()
		}
	}
}

func (a *app) navigatePrimaryPage(page int) {
	if page != 1 && page != 3 && page != 4 {
		return
	}
	a.runMenu(func() {
		switch page {
		case 3:
			a.openSettingsWindow(settingsApprovals)
		case 4:
			a.openSettingsWindow(settingsWorkspace)
		default:
			a.page = 1
			a.contentFocus = !a.toolsDetached && !a.fileExpanded && !a.prompt.open && a.terminalFocusWanted()
		}
	})
}
