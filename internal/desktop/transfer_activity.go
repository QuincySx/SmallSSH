package desktop

import (
	"fmt"

	"github.com/egoist/mygo/ui"
	"sshclient/internal/desktop/components"
	"sshclient/internal/transfer"
)

func transferActivityCounts(jobs []transfer.Job) (active, failed int) {
	for _, job := range jobs {
		switch job.State {
		case "queued", "waiting", "running":
			active++
		case "error":
			failed++
		}
	}
	return
}

func (a *app) showTransfers() {
	a.page, a.transfersOpen = 1, true
	a.activityTab = 0
	a.setTransferWindow(a.toolsWindowFocused())
}

func (a *app) setTransferWindow(detached bool) {
	if a.transfersDetached != detached {
		rebindToolList(&a.transferTable)
		a.transfersDetached = detached
	}
}

// The queue is an explicitly opened global activity popup. Progress updates
// neither select a tool tab nor open a popup over the user's current work.
func (a *app) transferActivity(c *ui.Context) {
	active, failed := transferActivityCounts(a.jobs)
	for _, job := range a.dockerJobs {
		if job.status == "running" {
			active++
		} else if job.status == "error" {
			failed++
		}
	}
	tooltip := tr(c, "传输任务")
	label := tr(c, "传输任务")
	if len(a.dockerJobs) > 0 {
		label = tr(c, "任务中心")
		tooltip = tr(c, "传输与服务器操作")
	}
	if active > 0 {
		tooltip = fmt.Sprintf(tr(c, "%d 项正在传输"), active)
		if len(a.dockerJobs) > 0 {
			tooltip = fmt.Sprintf(tr(c, "%d 项进行中"), active)
		}
	} else if failed > 0 {
		tooltip = fmt.Sprintf(tr(c, "%d 项传输失败"), failed)
		if len(a.dockerJobs) > 0 {
			tooltip = fmt.Sprintf(tr(c, "%d 项需要检查"), failed)
		}
	}
	button := components.SVGButton(c, components.TransfersIcon, label).Tooltip(tooltip)
	if active > 0 || failed > 0 {
		count, color := active, c.Theme().Accent
		if active == 0 {
			count, color = failed, c.Theme().Danger
		}
		button.Width(52).Gap(4).TextColor(color).Children(func() {
			label := fmt.Sprint(count)
			if count > 99 {
				label = "99+"
			}
			ui.Text(c, label).FontSize(11).Bold().SingleLine()
		})
	}
	if button.Clicked() {
		if a.transfersDetached != a.renderingToolsWindow {
			a.transfersOpen = true
		} else {
			a.transfersOpen = !a.transfersOpen
		}
		a.setTransferWindow(a.renderingToolsWindow)
	}
	if a.transfersDetached != a.renderingToolsWindow {
		return
	}
	w, h := c.Size()
	height := float32(144)
	if len(a.jobs) > 0 {
		height = min(390, 68+float32(len(a.jobs))*88)
	}
	if len(a.dockerJobs) > 0 {
		height = min(390, 128+float32(len(a.dockerJobs))*84)
		if a.activityTab == 0 {
			height = max(208, min(390, 96+float32(len(a.jobs))*88))
		}
	}
	ui.Popover(c, button, &a.transfersOpen, func() {
		ui.Column(c).Width(min(480, max(0, w-48))).Height(min(height, max(0, h-80))).Padding(12).Gap(8).Label(label + tr(c, "面板")).Children(func() {
			ui.Row(c).FillWidth().Shrink(0).AlignItems(ui.Center).Gap(8).Children(func() {
				ui.Text(c, label).FontSize(15).Bold()
				if active > 0 {
					ui.Text(c, fmt.Sprintf(tr(c, "%d 项进行中"), active)).FontSize(11).TextColor(c.Theme().TextMuted)
				}
				ui.Spacer(c).Grow(1)
				if components.SVGButton(c, components.CloseIcon, fmt.Sprintf(tr(c, "关闭%s"), label)).Clicked() {
					a.transfersOpen = false
				}
			})
			if len(a.dockerJobs) > 0 {
				ui.Tabs(c, &a.activityTab, tr(c, "传输"), tr(c, "服务器操作"))
			}
			if a.activityTab == 1 && len(a.dockerJobs) > 0 {
				a.dockerJobsView(c)
			} else {
				a.transferView(c)
			}
		})
	})
}
