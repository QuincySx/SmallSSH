package desktop

import (
	"context"
	"fmt"
	"slices"
	"strings"
	"time"

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
	a.activityHistory = [2]int{}
	if tool := a.workspaceTools[toolTransfers]; tool != nil {
		a.activateWorkspaceTab(tool.group, workspaceTab{tool: tool})
		a.page, a.transfersOpen = 1, false
		a.invalidateWindows()
		return
	}
	a.page, a.transfersOpen = 1, true
	a.activityTab = 0
	a.setTransferWindow(a.toolsWindowFocused())
}

func (a *app) setTransferWindow(detached bool) {
	if a.transfersDetached != detached {
		rebindToolList(&a.transferTable)
		rebindToolList(&a.activityTable)
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
	label, tooltip := tr(c, "任务中心"), tr(c, "传输与服务器操作")
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
		if a.workspaceTools[toolTransfers] != nil {
			a.showTransfers()
			return
		}
		if a.transfersDetached != a.renderingToolsWindow {
			a.transfersOpen = true
		} else {
			a.transfersOpen = !a.transfersOpen
		}
		a.setTransferWindow(a.renderingToolsWindow)
		if a.transfersOpen {
			a.activityHistory = [2]int{}
		}
	}
	if a.workspaceTools[toolTransfers] != nil {
		a.transfersOpen = false
		return
	}
	if a.transfersDetached != a.renderingToolsWindow {
		return
	}
	w, h := c.Size()
	rows := a.activityVisibleCount()
	height := max(float32(260), min(480, 186+float32(rows)*88))
	ui.Popover(c, button, &a.transfersOpen, func() {
		ui.Column(c).Width(min(480, max(0, w-48))).Height(min(height, max(0, h-80))).Padding(12).Gap(8).Label(label + tr(c, "面板")).Children(func() {
			ui.Row(c).FillWidth().Shrink(0).AlignItems(ui.Center).Gap(8).Children(func() {
				ui.Text(c, label).FontSize(15).Bold()
				if summary := a.activitySummary(c); summary != "" {
					ui.Text(c, summary).SingleLine().Shrink(1).MinWidth(0).FontSize(11).TextColor(c.Theme().TextMuted)
				}
				ui.Spacer(c).Grow(1)
				if components.SVGButton(c, components.CloseIcon, fmt.Sprintf(tr(c, "关闭%s"), label)).Tooltip(tr(c, "关闭面板不中断任务")).Clicked() {
					a.transfersOpen = false
				}
			})
			a.activityContent(c, &a.transferTable)
		})
	})
}

func (a *app) activityVisibleCount() int {
	n := 0
	history := a.activityHistory[a.activityTab] == 1
	if a.activityTab == 1 {
		for _, job := range a.dockerJobs {
			if (job.status == "done") == history {
				n++
			}
		}
	} else {
		for _, job := range a.jobs {
			if (job.State == "done") == history {
				n++
			}
		}
	}
	return n
}

// Both the global popup and restored legacy task cards use the same filters.
func (a *app) activityContent(c *ui.Context, state *ui.ListState) {
	components.Tabs(c, &a.activityTab, tr(c, "传输"), tr(c, "服务器操作"))
	ui.Row(c).FillWidth().Shrink(0).AlignItems(ui.Center).Gap(8).Children(func() {
		components.Tabs(c, &a.activityHistory[a.activityTab], tr(c, "当前任务"), tr(c, "历史记录"))
		ui.Spacer(c).Grow(1)
		if a.activityHistory[a.activityTab] == 0 && a.activityFailedCount() > 0 {
			tip := tr(c, "移除全部失败任务，并删除本机未完成的部分文件")
			if a.activityTab == 1 {
				tip = tr(c, "移除全部失败记录，不影响服务和审计")
			}
			if components.Button(c, tr(c, "清除失败")).Tooltip(tip).Disabled(a.transferHistoryBusy).Clicked() {
				if a.activityTab == 0 {
					a.transferMaintenance(func(m *transfer.Manager) error { return m.ClearFailed() })
				} else {
					a.dockerJobs = slices.DeleteFunc(a.dockerJobs, func(j *dockerJob) bool { return j.status == "error" })
				}
			}
		}
		if a.activityHistory[a.activityTab] == 1 {
			tip := tr(c, "只移除记录，不删除文件")
			if a.activityTab == 1 {
				tip = tr(c, "清理记录不影响服务和审计")
			}
			if components.Button(c, tr(c, "清空记录")).Tooltip(tip).Disabled(a.activityVisibleCount() == 0 || a.transferHistoryBusy).Clicked() {
				if a.activityTab == 0 {
					a.removeTransferHistory("")
				} else {
					a.dockerJobs = slices.DeleteFunc(a.dockerJobs, func(j *dockerJob) bool { return j.status == "done" })
				}
			}
		}
	})
	if a.activityTab == 1 {
		a.dockerJobsView(c)
	} else {
		a.transferViewWithState(c, state)
	}
	if a.transferHistoryError != "" && a.activityTab == 0 {
		ui.Text(c, reviewText(a.transferHistoryError)).FillWidth().FontSize(11).TextColor(c.Theme().Danger)
	}
}

func (a *app) removeTransferHistory(id string) {
	a.transferMaintenance(func(m *transfer.Manager) error {
		if id == "" {
			return m.ClearCompleted()
		}
		return m.RemoveCompleted(id)
	})
}

// transferMaintenance runs one record operation off the UI thread and then
// reloads the queue. The generation check drops results from a locked vault.
func (a *app) transferMaintenance(run func(*transfer.Manager) error) {
	if a.transferHistoryBusy || a.transfers == nil {
		return
	}
	a.transferHistoryBusy, a.transferHistoryError = true, ""
	manager, gen := a.transfers, a.generation
	go func() {
		err := run(manager)
		a.postUI(func() {
			if a.generation != gen || a.transfers != manager {
				return
			}
			a.transferHistoryBusy = false
			if err != nil {
				a.transferHistoryError = err.Error()
			}
			a.transferJobsVersion.Add(1)
			a.jobs = manager.List()
		})
	}()
}

// cancelTransfer stops a job wherever it runs and discards it together with
// its local partial data; a paused or failed job takes the same path without
// waiting. Several rows may cancel at once, each tracked by its own ID.
func (a *app) cancelTransfer(id string) {
	if a.transfers == nil || a.transferCancelling[id] {
		return
	}
	if a.transferCancelling == nil {
		a.transferCancelling = map[string]bool{}
	}
	a.transferCancelling[id], a.transferHistoryError = true, ""
	manager, gen := a.transfers, a.generation
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		err := manager.Cancel(ctx, id)
		a.postUI(func() {
			if a.generation != gen || a.transfers != manager {
				return
			}
			delete(a.transferCancelling, id)
			if err != nil {
				a.transferHistoryError = err.Error()
			}
			a.transferJobsVersion.Add(1)
			a.jobs = manager.List()
		})
	}()
}

// transferRowActions puts each state's next step on its row: running pauses,
// paused or queued continues, failed retries, and every unfinished job can be
// cancelled or removed with ×. Finished jobs only leave the history.
func (a *app) transferRowActions(c *ui.Context, j transfer.Job, history, cancelling bool) {
	if history {
		if components.SVGButton(c, components.CloseIcon, tr(c, "移除记录 ")+j.ID).Tooltip(tr(c, "只移除记录，不删除文件")).Disabled(a.transferHistoryBusy || a.transfers == nil).Clicked() {
			a.removeTransferHistory(j.ID)
		}
		return
	}
	busy := cancelling || a.transfers == nil
	switch j.State {
	case "running", "waiting":
		if components.Button(c, tr(c, "暂停")).Disabled(busy).Clicked() {
			a.transfers.Pause(j.ID)
		}
	case "error":
		if components.Button(c, tr(c, "重试")).Disabled(busy).Clicked() {
			a.runTransfer(j.ID)
		}
	default:
		if components.Button(c, tr(c, "继续")).Disabled(busy).Clicked() {
			a.runTransfer(j.ID)
		}
	}
	label, tip := tr(c, "取消传输 ")+j.ID, tr(c, "停止并移除任务，删除本机未完成的部分文件")
	if j.State == "error" {
		label, tip = tr(c, "移除失败任务 ")+j.ID, tr(c, "移除任务，删除本机未完成的部分文件")
	}
	if j.Direction == "upload" {
		tip = tr(c, "停止并移除任务；服务器上未完成的临时文件不会自动删除")
		if j.State == "error" {
			tip = tr(c, "移除任务；服务器上未完成的临时文件不会自动删除")
		}
	}
	if components.SVGButton(c, components.CloseIcon, label, tip).Disabled(busy).Clicked() {
		a.cancelTransfer(j.ID)
	}
}

// activitySummary counts both tabs, so the header agrees with the badge.
func (a *app) activitySummary(c *ui.Context) string {
	var active, paused, failed int
	for _, job := range a.jobs {
		switch job.State {
		case "waiting", "running":
			active++
		case "queued", "paused":
			paused++
		case "error":
			failed++
		}
	}
	for _, job := range a.dockerJobs {
		switch job.status {
		case "running":
			active++
		case "error":
			failed++
		}
	}
	var parts []string
	if active > 0 {
		parts = append(parts, fmt.Sprintf(tr(c, "%d 项进行中"), active))
	}
	if paused > 0 {
		parts = append(parts, fmt.Sprintf(tr(c, "%d 项已暂停"), paused))
	}
	if failed > 0 {
		parts = append(parts, fmt.Sprintf(tr(c, "%d 项失败"), failed))
	}
	return strings.Join(parts, " · ")
}

func (a *app) activityFailedCount() int {
	n := 0
	if a.activityTab == 1 {
		for _, job := range a.dockerJobs {
			if job.status == "error" {
				n++
			}
		}
		return n
	}
	for _, job := range a.jobs {
		if job.State == "error" {
			n++
		}
	}
	return n
}
