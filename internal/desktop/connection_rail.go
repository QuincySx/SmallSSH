package desktop

import (
	"slices"
	"sort"
	"strings"

	"github.com/egoist/mygo/ui"
	"sshclient/internal/desktop/components"
	"sshclient/internal/vault"
)

type connectionRailRow struct {
	group      string
	connection vault.Summary
	header     bool
}

type connectionRailModel struct {
	source      []vault.Summary
	rows        []connectionRailRow
	search      string
	closed      map[string]bool
	searchInput ui.Handle
}

// Keep independent snapshots so in-place edits invalidate the flattened rows,
// while equal refreshes, sidebar mode changes and ordinary renders reuse them.
func (m *connectionRailModel) rowsFor(connections []vault.Summary, search string, groups map[string]*bool) []connectionRailRow {
	sameSource := slices.EqualFunc(m.source, connections, func(a, b vault.Summary) bool {
		return a.ID == b.ID && a.Name == b.Name && a.Group == b.Group && a.Host == b.Host && a.Port == b.Port &&
			a.User == b.User && a.Auth == b.Auth && a.Fingerprint == b.Fingerprint && a.AgentFingerprint == b.AgentFingerprint &&
			a.Policy.Disabled == b.Policy.Disabled && a.Policy.Unrestricted == b.Policy.Unrestricted && slices.Equal(a.Policy.ProtectedPaths, b.Policy.ProtectedPaths)
	})
	if sameSource && m.search == search && m.sameClosedGroups(groups) {
		return m.rows
	}
	if !sameSource {
		m.source = slices.Clone(connections)
		for i := range m.source {
			m.source[i].Policy = m.source[i].Policy.Clone()
		}
	}
	m.search = search
	if m.closed == nil {
		m.closed = make(map[string]bool)
	} else {
		clear(m.closed)
	}
	for group, open := range groups {
		if open != nil && !*open {
			m.closed[group] = true
		}
	}
	m.rows = connectionRailRows(m.source, search, m.closed)
	return m.rows
}

func (m *connectionRailModel) sameClosedGroups(groups map[string]*bool) bool {
	count := 0
	for group, open := range groups {
		if open != nil && !*open {
			if !m.closed[group] {
				return false
			}
			count++
		}
	}
	return count == len(m.closed)
}

// Both sidebar presentations share the same search, group order and expansion
// state, so changing width never inserts or removes rows above an avatar.
func connectionRailRows(connections []vault.Summary, search string, closed map[string]bool) []connectionRailRow {
	grouped := make(map[string][]vault.Summary)
	search = strings.ToLower(search)
	for _, d := range connections {
		if search != "" && !strings.Contains(strings.ToLower(d.Name+" "+d.Host+" "+d.Group+" "+d.ID), search) {
			continue
		}
		group := d.Group
		if group == "" {
			group = "未分组"
		}
		grouped[group] = append(grouped[group], d)
	}
	groups := make([]string, 0, len(grouped))
	for group := range grouped {
		groups = append(groups, group)
	}
	sort.Strings(groups)
	rows := make([]connectionRailRow, 0, len(connections)+len(groups))
	for _, group := range groups {
		rows = append(rows, connectionRailRow{group: group, header: true})
		if closed[group] {
			continue
		}
		for _, d := range grouped[group] {
			rows = append(rows, connectionRailRow{group: group, connection: d})
		}
	}
	return rows
}

func connectionRailTooltip(d vault.Summary) string {
	return d.Name + "\n" + d.User + "@" + d.Host
}

func (a *app) connectionRail(c *ui.Context) { a.connectionSidebar(c, true) }

func (a *app) connectionSidebar(c *ui.Context, compact bool) {
	// The same keyed list owns the scroller at either width. The action,
	// search and group rows also keep their height when labels disappear.
	label := tr(c, "连接侧栏")
	if compact {
		label = tr(c, "连接窄栏")
	}
	c.Key("connection-sidebar")
	ui.Column(c).Fill().MinWidth(0).MinHeight(0).Gap(0).
		Background(c.Theme().Surface).Label(label).
		Disabled(a.page == 0 && a.connection.phase == "save").Children(func() {
		c.Key("connection-sidebar-actions")
		ui.Column(c).FillWidth().Height(36).Shrink(0).Padding(0, 6).Label(tr(c, "连接栏标题")).Children(func() {
			if components.NavigationIcon(c, "new-connection", tr(c, "新建连接"), components.PlusIcon, false, compact).Height(components.HeaderHeight).Clicked() {
				a.navigate(a.newConnection)
			}
		})
		c.Key("connection-sidebar-search-row")
		ui.Column(c).FillWidth().Height(36).Shrink(0).Padding(0, 6).Justify(ui.Center).Label(tr(c, "连接搜索栏")).Children(func() {
			if compact {
				if components.NavigationIcon(c, "connection-sidebar-search", tr(c, "搜索连接"), components.SearchIcon, false, true).Height(components.FormHeight).Clicked() {
					a.sidebarHidden = false
					a.connectionRailCache.searchInput.Focus(c)
					c.Invalidate()
				}
			} else {
				c.Key("connection-sidebar-search-input")
				ui.TextInput(c, &a.search).FillWidth().Height(28).Shrink(0).PaddingY(2).MinWidth(0).
					Placeholder(tr(c, "搜索名称 / 地址 / 分组")).Label(tr(c, "搜索连接")).Bind(&a.connectionRailCache.searchInput)
			}
		})
		rows := a.connectionRailCache.rowsFor(a.connections, a.search, a.groups)
		a.connectionRailList.Key = func(i int) any {
			if rows[i].header {
				return "group:" + rows[i].group
			}
			return "connection:" + rows[i].connection.ID
		}
		a.connectionRailList.Label = func(i int) string {
			if rows[i].header {
				return a.connectionGroupLabel(c, rows[i].group)
			}
			return rows[i].connection.Name
		}
		previous := a.selected
		c.Key("connection-sidebar-list")
		list := components.List(c, &a.connectionRailList, len(rows), func(i int) {
			r := rows[i]
			if r.header {
				groupLabel := a.connectionGroupLabel(c, r.group)
				if a.groups == nil {
					a.groups = make(map[string]*bool)
				}
				open := a.groups[r.group]
				if open == nil {
					value := true
					open = &value
					a.groups[r.group] = open
				}
				c.Key("connection-group:" + r.group)
				header := ui.ButtonBase(c).FillWidth().MinWidth(0).Height(20).Shrink(0).
					Padding(0, 8).Gap(4).AlignItems(ui.Center).Label(tr(c, "连接分组：") + groupLabel).Tooltip(groupLabel)
				header.Children(func() {
					if compact {
						ui.Box(c).FillWidth().Height(1).Shrink(0).Background(c.Theme().Border)
					} else {
						arrow := components.ChevronRightIcon
						if *open {
							arrow = components.ChevronDownIcon
						}
						ui.Icon(c, arrow).Size(12, 12).Shrink(0).TextColor(c.Theme().TextMuted)
						ui.Text(c, groupLabel).SingleLine().FontSize(11).TextColor(c.Theme().TextMuted).Grow(1).MinWidth(0)
					}
				})
				if header.Clicked() {
					*open = !*open
					a.connectionRailList.Focus(c)
					c.Invalidate()
				}
				return
			}
			d := r.connection
			item := components.NavigationItem(c, "connection:"+d.ID, d.Name, a.selected == d.ID, compact, func() {
				components.ConnectionAvatar(c, d.ID)
			}).Tooltip(connectionRailTooltip(d))
			if item.DoubleClicked() {
				a.selected = previous
				a.navigate(func() { a.switchHost(d.ID); a.openShellMode(true) })
			} else if item.Clicked() {
				a.selected = d.ID
				a.connectionRailList.Focus(c)
			}
			item.ContextMenu(func(m *ui.Menu) { a.connectionContextMenu(m, d, previous) })
		}).FillWidth().Grow(1).MinHeight(0).Padding(0, 6, 6, 6).Gap(4).
			Focusable().FocusGroup(ui.Vertical).Label(tr(c, "连接头像列表"))
		// Scope navigation to the virtual list, not only the avatars currently
		// built inside its viewport. Group headings never become selections.
		down := list.Shortcut(0, ui.KeyDown)
		up := list.Shortcut(0, ui.KeyUp)
		home := list.Shortcut(0, ui.KeyHome)
		end := list.Shortcut(0, ui.KeyEnd)
		// Enter keeps selection; connecting needs a double click or menu action.
		list.Shortcut(0, ui.KeyEnter)
		if down || up || home || end {
			at := slices.IndexFunc(rows, func(r connectionRailRow) bool { return !r.header && r.connection.ID == a.selected })
			to, step := at+1, 1
			switch {
			case home || at < 0 && up:
				to = 0
			case end:
				to, step = len(rows)-1, -1
			case up:
				to, step = at-1, -1
			}
			for to >= 0 && to < len(rows) && rows[to].header {
				to += step
			}
			if to >= 0 && to < len(rows) {
				a.selected = rows[to].connection.ID
				a.connectionRailList.ScrollIntoView(to)
				list.Focus()
				c.Invalidate()
			}
		}
	})
}

// Expanded rows and compact avatars retain the same explicit connection actions.
func (a *app) connectionContextMenu(m *ui.Menu, d vault.Summary, previous string) {
	if cancel := a.connecting[d.ID]; cancel != nil {
		if m.Item(tr(nil, "取消连接")).Chosen() {
			cancel()
		}
	}
	if m.Item(tr(nil, "新建终端标签")).Chosen() {
		a.selected = previous
		a.navigate(func() { a.switchHost(d.ID); a.openShellMode(true) })
	}
	if m.Item(tr(nil, "连接终端")).Chosen() {
		a.selected = previous
		a.navigate(func() { a.switchHost(d.ID); a.openShell() })
	}
	if m.Item(tr(nil, "编辑连接")).Chosen() {
		a.selected = previous
		a.navigate(func() { a.switchHost(d.ID); a.editConnection() })
	}
	if m.Item(tr(nil, "AI 访问…")).Chosen() {
		a.selected = previous
		a.navigate(func() {
			a.switchHost(d.ID)
			a.editConnection()
			a.connection.focusPolicy = true
		})
	}
	if m.Item(tr(nil, "备注…")).Chosen() {
		a.navigate(func() { a.openServerNotes(d.ID) })
	}
	if m.Item(tr(nil, "删除连接")).Chosen() {
		a.navigate(func() { a.deleteConnection(d) })
	}
}

func (a *app) connectionGroupLabel(c *ui.Context, group string) string {
	if group != "未分组" {
		return group
	}
	for _, connection := range a.connections {
		if connection.Group == group {
			return group
		}
	}
	return tr(c, "未分组")
}
