package desktop

import (
	"fmt"
	"slices"

	"github.com/egoist/mygo/ui"
	"sshclient/internal/desktop/components"
	"sshclient/internal/vault"
)

func (a *app) terminalGroupDrop(c *ui.Context, panel ui.Element, g *terminalGroup, before *session, edges bool) {
	a.workspaceGroupDrop(c, panel, g, workspaceTab{session: before}, edges)
}

func (a *app) terminalGroupView(c *ui.Context, g *terminalGroup, first, last bool) {
	panel := ui.Column(c).Fill().MinWidth(0).MinHeight(0).Gap(0).Key(fmt.Sprintf("terminal-group:%d", g.id)).Label(fmt.Sprintf(tr(c, "终端组%d"), g.id))
	panel.Children(func() {
		previous := a.renderingTerminalGroup
		a.renderingTerminalGroup = g
		defer func() { a.renderingTerminalGroup = previous }()
		if len(a.terminalGroups()) > 1 {
			a.terminalGroupHeader(c, g, false, false, panel.Bounds().W, panel.Bounds())
		}
		if g.selectedTool != nil {
			a.renderWorkspaceTool(c, g.selectedTool)
		} else if g.editorSelected && a.editorTerminalGroup == g && a.editPath != "" {
			a.editorStage(c)
		} else if g.active != nil && a.online && a.hasSession(g.active) {
			a.sessionTerminal(c, g.active)
		} else {
			a.emptyTerminalGroup(c)
		}
		a.workspaceGroupDrop(c, panel, g, workspaceTab{}, true)
	})
	if g.selectedTool != nil && panel.FocusWithin() && !a.applyingTerminalFocus {
		a.activateWorkspaceTab(g, workspaceTab{tool: g.selectedTool})
	}
}

func (a *app) terminalGroupHeader(c *ui.Context, g *terminalGroup, first, last bool, width float32, groupBounds ...ui.Rect) {
	entries := a.workspaceTabs(g)
	selected := a.selectedWorkspaceTab(g)
	active := max(0, slices.Index(entries, selected))
	displayedTab, displayedEditor, displayedTool := g.active, g.editorSelected, g.selectedTool
	reveal := g.revealedTab != displayedTab || g.revealedEditor != displayedEditor || g.revealedTool != displayedTool
	header := components.HeaderBar(c).Height(32).Label(fmt.Sprintf(tr(c, "终端栏标题%d"), g.id))
	if width <= 0 {
		width = header.Bounds().W
	}
	if width != g.headerWidth {
		g.headerWidth = width
		reveal = true
		c.AnimationFrame()
	}
	controls := float32(0)
	if last {
		controls = 30
	}
	tabWidth := float32(210)
	if width > 0 {
		tabWidth = min(tabWidth, max(20, width-12-controls-36))
	}
	header.Children(func() {
		if len(entries) > 0 {
			ui.ScrollHorizontal(c).Grow(1).MinWidth(0).Height(32).Children(func() {
				tabs := ui.TabsBase(c, &active, len(entries))
				tabs.List.Gap(3).AlignItems(ui.Center).Children(func() {
					for i, entry := range entries {
						label := a.workspaceTabTitle(c, entry)
						display := a.workspaceTabDisplayTitle(c, entry)
						row := ui.Row(c).Key(entry.key()).Gap(2).Padding(2).Radius(8).AlignItems(ui.Center).Border(1, c.Theme().Border.Alpha(0))
						if entry == selected {
							if a.activeTerminalGroup == g {
								row.Background(c.Theme().Accent.Alpha(.10)).TextColor(c.Theme().Accent)
							} else {
								row.Background(c.Theme().SurfaceHover.Alpha(.6)).TextColor(c.Theme().TextMuted)
							}
							if reveal {
								row.ScrollIntoView()
							}
						}
						row.Children(func() {
							tab := tabs.Tab(i).Height(28).Padding(4, 10).Radius(8).MinWidth(0).MaxWidth(tabWidth).Label(label).Tooltip(display)
							if tab.Pressed() {
								tab.Background(c.Theme().SurfacePressed)
							} else if tab.Hovered() {
								tab.Background(c.Theme().SurfaceHover)
							}
							tab.Children(func() { components.HeaderLabel(c, display).MaxWidth(max(0, tabWidth-20)) })
							tab.Drag(a.workspaceTabPayload(entry))
							a.workspaceGroupDrop(c, tab, g, entry, false, groupBounds...)
							tab.ContextMenu(func(m *ui.Menu) { a.workspaceTabMenu(m, entry) })
							if tab.Clicked() {
								a.activateWorkspaceTab(g, entry)
							}
							closeLabel := tr(c, "关闭标签：") + label
							if entry.tool != nil && entry.tool.kind == toolFiles {
								closeLabel = tr(c, "关闭文件标签")
							}
							if components.IconButton(c, "×", closeLabel).Tooltip(tr(c, "关闭 ") + label).Clicked() {
								a.closeWorkspaceTab(entry)
							}
						})
					}
				})
				if tabs.List.Changed() && active >= 0 && active < len(entries) {
					a.activateWorkspaceTab(g, entries[active])
				}
			})
		} else {
			components.HeaderLabel(c, tr(c, "终端")).TextColor(c.Theme().TextMuted).Grow(1)
		}
		if last {
			a.terminalLayoutMenu(c)
		}
	})
	g.revealedTab, g.revealedEditor, g.revealedTool = displayedTab, displayedEditor, displayedTool
	if selected != a.selectedWorkspaceTab(g) {
		c.AnimationFrame()
	}
}

func (a *app) emptyTerminalGroup(c *ui.Context) {
	ui.Column(c).Fill().Center().Children(func() {
		ui.Column(c).FillWidth().MaxWidth(360).Padding(0, 12).Gap(10).AlignItems(ui.Center).Children(func() {
			if len(a.connections) == 0 {
				ui.Text(c, tr(c, "添加你的第一台服务器")).FontSize(18).Bold().TextAlign(ui.Center)
				ui.Text(c, tr(c, "保存服务器信息后，即可在工作台连接终端。")).FontSize(12).TextColor(c.Theme().TextMuted).TextAlign(ui.Center)
				if components.PrimaryButton(c, tr(c, "添加服务器")).Clicked() {
					a.navigate(a.newConnection)
				}
				return
			}
			index := slices.IndexFunc(a.connections, func(d vault.Summary) bool { return d.ID == a.selected && d.ID != "" })
			if index < 0 {
				ui.Text(c, tr(c, "选择服务器，开始连接")).FontSize(18).Bold().TextAlign(ui.Center)
				ui.Text(c, tr(c, "从服务器列表中选择一台，或搜索已有服务器。")).FontSize(12).TextColor(c.Theme().TextMuted).TextAlign(ui.Center)
				if components.PrimaryButton(c, tr(c, "选择服务器")).Clicked() {
					a.focusConnectionSearch(c)
				}
				return
			}
			server := a.connections[index]
			name := server.Name
			if name == "" {
				name = server.Host
			}
			if name == "" {
				name = tr(c, "服务器")
			}
			name = reviewText(name)
			ui.Text(c, name).FontSize(18).Bold().TextAlign(ui.Center).FillWidth().SingleLine().Tooltip(name)
			ui.Text(c, tr(c, "连接此服务器以打开终端。")).FontSize(12).TextColor(c.Theme().TextMuted).TextAlign(ui.Center)
			if components.PrimaryButton(c, tr(c, "连接终端")).Clicked() {
				a.openShell()
			}
		})
	})
}
