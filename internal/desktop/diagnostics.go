package desktop

import (
	"fmt"
	"sshclient/internal/desktop/components"
	"strings"
	"time"

	"github.com/egoist/mygo/ui"
	"sshclient/internal/monitor"
)

func (a *app) diagnosticsView(c *ui.Context) {
	sess := a.serverStateSession()
	ui.Column(c).FillWidth().Grow(1).MinHeight(0).Padding(8).Gap(6).Children(func() {
		narrow := a.statusPaneWidth(c) < 650
		controls := func() {
			if a.diagnosticTab == 0 {
				if a.statusRange == "" {
					a.statusRange = "5 分钟"
				}
				sourceSelect(c, &a.statusRange, []string{"1 分钟", "5 分钟", "15 分钟"}).MinWidth(0).Width(96).Label(tr(c, "趋势时间范围"))
			}
			if sess != nil && sess.stats != nil {
				a.statusIndicator(c, sess)
			}
		}
		ui.Row(c).FillWidth().Shrink(0).AlignItems(ui.Center).Gap(8).Children(func() {
			ui.Tabs(c, &a.diagnosticTab, tr(c, "概览"), tr(c, "进程"), tr(c, "自启服务"), tr(c, "系统信息"), "Docker").Grow(1)
			if !narrow {
				controls()
			}
		})
		if narrow {
			ui.Row(c).FillWidth().Shrink(0).AlignItems(ui.Center).Gap(8).Children(controls)
		}
		if sess == nil {
			ui.Text(c, tr(c, "连接服务器后自动显示状态")).FontSize(11).TextColor(c.Theme().TextMuted)
			return
		}
		if sess.stats == nil {
			ui.Text(c, monitorStatusText(c, sess.monitorStatus)).FontSize(11).TextColor(c.Theme().TextMuted)
			return
		}
		s := sess.stats
		switch a.diagnosticTab {
		case 1:
			a.processView(c, s)
		case 2:
			a.startupView(c, s.Inventory)
		case 3:
			a.systemInfoView(c, s)
		case 4:
			a.dockerView(c, s.Inventory)
		default:
			a.overview(c, sess)
		}
		a.managementFeedback(c)
	})
}

func (a *app) hasSession(target *session) bool {
	for _, sess := range a.sessions {
		if sess == target {
			return true
		}
	}
	return false
}

func (a *app) serverStateSession() *session {
	if a.active >= 0 && a.active < len(a.sessions) {
		return a.sessions[a.active]
	}
	return nil
}

func (a *app) processView(c *ui.Context, s *monitor.Stats) {
	state := a.managementState()
	var processes []monitor.Process
	if a.processSort.Column == "" {
		a.processSort = ui.SortOrder{Column: "cpu", Descending: true}
	}

	processes = make([]monitor.Process, 0, len(s.Processes))
	for _, p := range s.Processes {
		if strings.Contains(strings.ToLower(p.Name+" "+p.User+" "+fmt.Sprint(p.PID)), strings.ToLower(a.processSearch)) {
			processes = append(processes, p)
		}
	}
	sortProcesses(processes, a.processSort, s.MemoryTotal)
	var chosen *monitor.Process
	if state.processChoice.Len() == 1 {
		for n := range processes {
			p := &processes[n]
			if state.processChoice.Has(processIdentity{p.PID, p.StartTicks, s.BootID}) {
				chosen = p
				break
			}
		}
	}
	ui.Row(c).FillWidth().AlignItems(ui.Center).Gap(6).Children(func() {
		ui.TextInput(c, &a.processSearch).Grow(1).Placeholder(tr(c, "搜索进程、PID 或账户")).Label(tr(c, "搜索进程"))
		actionMenu(c, chosen == nil || state.busy || !a.statusActionsAvailable(), func(m *ui.Menu) {
			if chosen != nil {
				a.processMenu(m, *chosen, s)
			}
		})
	})
	state.processTable.Sort = &a.processSort
	state.processTable.Selected = &state.processSelected
	state.processTable.Selection = &state.processChoice
	state.processTable.Key = func(i int) any { return processIdentity{processes[i].PID, processes[i].StartTicks, s.BootID} }
	state.processTable.Label = func(i int) string { return processes[i].Name }
	components.Table(c, &state.processTable, []ui.TableColumn{{ID: "name", Title: tr(c, "进程"), MinWidth: 120, Sortable: true}, {ID: "pid", Title: "PID", Width: 70, Align: ui.End, Sortable: true}, {ID: "cpu", Title: "CPU %", Width: 80, Align: ui.End, Sortable: true}, {ID: "memory", Title: tr(c, "内存"), Width: 92, Align: ui.End, Sortable: true}, {ID: "user", Title: tr(c, "账户"), Width: 95, Sortable: true}, {ID: "state", Title: tr(c, "状态"), Width: 72, Sortable: true}, {ID: "elapsed", Title: tr(c, "运行时间"), Width: 110, Sortable: true}}, len(processes), func(row, col int) {
		p := processes[row]
		text := p.Name
		if col == 1 {
			text = fmt.Sprint(p.PID)
		}
		if col == 2 {
			text = fmt.Sprintf("%.1f", p.CPU)
		}
		if col == 3 {
			if p.RSS > 0 {
				text = size(p.RSS)
			} else {
				text = fmt.Sprintf("%.1f%%", p.Memory)
			}
		}
		if col == 4 {
			text = nonEmpty(p.User)
		}
		if col == 5 {
			text = nonEmpty(p.State)
		}
		if col == 6 {
			text = nonEmpty(p.Elapsed)
		}
		cell := ui.Row(c).Fill().AlignItems(ui.Center).Children(func() { components.CellText(c, reviewText(text)).Tooltip(text) })
		cell.ContextMenu(func(m *ui.Menu) {
			state.processSelected = row
			state.processChoice.Clear()
			state.processChoice.Add(processIdentity{p.PID, p.StartTicks, s.BootID})
			a.processMenu(m, p, s)
		})
	}).FillWidth().Grow(1).MinHeight(0).Label(tr(c, "进程列表"))
	note := fmt.Sprintf(tr(c, "高占用进程 %d 项 · CPU 为进程运行期间平均值"), len(processes))
	if s.ProcessNote != "" {
		note = s.ProcessNote
	}
	ui.Text(c, note).FontSize(10).TextColor(c.Theme().TextMuted).Tooltip(tr(c, "合并 CPU 和内存占用最高的各 32 个进程；进程 CPU 来自 ps，可超过 100%（多核）。"))
}

func (a *app) startupView(c *ui.Context, i *monitor.Inventory) {
	state := a.managementState()
	var items []monitor.Service
	if i == nil {
		ui.Text(c, tr(c, "尚未读取系统服务信息")).FontSize(11)
		return
	}

	items = make([]monitor.Service, 0, len(i.Services))
	query := strings.ToLower(a.serviceSearch)
	for _, s := range i.Services {
		if strings.Contains(strings.ToLower(s.Name+" "+s.Description), query) {
			items = append(items, s)
		}
	}
	if a.serviceSort.Column == "" {
		a.serviceSort = ui.SortOrder{Column: "name"}
	}
	sortServices(items, a.serviceSort)
	var chosen *monitor.Service
	if state.serviceChoice.Len() == 1 {
		for n := range items {
			if state.serviceChoice.Has(items[n].Name) {
				chosen = &items[n]
				break
			}
		}
	}
	ui.Row(c).FillWidth().Gap(7).AlignItems(ui.Center).Children(func() {
		ui.TextInput(c, &a.serviceSearch).Grow(1).Placeholder(tr(c, "搜索服务")).Label(tr(c, "搜索自启服务"))
		ui.Text(c, i.Manager).FontSize(10).TextColor(c.Theme().TextMuted)
		actionMenu(c, chosen == nil || state.busy || !a.statusActionsAvailable(), func(m *ui.Menu) {
			if chosen != nil {
				a.serviceMenu(m, *chosen, i.Manager)
			}
		})
	})
	state.serviceTable.Sort = &a.serviceSort
	state.serviceTable.Selected = &state.serviceSelected
	state.serviceTable.Selection = &state.serviceChoice
	state.serviceTable.Key = func(n int) any { return items[n].Name }
	state.serviceTable.Label = func(n int) string { return items[n].Name }
	components.Table(c, &state.serviceTable, []ui.TableColumn{{ID: "name", Title: tr(c, "服务"), MinWidth: 100, Sortable: true}, {ID: "state", Title: tr(c, "状态"), Width: 90, Sortable: true}, {ID: "enabled", Title: tr(c, "启用"), Width: 95, Sortable: true}}, len(items), func(row, col int) {
		s := items[row]
		text := s.Name
		if col == 1 {
			text = serviceState(s.Active, s.Sub)
		}
		if col == 2 {
			text = serviceEnabled(s.Enabled)
		}
		cell := ui.Row(c).Fill().AlignItems(ui.Center).Children(func() { components.CellText(c, text).Tooltip(s.Description) })
		cell.ContextMenu(func(m *ui.Menu) {
			state.serviceSelected = row
			state.serviceChoice.Clear()
			state.serviceChoice.Add(s.Name)
			a.serviceMenu(m, s, i.Manager)
		})
	}).FillWidth().Grow(1).MinHeight(0).Label(tr(c, "自启服务列表"))
	note := i.ServiceNote
	if note == "" {
		note = fmt.Sprintf(tr(c, "%d 项服务"), len(items))
	}
	ui.Text(c, note).FontSize(10).TextColor(c.Theme().TextMuted)
}

func serviceState(active, sub string) string {
	switch active {
	case "active":
		if sub == "running" {
			return tr(nil, "运行中")
		}
		return tr(nil, "已激活")
	case "failed":
		return tr(nil, "失败")
	case "inactive":
		return tr(nil, "未运行")
	case "activating":
		return tr(nil, "启动中")
	case "deactivating":
		return tr(nil, "停止中")
	}
	return tr(nil, "未读取")
}

func uptime(seconds float64) string {
	d := time.Duration(seconds) * time.Second
	days := int(d.Hours()) / 24
	hours := int(d.Hours()) % 24
	minutes := int(d.Minutes()) % 60
	if days > 0 {
		return fmt.Sprintf(tr(nil, "%d 天 %d 小时"), days, hours)
	}
	return fmt.Sprintf(tr(nil, "%d 小时 %d 分钟"), hours, minutes)
}

func monitorStatusText(c *ui.Context, source string) string {
	switch source {
	case "正在读取服务器状态…", "自动更新", "已断开，保留最后状态":
		return tr(c, source)
	default:
		return source
	}
}
