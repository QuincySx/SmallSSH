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
	bar, height := windowChromeMetrics(c)
	height = max(height, 48)
	windowWidth, _ := c.Size()
	sidebarWidth := a.sidebarWidth
	if sidebarWidth <= 0 {
		sidebarWidth = 190
	}
	maximum := min(float32(400), max(float32(140), windowWidth-308))
	sidebarWidth = max(140, min(maximum, sidebarWidth))
	if a.sidebarHidden {
		sidebarWidth = components.RailWidth
	}
	// Match the workspace's outer and split gaps, accounting for the tab
	// strip's own inset. On a narrow hidden-title-bar window the native
	// controls and sidebar toggle take precedence over alignment.
	tabStart := 8 + sidebarWidth + 8 - components.HeaderInset
	spacerWidth := max(0, tabStart-bar.Left-8-30)
	// Native hit testing lets interactive descendants keep clicks and tab
	// dragging; the header catches all remaining space, including above tabs.
	components.HeaderBar(c).Height(height).Padding(0, bar.Right, 0, bar.Left).DragWindow().Key("workspace-header").Label(tr(c, "全局导航栏")).Children(func() {
		ui.Spacer(c).Width(8).Height(height).Grow(0).Shrink(0).DragWindow()
		c.Key("workspace-sidebar-control")
		headerControl(c, height, 32, func() { a.sidebarToggle(c) })
		ui.Spacer(c).Width(spacerWidth).Height(height).Grow(0).Shrink(0).DragWindow()
		c.Key("workspace-terminal-tabs")
		ui.Row(c).Height(32).Grow(1).MinWidth(0).AlignItems(ui.Center).Children(func() {
			a.workspaceTerminalTabs(c)
			c.Key("workspace-new-terminal")
			if components.SVGButton(c, components.PlusIcon, tr(c, "新建终端标签")).Disabled(a.selected == "").Clicked() {
				a.runMenu(func() { a.openShellMode(true) })
			}
			ui.Spacer(c).Width(32).Height(32).Grow(0).Shrink(0).DragWindow().Label(tr(c, "窗口拖动区域"))
		})
		c.Key("workspace-transfer-control")
		headerControl(c, height, 32, func() { a.transferActivity(c) })
		c.Key("workspace-tools-control")
		headerControl(c, height, 32, func() { a.toolsToggle(c) })
		c.Key("workspace-settings")
		headerControl(c, height, 32, func() {
			if components.SVGButton(c, components.SettingsIcon, tr(c, "设置")).Clicked() {
				a.runMenu(func() { a.openSettingsWindow(settingsWorkspace) })
			}
		})
		ui.Spacer(c).Width(8).Height(height).Grow(0).Shrink(0).DragWindow()
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
		ui.Spacer(c).Grow(1).MinWidth(0).Height(32).DragWindow()
		a.terminalLayoutMenu(c)
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
