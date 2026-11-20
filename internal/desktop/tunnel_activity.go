package desktop

import (
	"fmt"

	"github.com/egoist/mygo/ui"
	"sshclient/internal/desktop/components"
)

func (a *app) runningTunnels() []*tunnelRun {
	var runs []*tunnelRun
	for _, run := range a.tunnels.active {
		if run.running && run.ready {
			runs = append(runs, run)
		}
	}
	return runs
}

func stopTunnel(run *tunnelRun) {
	run.openWebsite, run.ready = false, false
	if run.cancel != nil {
		run.cancel()
	}
	run.status = tr(nil, "正在停止…")
}

func (a *app) tunnelActivity(c *ui.Context) {
	runs := a.runningTunnels()
	count := len(runs)
	color := c.Theme().TextMuted
	if count > 0 {
		color = c.Theme().Accent
	}
	button := components.Button(c, "", components.ButtonOptions{Variant: components.Quiet}).Size(32, 32).Padding(0).Gap(2).
		Label(tr(c, "端口转发")).Tooltip(fmt.Sprintf(tr(c, "%d 项运行中 · 新建或管理服务"), count)).TextColor(color).Children(func() {
		ui.Icon(c, components.TunnelIcon).Size(14, 14).Shrink(0)
		if count > 0 {
			ui.Text(c, fmt.Sprint(count)).FontSize(11).SingleLine()
		}
	})
	show := func() {
		a.tunnels.open = true
		a.tunnels.showingServices = true
	}
	if button.Clicked() {
		show()
	}
	button.ContextMenu(func(m *ui.Menu) {
		if m.Item(tr(c, "新建服务…")).ID("new-service").Chosen() {
			a.openTunnels(a.selected)
		}
		if m.Item(tr(c, "查看服务")).ID("view-services").Chosen() {
			show()
		}
		m.Separator()
		if len(a.runningTunnels()) == 0 {
			m.Item(tr(c, "暂无运行中的服务")).Disabled(true)
		}
		for _, run := range a.runningTunnels() {
			label := tr(c, "停止 ") + a.tunnelServerName(run.host) + " · " + tr(c, run.label()) + " · " + run.address
			if m.Item(label).ID(run.requestID).Disabled(run.status == tr(nil, "正在停止…")).Chosen() {
				stopTunnel(run)
			}
		}
		if count > 1 {
			m.Separator()
			if m.Item(tr(c, "停止全部服务")).ID("stop-services").Danger().Chosen() {
				for _, run := range a.runningTunnels() {
					stopTunnel(run)
				}
			}
		}
	})
}
