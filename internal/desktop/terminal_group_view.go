package desktop

import (
	"fmt"
	"slices"
	"strings"

	"github.com/egoist/mygo/ui"
	"sshclient/internal/desktop/components"
)

func (a *app) terminalGroupDrop(c *ui.Context, panel ui.Element, g *terminalGroup, before *session, edge bool) {
	x, _, _ := panel.PointerPosition()
	createRight := edge && a.terminalLayout == nil && x > panel.Bounds().W*.65
	if d, ok := ui.DragOver[terminalTabDrag](panel); ok && a.validTerminalDrag(d) {
		if createRight {
			ui.Box(c).Absolute().Right(0).Top(34).Bottom(0).Width(panel.Bounds().W / 2).
				Background(c.Theme().Accent.Alpha(.14)).Label(tr(c, "右侧分组放置区"))
		} else {
			panel.Border(1, c.Theme().Accent)
		}
	}
	if d, ok := ui.Drop[terminalTabDrag](panel); ok {
		a.dropTerminalTab(d, g, before, createRight)
	}
}

func (a *app) terminalGroupView(c *ui.Context, g *terminalGroup, first, last bool) {
	panel := ui.Column(c).Fill().MinWidth(0).MinHeight(0).Gap(0).Key(fmt.Sprintf("terminal-group:%d", g.id)).Label(fmt.Sprintf(tr(c, "终端组%d"), g.id))
	width := panel.Bounds().W
	if width != g.headerWidth {
		// Bounds describe the preceding layout. Rebuild once after the
		// splitter settles so labels cannot obscure their close buttons.
		g.headerWidth = width
		g.revealedTab, g.revealedEditor = nil, false
		c.AnimationFrame()
	}
	panel.Children(func() {
		a.terminalGroupHeader(c, g, first, last, width)
		previous := a.renderingTerminalGroup
		a.renderingTerminalGroup = g
		defer func() { a.renderingTerminalGroup = previous }()
		if g.editorSelected && a.editorTerminalGroup == g && a.editPath != "" {
			a.editorStage(c)
		} else if g.active != nil && a.online && a.hasSession(g.active) {
			a.sessionTerminal(c, g.active)
		} else {
			a.emptyTerminalGroup(c)
		}
		a.terminalGroupDrop(c, panel, g, nil, true)
	})
}

func (a *app) terminalGroupHeader(c *ui.Context, g *terminalGroup, first, last bool, width float32) {
	labels := make([]string, 0, len(g.sessions)+1)
	for _, s := range g.sessions {
		labels = append(labels, sessionDisplayName(c, s))
	}
	if a.editorTerminalGroup == g && a.editPath != "" {
		name := a.editPath
		if i := strings.LastIndexAny(name, "/\\"); i >= 0 {
			name = name[i+1:]
		}
		if a.edit.Buffer.Modified() {
			name += " ·"
		}
		labels = append(labels, tr(c, "编辑: ")+name)
	}
	displayedTab, displayedEditor := g.active, g.editorSelected
	active := max(0, slices.Index(g.sessions, displayedTab))
	reveal := g.revealedTab != displayedTab || g.revealedEditor != displayedEditor
	if displayedEditor && a.editorTerminalGroup == g {
		active = len(g.sessions)
	}
	displayedIndex := active
	color := c.Theme().Border
	if a.terminalLayout != nil && a.activeTerminalGroup == g {
		color = c.Theme().Accent
	}
	header := components.HeaderBar(c).BorderColor(color).Label(fmt.Sprintf(tr(c, "终端栏标题%d"), g.id))
	controls := float32(0)
	if last {
		controls = 30
	}
	tabWidth := float32(210)
	if width > 0 {
		tabWidth = min(tabWidth, max(20, width-12-controls-36))
	}
	header.Children(func() {
		if len(labels) > 0 {
			ui.ScrollHorizontal(c).Grow(1).MinWidth(0).Height(34).Children(func() {
				tabs := ui.TabsBase(c, &active, len(labels))
				tabs.List.Gap(3).AlignItems(ui.Center).Children(func() {
					for i, label := range labels {
						drag := terminalTabDrag{owner: a, generation: a.generation, document: a.edit}
						key := "editor-tab"
						if i < len(g.sessions) {
							drag.session, drag.document = g.sessions[i], nil
							key = fmt.Sprintf("terminal-tab:%p", drag.session)
						}
						row := ui.Row(c).Key(key).Gap(2).Padding(2).Radius(6).AlignItems(ui.Center).Border(1, c.Theme().Border.Alpha(0))
						if i == displayedIndex {
							row.Background(c.Theme().Background).BorderColor(c.Theme().Border)
							if reveal {
								row.ScrollIntoView()
							}
						}
						row.Children(func() {
							tab := tabs.Tab(i).Height(28).Padding(4, 10).MinWidth(0).MaxWidth(tabWidth).Label(label).Tooltip(label).Children(func() { components.HeaderLabel(c, label).MaxWidth(max(0, tabWidth-20)) })
							tab.Drag(drag)
							a.terminalGroupDrop(c, tab, g, drag.session, false)
							if drag.session != nil {
								tab.ContextMenu(func(m *ui.Menu) { a.terminalTabMenu(m, drag.session) })
							}
							if tab.Clicked() {
								if drag.session != nil {
									a.activateTerminal(drag.session, true)
								} else {
									a.activateGroupEditor(g)
								}
							}
							if components.IconButton(c, "×", tr(c, "关闭标签：")+label).Tooltip(tr(c, "关闭 ") + label).Clicked() {
								if drag.session != nil {
									a.closeSessionAt(a.sessionIndex(drag.session))
								} else {
									a.closeEditor()
								}
							}
						})
					}
				})
				if tabs.List.Changed() {
					if active < len(g.sessions) {
						a.activateTerminal(g.sessions[active], true)
					} else {
						a.activateGroupEditor(g)
					}
				}
			})
		} else {
			components.HeaderLabel(c, tr(c, "终端")).TextColor(c.Theme().TextMuted).Grow(1)
		}
		if last {
			a.terminalLayoutMenu(c)
		}
	})
	// Input may select another tab during this build. Only mark the tab
	// whose row was actually built, so the next pass can reveal the new one.
	g.revealedTab, g.revealedEditor = displayedTab, displayedEditor
	if displayedTab != g.active || displayedEditor != g.editorSelected {
		c.AnimationFrame()
	}
}

func (a *app) emptyTerminalGroup(c *ui.Context) {
	ui.Column(c).Fill().Center().Gap(16).Children(func() {
		if len(a.connections) == 0 {
			ui.Text(c, tr(c, "添加你的第一台服务器")).FontSize(20).Bold()
			if components.PrimaryButton(c, tr(c, "添加服务器")).Height(38).Clicked() {
				a.newConnection()
			}
		} else {
			ui.Text(c, tr(c, "选择服务器，开始连接")).FontSize(18).TextColor(c.Theme().TextMuted)
			if a.selected != "" && components.PrimaryButton(c, tr(c, "连接终端")).Height(36).Clicked() {
				a.openShell()
			}
		}
	})
}
