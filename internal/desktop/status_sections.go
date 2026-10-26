package desktop

import (
	"fmt"
	"strings"

	"github.com/egoist/mygo/ui"
	"sshclient/internal/desktop/components"
	"sshclient/internal/monitor"
)

func coreParts(c *ui.Context, core monitor.CPUUsage) []components.MeterPart {
	if !core.Valid {
		return nil
	}
	return []components.MeterPart{{Value: core.System, Color: ui.Hex("#ef5350")}, {Value: core.User, Color: ui.Hex("#2bc66d")}, {Value: core.Guest, Color: ui.Hex("#a855f7")}, {Value: core.Wait, Color: ui.Hex("#f59e0b")}, {Value: core.Steal, Color: ui.Hex("#eab308")}}
}

func (a *app) coreGrid(c *ui.Context, s *monitor.Stats) {
	cores := s.Cores[:min(len(s.Cores), 256)]
	coreMeterLayout.Grid(c, a.resourceContentWidth(c), len(cores), func(i int) {
		core := cores[i]
		ui.Column(c).MinWidth(0).Gap(5).Label(tr(c, "核心 ") + core.Name).Children(func() {
			value := "—"
			if core.Valid {
				value = percentText(core.Used)
			}
			ui.Text(c, strings.ToUpper(core.Name)+"  "+value).FontSize(11)
			tooltip := tr(c, "等待两次有效采样")
			if core.Valid {
				tooltip = fmt.Sprintf(tr(c, "用户 %.1f%% · 系统 %.1f%% · Guest %.1f%% · I/O 等待 %.1f%% · Steal %.1f%%"), core.User, core.System, core.Guest, core.Wait, core.Steal)
			}
			components.SegmentedMeter(c, coreParts(c, core)).Tooltip(tooltip)
		})
	}).Label(tr(c, "逻辑核心网格"))
	if len(s.Cores) > 256 {
		ui.Text(c, tr(c, "显示前 256 个逻辑核心")).FontSize(10).TextColor(c.Theme().TextMuted)
	}
}

func (a *app) coreSummary(c *ui.Context, s *monitor.Stats) {
	if len(s.Cores) == 0 {
		return
	}
	components.Surface(c, tr(c, "核心负载")).Children(func() {
		sectionTitle(c, tr(c, "各核心负载"))
		ui.Row(c).FillWidth().Wrap().Gap(12).Children(func() {
			for _, metric := range []struct {
				name  string
				v     float64
				color ui.Color
			}{{tr(c, "系统"), s.CPUDetail.System, ui.Hex("#ef5350")}, {tr(c, "用户"), s.CPUDetail.User, ui.Hex("#2bc66d")}, {"Guest", s.CPUDetail.Guest, ui.Hex("#a855f7")}, {tr(c, "I/O 等待"), s.CPUDetail.Wait, ui.Hex("#f59e0b")}, {"Steal", s.CPUDetail.Steal, ui.Hex("#eab308")}} {
				value := "—"
				if s.CPUDetail.Valid {
					value = percentText(metric.v)
				}
				ui.Text(c, "● "+metric.name+" "+value).FontSize(10).TextColor(metric.color)
			}
		})
		a.coreGrid(c, s)
	})
}

func (a *app) networkCards(c *ui.Context, s *monitor.Stats) {
	meta := map[string]monitor.Interface{}
	if s.Inventory != nil {
		for _, n := range s.Inventory.Interfaces {
			meta[n.Name] = n
		}
	}
	var physical, virtual []monitor.Network
	for _, n := range s.Networks {
		m, known := meta[n.Name]
		isVirtual := m.Virtual || n.Name == "lo" || !known && (strings.HasPrefix(n.Name, "veth") || strings.HasPrefix(n.Name, "docker") || strings.HasPrefix(n.Name, "br-"))
		if isVirtual && n.Name != s.PrimaryNetwork() {
			virtual = append(virtual, n)
		} else {
			physical = append(physical, n)
		}
	}
	a.networkGroup(c, physical, meta, s.DefaultNetwork, false)
	if s.TCP.Available {
		components.Surface(c, tr(c, "TCP 连接状态")).Children(func() {
			sectionTitle(c, tr(c, "TCP 连接"))
			cols := 4
			if a.resourceContentWidth(c) < 550 {
				cols = 2
			}
			ui.Grid(c).Columns(cols).FillWidth().Gap(10).Children(func() {
				rate := "—"
				if s.TCP.RateValid {
					rate = percentText(s.TCP.RetransmitPercent)
				}
				statValue(c, tr(c, "重传率（采样间隔）"), rate)
				statValue(c, tr(c, "主动建连（累计）"), fmt.Sprint(s.TCP.ActiveOpens))
				statValue(c, tr(c, "被动建连（累计）"), fmt.Sprint(s.TCP.PassiveOpens))
				statValue(c, tr(c, "当前已建立"), fmt.Sprint(s.TCP.Established))
			})
		})
	}
	if len(virtual) > 0 {
		state := a.managementState()
		ui.Collapsible(c, fmt.Sprintf(tr(c, "虚拟网卡 · %d"), len(virtual)), &state.virtualOpen, func() {
			a.networkGroup(c, virtual, meta, "", true)
		})
	}
}

func (a *app) networkGroup(c *ui.Context, rows []monitor.Network, meta map[string]monitor.Interface, primary string, virtual bool) {
	if len(rows) == 0 {
		return
	}
	width := a.resourceContentWidth(c)
	if virtual {
		// The native disclosure indents its content by this theme spacing.
		width = max(0, width-c.Theme().Space(5.5))
	}
	columns, cardWidth := networkCardLayout.ColumnsFor(width, len(rows)), networkCardLayout.CellWidth(width, len(rows))
	cell := func(i int) { a.networkCard(c, rows[i], meta[rows[i].Name], rows[i].Name == primary, cardWidth) }
	if !virtual || len(rows) <= 4 {
		networkCardLayout.Grid(c, width, len(rows), cell).Label(tr(c, "网卡卡片"))
		return
	}
	// Virtualize complete grid rows, retaining measured heights and stable
	// interface identities as the viewport changes between one and two columns.
	state := a.managementState()
	state.networkList.Key = func(row int) any { return rows[row*columns].Name }
	components.List(c, &state.networkList, (len(rows)+columns-1)/columns, func(row int) {
		start := row * columns
		components.ResourceRow(c, columns, min(columns, len(rows)-start), func(i int) { cell(start + i) })
	}).FillWidth().Gap(8).Height(320).Label(tr(c, "虚拟网卡列表"))
}

func (a *app) networkCard(c *ui.Context, n monitor.Network, meta monitor.Interface, primary bool, width float32) {
	components.Surface(c, tr(c, "网卡 ")+n.Name).Children(func() {
		ui.Row(c).FillWidth().Gap(8).AlignItems(ui.Center).Children(func() {
			color := c.Theme().TextMuted
			if meta.State == "up" {
				color = c.Theme().Success
			}
			ui.Text(c, "●").TextColor(color).FontSize(11)
			ui.Text(c, reviewText(n.Name)).Bold().FontSize(12).Grow(1).MinWidth(0).SingleLine().Tooltip(n.Name)
			if primary {
				ui.Text(c, tr(c, "默认路由")).FontSize(10).TextColor(c.Theme().TextMuted)
			}
		})
		if len(meta.Addresses) > 0 {
			address := strings.Join(meta.Addresses, " · ")
			ui.Text(c, reviewText(address)).FillWidth().MinWidth(0).FontSize(11).SingleLine().TextColor(c.Theme().TextMuted).Selectable().Tooltip(address)
		}
		cols := 4
		if width-18 < 550 {
			cols = 2
		}
		ui.Grid(c).Columns(cols).FillWidth().Gap(8).Children(func() {
			statValue(c, tr(c, "↓ 接收 / 秒"), sampleRate(n.RXRate, n.Valid)).Label(tr(c, "网卡 ") + n.Name + tr(c, " 接收速率"))
			statValue(c, tr(c, "↑ 发送 / 秒"), sampleRate(n.TXRate, n.Valid)).Label(tr(c, "网卡 ") + n.Name + tr(c, " 发送速率"))
			statValue(c, tr(c, "累计接收"), size(n.RX)).Label(tr(c, "网卡 ") + n.Name + tr(c, " 累计接收"))
			statValue(c, tr(c, "累计发送"), size(n.TX)).Label(tr(c, "网卡 ") + n.Name + tr(c, " 累计发送"))
		})
	})
}
