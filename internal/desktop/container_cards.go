package desktop

import (
	"fmt"
	"strings"
	"time"

	"github.com/egoist/mygo/ui"
	"sshclient/internal/desktop/components"
	"sshclient/internal/monitor"
)

func (a *app) containerMetrics(d *monitor.DockerInfo) map[string]monitor.ContainerStats {
	rows := map[string]monitor.ContainerStats{}
	sess := a.serverStateSession()
	if sess == nil || sess.stats == nil || sess.stats.DockerStats == nil {
		return rows
	}
	s := sess.stats.DockerStats
	if s.Status != "ready" || s.EngineID != d.EngineID || s.Socket != d.Socket || time.Since(s.Time) > 20*time.Second {
		return rows
	}
	for _, row := range s.Rows {
		rows[row.ID] = row
	}
	return rows
}

func (a *app) containerCard(c *ui.Context, row monitor.Container, d *monitor.DockerInfo, metrics monitor.ContainerStats) {
	if row.State != "running" && row.State != "paused" {
		metrics = monitor.ContainerStats{}
	}
	card := components.Surface(c, tr(c, "容器 ")+row.ID).FillHeight().Gap(8)
	card.Children(func() {
		ui.Row(c).FillWidth().Gap(7).AlignItems(ui.Center).Children(func() {
			color := conditionColor(c, row)
			ui.Text(c, "●").TextColor(color).FontSize(10)
			if components.Button(c, reviewText(row.Names), components.ButtonOptions{Variant: components.Quiet}).Padding(0).Height(24).MinWidth(0).Grow(1).Justify(ui.Start).Label(tr(c, "查看容器 ") + row.Names).Tooltip(row.Names).Clicked() {
				a.openContainerDetails(row, d)
			}
			ui.Text(c, conditionText(row)).FontSize(10).TextColor(color).Tooltip(row.Status)
			components.IconButton(c, "⋯", tr(c, "容器操作 ")+row.Names).Size(22, 22).Menu(func(m *ui.Menu) { a.containerMenu(m, row, d) })
		})
		ui.Row(c).FillWidth().Gap(12).AlignItems(ui.Center).Children(func() {
			value := "—"
			if metrics.CPUValid {
				value = fmt.Sprintf("%.0f%%", metrics.CPU)
			}
			ring := components.Ring(c, metrics.CPU, value, c.Theme().Success).Label(tr(c, "容器 CPU ") + row.Names).Tooltip(tr(c, "CPU 使用率按单核计算，多核容器可以超过 100%"))
			if a.statusPaneWidth(c) < 400 {
				ring.Size(44, 44)
			}
			ui.Grid(c).Columns(3).Grow(1).MinWidth(0).Gap(10).Children(func() {
				memory, read, write, rx, tx := "—", "—", "—", "—", "—"
				if metrics.MemoryValid {
					memory = size(metrics.Memory)
				}
				if metrics.BlockValid {
					read, write = size(metrics.Read), size(metrics.Write)
				}
				if metrics.NetworkValid {
					rx, tx = size(metrics.RX), size(metrics.TX)
				}
				ui.Column(c).MinWidth(0).Gap(4).Children(func() {
					components.Metric(c, tr(c, "内存"), memory).Tooltip(tr(c, "Docker 内存口径（扣除可回收缓存），上限 ") + size(metrics.Limit))
					if metrics.MemoryValid {
						ui.Text(c, percentText(metrics.MemoryPercent)).FontSize(10).TextColor(c.Theme().TextMuted)
					}
				})
				ui.Column(c).MinWidth(0).Gap(4).Children(func() {
					ui.Text(c, tr(c, "磁盘累计")).FontSize(10).TextColor(c.Theme().TextMuted)
					ui.Text(c, tr(c, "读 ")+read).FontSize(11).SingleLine()
					ui.Text(c, tr(c, "写 ")+write).FontSize(11).SingleLine()
				})
				ui.Column(c).MinWidth(0).Gap(4).Children(func() {
					ui.Text(c, tr(c, "网络累计")).FontSize(10).TextColor(c.Theme().TextMuted)
					ui.Text(c, "↓ "+rx).FontSize(11).SingleLine()
					ui.Text(c, "↑ "+tx).FontSize(11).SingleLine()
				})
			})
		})
		parts := []string{}
		for _, part := range []string{row.Image, row.Status} {
			if part != "" {
				parts = append(parts, part)
			}
		}
		ui.Text(c, reviewText(strings.Join(parts, " · "))).FontSize(10).SingleLine().TextColor(c.Theme().TextMuted).Tooltip(strings.Join(parts, " · ") + " · " + row.Ports)
	})
	card.ContextMenu(func(m *ui.Menu) { a.containerMenu(m, row, d) })
}

func composeProject(row monitor.Container) string { return monitor.ComposeProjectName(row) }
