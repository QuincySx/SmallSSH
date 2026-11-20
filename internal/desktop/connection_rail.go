package desktop

import (
	"math"
	"slices"
	"strings"
	"time"

	"github.com/egoist/mygo/ui"
	"sshclient/internal/connectcfg"
	"sshclient/internal/desktop/components"
	"sshclient/internal/protocol"
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
	focusSearch bool
	loading     bool
	loadFailed  bool
	pending     []vault.Summary
}

const connectionRailItemHeight = 36
const connectionRailGroupHeight = 20
const connectionSidebarInset float32 = 6

func (a *app) focusConnectionSearch(c *ui.Context) {
	a.sidebarHidden = false
	a.connectionRailCache.focusSearch = true
	c.Invalidate()
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
	groups := []string{}
	search = strings.ToLower(search)
	for _, d := range connections {
		if search != "" && !strings.Contains(strings.ToLower(d.Name+" "+d.Host+" "+d.Group+" "+d.ID), search) {
			continue
		}
		group := d.Group
		if _, exists := grouped[group]; !exists {
			groups = append(groups, group)
		}
		grouped[group] = append(grouped[group], d)
	}
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

func (a *app) wakePoll() {
	select {
	case a.pollWake <- struct{}{}:
	default:
	}
}

func (a *app) applyConnectionSnapshot(epoch, orderVersion uint64, h protocol.Message, err error) {
	if !a.online || a.epoch.Load() != epoch || a.connectionOrderVersion.Load() != orderVersion || a.connectionRailCache.pending != nil {
		return
	}
	if err != nil {
		if a.connectionRailCache.loading {
			a.connectionRailCache.loadFailed = true
		}
		return
	}
	a.connectionRailCache.loading, a.connectionRailCache.loadFailed = false, false
	a.hosts, a.connections, a.routes = h.Hosts, h.Connections, h.Routes
	if a.page != 0 && a.selected == "" && len(a.hosts) > 0 {
		a.switchHost(hostID(a.hosts[0]))
	}
}

func (a *app) connectionRail(c *ui.Context) { a.connectionSidebar(c, true) }

func (a *app) connectionRailLoading(c *ui.Context, compact bool) {
	if a.connectionRailCache.loadFailed {
		label := tr(c, "无法加载连接，正在重试…")
		row := ui.Row(c).FillWidth().Height(36).Padding(0, connectionSidebarInset).AlignItems(ui.Center).Gap(6).Label(label).Tooltip(label)
		row.Children(func() {
			ui.Spinner(c).Size(12, 12).Shrink(0)
			if !compact {
				ui.Text(c, label).FontSize(11).TextColor(c.Theme().TextMuted).Wrap()
			}
		})
		return
	}
	theme := c.Theme()
	strength := float32(.46)
	if !c.Preferences().ReduceMotion {
		c.After(50 * time.Millisecond)
		phase := float64(c.Now().UnixMilli()%1400) / 1400
		strength += .16 * float32(math.Sin(phase*2*math.Pi))
	}
	color := theme.Surface.Mix(theme.Border, strength)
	ui.Column(c).FillWidth().Padding(0, connectionSidebarInset).Gap(2).Label(tr(c, "正在加载连接…")).Children(func() {
		ui.Row(c).FillWidth().Height(connectionRailGroupHeight).AlignItems(ui.Center).Padding(0, 8).Children(func() {
			ui.Box(c).Width(53).MaxWidthPercent(100).Height(7).Radius(4).Background(color)
		})
		for _, width := range []float32{94, 72, 84} {
			ui.Row(c).FillWidth().Height(connectionRailItemHeight).Padding(0, 4).AlignItems(ui.Center).Gap(8).Children(func() {
				ui.Box(c).Size(28, 28).Radius(7).Shrink(0).Background(color)
				if !compact {
					ui.Box(c).Width(width).MinWidth(0).Height(10).Radius(5).Background(color)
				}
			})
		}
	})
}

func (a *app) connectionSidebar(c *ui.Context, compact bool) {
	// The same keyed list owns the scroller at either width. Search and group
	// rows keep their height when labels disappear.
	label := tr(c, "连接侧栏")
	if compact {
		label = tr(c, "连接窄栏")
	}
	inset := connectionSidebarInset
	c.Key("connection-sidebar")
	ui.Column(c).Fill().MinWidth(0).MinHeight(0).Gap(0).
		Padding(inset, 0, 0, 0).Radius(components.PanelRadius).Clip().Background(c.Theme().Surface).Label(label).
		Disabled(a.page == 0 && a.connection.phase == "save").Children(func() {
		c.Key("connection-sidebar-search-row")
		ui.Column(c).FillWidth().Height(36).Shrink(0).Padding(0, inset).Justify(ui.Center).Label(tr(c, "连接搜索栏")).Children(func() {
			if compact {
				if components.NavigationIcon(c, "connection-sidebar-search", tr(c, "搜索连接"), components.SearchIcon, false, true).Height(components.FormHeight).Clicked() {
					a.focusConnectionSearch(c)
				}
			} else {
				c.Key("connection-sidebar-search-input")
				input := ui.TextInput(c, &a.search).FillWidth().Height(28).Shrink(0).PaddingY(2).MinWidth(0).
					Placeholder(tr(c, "搜索服务器")).Label(tr(c, "搜索连接")).Tooltip(tr(c, "搜索名称 / 地址 / 分组")).
					Bind(&a.connectionRailCache.searchInput)
				if a.connectionRailCache.focusSearch {
					input.Focus()
					a.connectionRailCache.focusSearch = false
				}
			}
		})
		if a.connectionRailCache.loading && len(a.connections) == 0 {
			a.connectionRailLoading(c, compact)
			return
		}
		connections := a.connections
		if a.connectionRailCache.pending != nil {
			connections = a.connectionRailCache.pending
		}
		rows := a.connectionRailCache.rowsFor(connections, a.search, a.groups)
		a.connectionRailList.DragRows = nil
		if a.connectionRailCache.pending == nil && a.page != 0 {
			a.connectionRailList.DragRows = func(indices []int) any {
				if len(indices) != 1 || indices[0] < 0 || indices[0] >= len(rows) {
					return nil
				}
				r := rows[indices[0]]
				return connectionDrag{Revision: vault.ConnectionOrderRevision(a.connections), ID: r.connection.ID, Group: r.group, Header: r.header}
			}
		}
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
				header := ui.ButtonBase(c).FillWidth().MinWidth(0).Height(connectionRailGroupHeight).Shrink(0).
					Padding(0, 8).Gap(4).AlignItems(ui.Center).Label(tr(c, "连接分组：") + groupLabel).Tooltip(groupLabel)
				header.Children(func() {
					if compact {
						arrow := components.ChevronRightIcon
						if *open {
							arrow = components.ChevronDownIcon
						}
						ui.Icon(c, arrow).Size(12, 12).TextColor(c.Theme().TextMuted)
					} else {
						arrow := components.ChevronRightIcon
						if *open {
							arrow = components.ChevronDownIcon
						}
						ui.Icon(c, arrow).Size(12, 12).Shrink(0).TextColor(c.Theme().TextMuted)
						ui.Text(c, groupLabel).SingleLine().FontSize(11).TextColor(c.Theme().TextMuted).Grow(1).MinWidth(0)
					}
				})
				a.connectionDropTarget(c, header, r)
				if header.Clicked() {
					*open = !*open
					a.connectionRailList.Focus(c)
					c.Invalidate()
				}
				return
			}
			d := r.connection
			item := components.NavigationItem(c, "connection:"+d.ID, d.Name, a.selected == d.ID, compact, func() {
				avatar := ui.Box(c).Size(28, 28).Shrink(0)
				avatar.Children(func() { components.ConnectionAvatar(c, d.ID) })
			}).Height(connectionRailItemHeight).Tooltip(connectionRailTooltip(d))
			a.connectionDropTarget(c, item, r)
			if item.DoubleClicked() {
				a.selected = previous
				a.navigate(func() { a.switchHost(d.ID); a.openShellMode(true) })
			} else if item.Clicked() {
				a.selected = d.ID
				a.connectionRailList.Focus(c)
			}
			item.ContextMenu(func(m *ui.Menu) { a.connectionContextMenu(m, d, previous) })
		}).FillWidth().Grow(1).MinHeight(0).Padding(0, inset, 6, inset).Gap(2).
			Focusable().FocusGroup(ui.Vertical).Label(tr(c, "连接头像列表"))
		// Scope navigation to the virtual list, not only the avatars currently
		// built inside its viewport. Group headings never become selections.
		down := list.Shortcut(0, ui.KeyDown)
		up := list.Shortcut(0, ui.KeyUp)
		home := list.Shortcut(0, ui.KeyHome)
		end := list.Shortcut(0, ui.KeyEnd)
		// Enter opens a new terminal for the selected server, as a double
		// click does, through the same unsaved-connection guard.
		if list.Shortcut(0, ui.KeyEnter) {
			if id := a.selected; slices.ContainsFunc(rows, func(r connectionRailRow) bool { return !r.header && r.connection.ID == id }) {
				a.navigate(func() { a.switchHost(id); a.openShellMode(true) })
			}
		}
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
	m.Submenu(tr(nil, "终端连接"), func(menu *ui.Menu) {
		connect := func(route connectcfg.RouteSelection) {
			a.selected = previous
			a.navigate(func() {
				a.switchHost(d.ID)
				a.openShellForHostAfter(d.ID, true, nil, route)
			})
		}
		if menu.Item(tr(nil, "使用默认线路")).Chosen() {
			connect(connectcfg.RouteSelection{})
		}
		if menu.Item(tr(nil, "本次直连")).Chosen() {
			connect(connectcfg.RouteSelection{Mode: "direct"})
		}
		live := a.liveConnectionRoutes(d.ID)
		if len(live) > 0 {
			menu.Separator()
		}
		for _, choice := range live {
			if menu.Item(choice.label).ID(choice.route.Key()).Chosen() {
				connect(choice.route)
			}
		}
		relays := a.relayServerOptions(d.ID)
		if len(a.routes) > 0 || len(relays) > 0 {
			menu.Separator()
		}
		if len(relays) > 0 {
			relayServerMenu(nil, menu, relays, connect)
		}
		for _, route := range a.routes {
			label := route.Name
			if route.Relay != "" {
				label += " · " + tr(nil, "SSH 跳板机")
			}
			if menu.Item(label).Chosen() {
				connect(connectcfg.RouteSelection{Mode: "saved", ID: route.ID})
			}
		}
	})
	if m.Item(tr(nil, "新建端口转发…")).Chosen() {
		a.openTunnels(d.ID)
	}
	if m.Item(tr(nil, "VNC 远程桌面…")).Chosen() {
		a.openVNC(d.ID)
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
	if group == "" {
		return tr(c, "未分组")
	}
	return group
}
