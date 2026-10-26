package desktop

import (
	"fmt"
	"strings"
	"time"

	"github.com/egoist/mygo/ui"
	"sshclient/internal/desktop/components"
	"sshclient/internal/monitor"
)

func (a *app) statusIndicator(c *ui.Context, sess *session) {
	s := sess.stats
	text, color := tr(c, "实时"), c.Theme().Success
	if sess.closed {
		text, color = tr(c, "已断开 · 最后状态"), c.Theme().TextMuted
	} else if sess.monitorStatus != "" && !strings.HasPrefix(sess.monitorStatus, "自动更新") {
		text, color = monitorStatusText(c, sess.monitorStatus), c.Theme().Warning
	} else if time.Since(s.Time) > 10*time.Second {
		text, color = tr(c, "更新延迟 · ")+s.Time.Local().Format("15:04:05"), c.Theme().Warning
	}
	ui.Text(c, "● "+reviewText(text)).FontSize(10).SingleLine().TextColor(color).Tooltip(tr(c, "最后采集 ") + s.Time.Local().Format("2006-01-02 15:04:05") + tr(c, " · 连接期间每 2 秒更新"))
}

func (a *app) selectedNetwork(s *monitor.Stats) string {
	for _, n := range s.Networks {
		if n.Name == a.statusNetwork {
			return n.Name
		}
	}
	return s.PrimaryNetwork()
}

func (a *app) overview(c *ui.Context, sess *session) {
	s := sess.stats
	// Fixtures and a just-attached session may have only a latest sample.
	if len(sess.history.points) == 0 {
		sess.history.add(s)
	}
	if a.statusMetric != "" {
		a.resourceNavigation(c, s)
	}
	state := a.managementState()
	scroll := &state.resourceOverviewScroll
	view := "overview"
	if a.statusMetric != "" {
		scroll, view = &state.resourceDetailScroll, "detail"
	}
	viewport := ui.Scroll(c).Key("server-resources:"+view).TrackScroll(scroll).FillWidth().Grow(1).MinHeight(0).
		Padding(0, statusScrollGutter, 6, 0).Label(tr(c, "服务器资源内容"))
	// Observe committed geometry after layout. Bounds() while building still
	// describes the previous frame and cannot detect a just-resized window.
	viewport.Draw(func(_ *ui.Painter, r ui.Rect) {
		if state.resourceViewportHeight != r.H || state.resourceViewportWidth != r.W {
			state.resourceViewportHeight, state.resourceViewportWidth = r.H, r.W
			c.AnimationFrame()
		}
	})
	height := state.resourceViewportHeight
	compact := height > 0 && height < 260
	width := a.resourceContentWidth(c)
	viewport.Children(func() {
		ui.Column(c).FillWidth().Gap(8).Children(func() {
			if a.statusMetric != "" {
				a.resourceDetail(c, sess)
				return
			}
			overviewCardLayout.Grid(c, width, 4, func(index int) {
				switch index {
				case 0:
					cpuText := fmt.Sprintf("%.1f%%", s.CPU)
					if s.CPUDetail.Name != "" && !s.CPUDetail.Valid {
						cpuText = "—"
					}
					a.resourceCard(c, "CPU", cpuText, coreText(s), &sess.history, cpuSeries(c), 100, compact)
				case 1:
					a.resourceCard(c, "内存", fmt.Sprintf("%.1f%%", percentage(s.MemoryUsed, s.MemoryTotal)), size(s.MemoryUsed)+" / "+size(s.MemoryTotal), &sess.history, memorySeries(c), 100, compact)
				case 2:
					var rx, tx float64
					var netValid bool
					network := a.selectedNetwork(s)
					for _, n := range s.Networks {
						if n.Name == network {
							rx, tx = n.RXRate, n.TXRate
							netValid = n.Valid
						}
					}
					a.resourceCard(c, "网络", "↓ "+sampleRate(rx, netValid), "↑ "+sampleRate(tx, netValid)+" · "+network, &sess.history, networkSeries(c, network), 0, compact)
				case 3:
					var read, write float64
					var ioValid bool
					for _, b := range s.BlockIO {
						read += b.ReadRate
						write += b.WriteRate
						ioValid = ioValid || b.Valid
					}
					a.resourceCard(c, "磁盘", tr(c, "读 ")+sampleRate(read, ioValid), tr(c, "写 ")+sampleRate(write, ioValid), &sess.history, ioSeries(c), 0, compact)
				}
			}).Label(tr(c, "概览资源卡片"))
			a.systemSummary(c, s)
		})
	})
}

func (a *app) resourceContentWidth(c *ui.Context) float32 {
	if width := a.managementState().resourceViewportWidth; width > 0 {
		return max(0, width-statusScrollGutter)
	}
	return max(0, a.statusPaneWidth(c)-statusScrollGutter)
}

// Resource navigation belongs to the panel chrome, outside the scrolling data.
// Neither opening a detail nor returning changes the terminal's layout.
func (a *app) resourceNavigation(c *ui.Context, s *monitor.Stats) {
	ui.Row(c).FillWidth().Shrink(0).Padding(3, 0, 7, 0).Gap(10).AlignItems(ui.Center).
		BorderWidth(0, 0, 1, 0).BorderColor(c.Theme().Border).Label(tr(c, "资源详情导航")).Children(func() {
		if components.Button(c, tr(c, "← 返回概览")).Label(tr(c, "返回概览")).Height(32).TextColor(c.Theme().Accent).Clicked() {
			a.statusMetric = ""
		}
		ui.Text(c, fmt.Sprintf(tr(c, "%s详情"), tr(c, a.statusMetric))).Bold().FontSize(13).Grow(1).MinWidth(0).SingleLine()
		if a.statusMetric == "网络" && len(s.Networks) > 0 {
			names := make([]string, 0, len(s.Networks))
			for _, n := range s.Networks {
				names = append(names, n.Name)
			}
			a.statusNetwork = a.selectedNetwork(s)
			ui.Select(c, &a.statusNetwork, names).Width(120).Label(tr(c, "监控网卡"))
		}
	})
}

func coreText(s *monitor.Stats) string {
	n := len(s.Cores)
	if s.Inventory != nil && s.Inventory.Cores > 0 {
		n = s.Inventory.Cores
	}
	if n == 0 {
		return tr(nil, "负载 ") + s.Load
	}
	return fmt.Sprintf(tr(nil, "%d 核 · 负载 %s"), n, s.Load)
}

func (a *app) resourceCard(c *ui.Context, name, value, note string, h *statusHistory, series []trendSeries, ceiling float64, compact bool) {
	height, padding, gap, font := float32(106), float32(8), float32(3), float32(17)
	if compact {
		height, padding, gap, font = 64, 6, 2, 15
	}
	card := components.Button(c, "").MinWidth(0).Height(height).Padding(padding).Label(fmt.Sprintf(tr(c, "%s详情"), tr(c, name))).Tooltip(tr(c, "查看") + tr(c, name) + tr(c, "趋势与详情\n") + value + " · " + note)
	card.Children(func() {
		ui.Column(c).FillWidth().Gap(gap).Children(func() {
			ui.Row(c).FillWidth().Children(func() {
				ui.Text(c, tr(c, name)).FontSize(11).TextColor(c.Theme().TextMuted).Grow(1)
				ui.Text(c, "›").FontSize(13).TextColor(c.Theme().TextMuted)
			})
			ui.Text(c, value).FontSize(font).Bold().SingleLine().Tooltip(value)
			if !compact {
				trendChart(c, tr(c, name)+tr(c, "迷你趋势"), h, a.statusWindow(), series, ceiling, 28, true)
			}
			ui.Text(c, note).FontSize(10).TextColor(c.Theme().TextMuted).SingleLine().Tooltip(note)
		})
	})
	if card.Clicked() {
		a.statusMetric = name
		a.managementState().resourceDetailScroll.X, a.managementState().resourceDetailScroll.Y = 0, 0
	}
}

func (a *app) resourceDetail(c *ui.Context, sess *session) {
	s := sess.stats
	history := &sess.history
	window := a.statusWindow()
	chartHeight := a.resourceChartHeight(c)
	switch a.statusMetric {
	case "CPU":
		cpu := percentText(s.CPU)
		if s.CPUDetail.Name != "" && !s.CPUDetail.Valid {
			cpu = "—"
		}
		a.detailHeadline(c, cpu, coreText(s))
		series := []trendSeries{{tr(c, "总使用"), c.Theme().Accent, func(p statusPoint) (float64, bool) { return p.cpu, p.cpuValid }, percentText}, {tr(c, "I/O 等待"), ui.Hex("#f59e0b"), func(p statusPoint) (float64, bool) { return p.wait, p.cpuValid }, percentText}}
		trendLegend(c, series)
		trendChart(c, tr(c, "CPU 使用率趋势"), history, window, series, 100, chartHeight, false)
		ui.Grid(c).Columns(components.ResourceColumns(a.resourceContentWidth(c), 130, 4)).FillWidth().Gap(10).Children(func() {
			statValue(c, tr(c, "用户态"), percentText(s.CPUDetail.User))
			statValue(c, tr(c, "系统态"), percentText(s.CPUDetail.System))
			statValue(c, tr(c, "I/O 等待"), percentText(s.CPUDetail.Wait))
			statValue(c, tr(c, "空闲"), percentText(max(0, 100-s.CPU-s.CPUDetail.Wait)))
			statValue(c, "Guest", percentText(s.CPUDetail.Guest))
			statValue(c, "Steal", percentText(s.CPUDetail.Steal))
		})
		sectionTitle(c, tr(c, "各核心使用率"))
		if len(s.Cores) == 0 {
			ui.Text(c, tr(c, "尚未读取各核心数据")).FontSize(11).TextColor(c.Theme().TextMuted)
		}
		a.coreGrid(c, s)
		sectionTitle(c, tr(c, "系统负载"))
		fields := strings.Split(s.Load, " / ")
		ui.Grid(c).Columns(components.ResourceColumns(a.resourceContentWidth(c), 130, 3)).FillWidth().Gap(10).Children(func() {
			for j, title := range []string{tr(c, "1 分钟"), tr(c, "5 分钟"), tr(c, "15 分钟")} {
				value := "—"
				if j < len(fields) {
					value = fields[j]
				}
				statValue(c, title, value)
			}
		})
	case "内存":
		a.detailHeadline(c, percentText(percentage(s.MemoryUsed, s.MemoryTotal)), size(s.MemoryUsed)+tr(c, " 已用 / ")+size(s.MemoryTotal)+tr(c, " 总量"))
		trendLegend(c, memorySeries(c))
		trendChart(c, tr(c, "内存使用率趋势"), history, window, memorySeries(c), 100, chartHeight, false)
		usageBar(c, percentage(s.MemoryUsed, s.MemoryTotal), ui.Hex("#8b5cf6"))
		ui.Grid(c).Columns(components.ResourceColumns(a.resourceContentWidth(c), 140, 3)).FillWidth().Gap(12).Children(func() {
			statValue(c, tr(c, "已用"), size(s.MemoryUsed))
			statValue(c, tr(c, "可用"), size(s.MemoryAvailable))
			statValue(c, tr(c, "空闲"), size(s.MemoryFree))
			statValue(c, tr(c, "页缓存（含可回收）"), size(s.MemoryCached))
			statValue(c, tr(c, "缓冲区"), size(s.MemoryBuffers))
			value := tr(c, "未启用")
			if s.SwapTotal > 0 {
				value = size(s.SwapUsed) + " / " + size(s.SwapTotal)
			}
			statValue(c, "Swap", value)
		}).Tooltip(tr(c, "已用 = 总量 − 可用；可用包含可回收缓存，缓存与其他项并非互斥。"))
	case "网络":
		name := a.selectedNetwork(s)
		var current monitor.Network
		for _, n := range s.Networks {
			if n.Name == name {
				current = n
			}
		}
		a.detailHeadline(c, "↓ "+sampleRate(current.RXRate, current.Valid)+"    ↑ "+sampleRate(current.TXRate, current.Valid), name+tr(c, " · 接收 / 发送"))
		series := networkSeries(c, name)
		trendLegend(c, series)
		trendChart(c, tr(c, "网卡流量趋势"), history, window, series, 0, chartHeight, false)
		ui.Grid(c).Columns(components.ResourceColumns(a.resourceContentWidth(c), 140, 3)).FillWidth().Gap(12).Children(func() {
			statValue(c, tr(c, "累计接收"), size(current.RX))
			statValue(c, tr(c, "累计发送"), size(current.TX))
			statValue(c, tr(c, "接收 / 发送包数"), fmt.Sprintf("%d / %d", current.RXPackets, current.TXPackets))
			statValue(c, tr(c, "接收 / 发送错误"), fmt.Sprintf("%d / %d", current.RXErrors, current.TXErrors))
			statValue(c, tr(c, "接收 / 发送丢包"), fmt.Sprintf("%d / %d", current.RXDropped, current.TXDropped))
			statValue(c, tr(c, "默认路由网卡"), nonEmpty(s.DefaultNetwork))
		}).Tooltip(tr(c, "累计值自网卡启动计数；默认按路由网卡展示，避免桥接和容器流量重复累加。"))
		sectionTitle(c, tr(c, "网络接口与连接"))
		a.networkCards(c, s)
	case "磁盘":
		var read, write float64
		var ioValid bool
		for _, b := range s.BlockIO {
			read += b.ReadRate
			write += b.WriteRate
			ioValid = ioValid || b.Valid
		}
		a.detailHeadline(c, tr(c, "读 ")+sampleRate(read, ioValid)+tr(c, "    写 ")+sampleRate(write, ioValid), tr(c, "物理块设备吞吐量"))
		series := ioSeries(c)
		trendLegend(c, series)
		trendChart(c, tr(c, "磁盘读写趋势"), history, window, series, 0, chartHeight, false)
		sectionTitle(c, tr(c, "块设备 I/O"))
		if len(s.BlockIO) == 0 {
			ui.Text(c, tr(c, "当前系统未提供可读取的块设备计数")).FontSize(11).TextColor(c.Theme().TextMuted)
		}
		blockCardLayout.Grid(c, a.resourceContentWidth(c), len(s.BlockIO), func(i int) {
			b := s.BlockIO[i]
			components.Surface(c, tr(c, "块设备 ")+b.Name).Children(func() {
				ui.Row(c).FillWidth().AlignItems(ui.Center).Children(func() {
					ui.Text(c, reviewText(b.Name)).FontSize(12).Bold().Grow(1).MinWidth(0).SingleLine().Tooltip(b.Name)
					ui.Text(c, tr(c, "忙碌 ")+samplePercent(b.Busy, b.Valid)).FontSize(10).TextColor(c.Theme().TextMuted)
				})
				values := [][2]string{{tr(c, "读 / 秒"), sampleRate(b.ReadRate, b.Valid)}, {tr(c, "写 / 秒"), sampleRate(b.WriteRate, b.Valid)}, {tr(c, "IOPS 读 / 写"), "—"}}
				busy := float64(0)
				if b.Valid {
					values[2][1] = fmt.Sprintf("%.1f / %.1f", b.ReadIOPS, b.WriteIOPS)
					busy = b.Busy
				}
				components.ResourceGrid(c, blockCardLayout.CellWidth(a.resourceContentWidth(c), len(s.BlockIO))-18, 100, 3, len(values), func(j int) {
					statValue(c, values[j][0], values[j][1])
				})
				usageBar(c, busy, c.Theme().Accent)
			})
		}).Label(tr(c, "块设备卡片"))
		sectionTitle(c, tr(c, "文件系统容量"))
		a.diskCapacity(c, s, true)
	}
}

func (a *app) detailHeadline(c *ui.Context, value, note string) {
	if !a.fileExpanded && !a.toolsDockRight(c) && !a.renderingToolsWindow && a.resourceContentWidth(c) >= 520 {
		ui.Row(c).FillWidth().AlignItems(ui.Center).Gap(12).Children(func() {
			ui.Text(c, value).FontSize(20).Bold().SingleLine()
			ui.Text(c, note).FontSize(11).TextColor(c.Theme().TextMuted).Grow(1).MinWidth(0).SingleLine().Tooltip(note)
		})
		return
	}
	ui.Column(c).FillWidth().Gap(3).Children(func() {
		ui.Text(c, value).FontSize(23).Bold().FillWidth().MinWidth(0)
		ui.Text(c, note).FontSize(11).TextColor(c.Theme().TextMuted).FillWidth().MinWidth(0)
	})
}

// Fit the primary trend to the bottom dock; secondary data remains scrollable.
// Derive from current layout preferences rather than stale element bounds.
func (a *app) resourceChartHeight(c *ui.Context) float32 {
	if a.fileExpanded || a.toolsDockRight(c) || a.renderingToolsWindow {
		return 178
	}
	_, height := c.Size()
	share := (components.SplitLimits{Min: 30, Max: 90, Default: 72}).Clamp(a.terminalShare)
	return max(70, min(178, height*(100-share)/100-194))
}
func statValue(c *ui.Context, label, value string) ui.Element {
	return ui.Column(c).MinWidth(0).Gap(3).Children(func() {
		ui.Text(c, label).FontSize(10).TextColor(c.Theme().TextMuted).FillWidth().MinWidth(0)
		ui.Text(c, reviewText(value)).FontSize(12).FillWidth().MinWidth(0).Selectable().Tooltip(value)
	})
}
func nonEmpty(s string) string {
	if s == "" {
		return "—"
	}
	return s
}
func sampleRate(v float64, valid bool) string {
	if !valid {
		return "—"
	}
	return rateSize(v)
}
func samplePercent(v float64, valid bool) string {
	if !valid {
		return "—"
	}
	return percentText(v)
}
func sectionTitle(c *ui.Context, title string) { ui.Text(c, title).FontSize(12).Bold() }
func trendLegend(c *ui.Context, series []trendSeries) {
	ui.Row(c).FillWidth().Wrap().Gap(15).Children(func() {
		for _, s := range series {
			ui.Text(c, "● "+s.name).FontSize(10).TextColor(s.color)
		}
	})
}
func usageBar(c *ui.Context, value float64, color ui.Color) {
	bg := c.Theme().Border.Alpha(.6)
	ui.Box(c).FillWidth().Height(5).Draw(func(p *ui.Painter, r ui.Rect) {
		p.Fill(r, bg, 2)
		r.W *= float32(max(0, min(100, value)) / 100)
		p.Fill(r, color, 2)
	})
}

func (a *app) systemSummary(c *ui.Context, s *monitor.Stats) {
	ui.Row(c).FillWidth().MinWidth(0).Wrap().Gap(14).Label(tr(c, "系统摘要")).Children(func() {
		if s.Inventory != nil {
			ui.Text(c, reviewText(s.Inventory.OS+" · "+s.Inventory.Architecture)).MinWidth(0).FontSize(11).TextColor(c.Theme().TextMuted)
		}
		if s.Uptime > 0 {
			ui.Text(c, tr(c, "已运行 ")+uptime(s.Uptime)).FontSize(11).TextColor(c.Theme().TextMuted)
		}
		if s.ProcessCount > 0 {
			ui.Text(c, fmt.Sprintf(tr(c, "%d 任务 · %d 运行"), s.ProcessCount, s.RunningProcesses)).FontSize(11).TextColor(c.Theme().TextMuted)
		}
	})
}

func (a *app) diskCapacity(c *ui.Context, s *monitor.Stats, all bool) {
	rows := make([]monitor.Disk, 0, len(s.Disks))
	for _, d := range s.Disks {
		core := d.Mount == "/" || d.Mount == "/overlay" || d.Mount == "/rom"
		if !all && !core && (d.Filesystem == "tmpfs" || d.Filesystem == "devtmpfs" || d.Filesystem == "overlay" || strings.HasPrefix(d.Mount, "/snap/")) {
			continue
		}
		rows = append(rows, d)
		if !all && len(rows) >= 4 {
			break
		}
	}
	if len(rows) == 0 {
		ui.Text(c, tr(c, "尚未读取文件系统容量")).FontSize(11).TextColor(c.Theme().TextMuted)
		return
	}
	// Use the tool pane's available width, including right docking and detached
	// windows. Keep the same column widths when the last row is incomplete.
	filesystemCardLayout.Grid(c, a.resourceContentWidth(c), len(rows), func(i int) {
		d := rows[i]
		components.Surface(c, tr(c, "文件系统 ")+d.Mount).Children(func() {
			ui.Row(c).FillWidth().Gap(10).AlignItems(ui.Center).Children(func() {
				ui.Text(c, reviewText(d.Mount)).FontSize(14).Bold().Grow(1).MinWidth(0).SingleLine().Selectable().Tooltip(d.Mount)
				ui.Text(c, strings.ToUpper(nonEmpty(d.Filesystem))).FontSize(10).TextColor(c.Theme().TextMuted)
				if d.ModeKnown {
					mode := tr(c, "读写")
					if d.ReadOnly {
						mode = tr(c, "只读")
					}
					ui.Text(c, mode).FontSize(10).TextColor(c.Theme().TextMuted)
				}
			})
			ui.Row(c).FillWidth().Gap(10).Children(func() {
				ui.Text(c, reviewText(d.Device)).FontSize(11).SingleLine().Grow(1).MinWidth(0).TextColor(c.Theme().TextMuted).Tooltip(d.Device)
				ui.Text(c, size(d.Used)+" / "+size(d.Total)).FontSize(12).Bold().SingleLine().Shrink(0)
			})
			color := c.Theme().Accent
			if d.ReadOnly {
				color = c.Theme().TextMuted
			} else if percentage(d.Used, d.Total) >= 90 {
				color = c.Theme().Warning
			}
			usageBar(c, percentage(d.Used, d.Total), color)
			ui.Row(c).FillWidth().Children(func() {
				ui.Text(c, tr(c, "可用 ")+size(d.Free)).FontSize(10).Grow(1).TextColor(c.Theme().TextMuted)
				ui.Text(c, percentText(percentage(d.Used, d.Total))).FontSize(10).TextColor(c.Theme().TextMuted)
			})
		})
	}).Label(tr(c, "文件系统容量卡片"))
}

func (a *app) systemInfoView(c *ui.Context, s *monitor.Stats) {
	ui.Scroll(c).FillWidth().Grow(1).MinHeight(0).Padding(0, statusScrollGutter, 6, 0).Label(tr(c, "系统信息内容")).Children(func() {
		ui.Column(c).FillWidth().Gap(14).Children(func() {
			i := s.Inventory
			if i == nil {
				ui.Text(c, tr(c, "正在读取系统信息…")).FontSize(11)
				return
			}
			sections := []struct {
				title string
				rows  [][2]string
			}{
				{tr(c, "操作系统"), [][2]string{{tr(c, "系统"), i.OS}, {tr(c, "内核"), i.Kernel}, {tr(c, "架构"), i.Architecture}, {tr(c, "运行时间"), uptime(s.Uptime)}, {tr(c, "服务管理器"), i.Manager}}},
				{tr(c, "硬件"), [][2]string{{tr(c, "服务器型号"), i.Model}, {tr(c, "处理器"), i.CPU}, {tr(c, "逻辑核心"), fmt.Sprintf("%d", i.Cores)}, {tr(c, "物理内存"), size(s.MemoryTotal)}, {"Swap", size(s.SwapTotal)}}},
			}
			systemCardLayout.Grid(c, a.statusPaneWidth(c)-statusScrollGutter, len(sections), func(n int) {
				section := sections[n]
				components.Surface(c, tr(c, "系统信息 ")+section.title).Children(func() {
					sectionTitle(c, section.title)
					infoRows(c, section.rows)
				})
			}).Label(tr(c, "系统信息卡片")).Tooltip(tr(c, "系统信息更新于 ") + i.Time.Local().Format("15:04:05"))
		})
	})
}
func infoRows(c *ui.Context, rows [][2]string) {
	ui.Column(c).FillWidth().Gap(0).Children(func() {
		for _, row := range rows {
			ui.Row(c).FillWidth().Padding(7, 0).Gap(10).BorderWidth(0, 0, 1, 0).BorderColor(c.Theme().Border.Alpha(.5)).Children(func() {
				ui.Text(c, row[0]).Width(90).FontSize(11).TextColor(c.Theme().TextMuted)
				ui.Text(c, reviewText(nonEmpty(row[1]))).FontSize(12).Grow(1).MinWidth(0).Selectable().Tooltip(row[1])
			})
		}
	})
}

// Derive the current pane width from this frame's window and split settings.
// Previous element bounds lag a resize and can misroute the first click.
func (a *app) statusPaneWidth(c *ui.Context) float32 {
	w, _ := c.Size()
	if a.renderingToolsWindow {
		return w - 18
	}
	sidebar := a.sidebarWidth
	if sidebar <= 0 {
		sidebar = 190
	}
	w -= 18
	if !a.sidebarHidden {
		w -= sidebar
	}
	if a.toolsDockRight(c) && !a.fileExpanded {
		share := a.terminalWidthShare
		if share <= 0 {
			share = 65
		}
		w = w * (100 - share) / 100
	}
	return w
}
