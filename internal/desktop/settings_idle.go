package desktop

import (
	"fmt"

	"github.com/egoist/mygo/ui"
)

func (a *app) idleSettings(c *ui.Context) {
	for _, session := range []bool{false, true} {
		title, explanation, switchLabel, selectLabel := tr(c, "保险库闲置自动锁定"), tr(c, "关闭后不会因电脑闲置自动锁定；手动锁定、系统锁屏和休眠仍会结束访问。"), tr(c, "保险库闲置自动锁定开关"), tr(c, "保险库闲置等待时间")
		minutes := []int{5, 10, 15, 20, 30, 60}
		enabled := behaviorEnabled(a.behavior.IdleLockEnabled)
		selectedMinutes := behaviorMinutes(a.behavior.IdleLockMinutes, false)
		if session {
			title, explanation, switchLabel, selectLabel = tr(c, "新 CLI 会话的默认闲置断开"), tr(c, "用于之后的会话审批；已有会话按批准时的期限执行。CLI 提交的是建议，最终由你确认。"), tr(c, "新 CLI 会话闲置断开开关"), tr(c, "新 CLI 会话默认时长")
			minutes = []int{5, 10, 15, 20, 30}
			enabled = behaviorEnabled(a.behavior.SessionIdleEnabled)
			selectedMinutes = behaviorMinutes(a.behavior.SessionIdleMinutes, true)
		}
		labels := make([]string, len(minutes))
		selected := ""
		for i, minute := range minutes {
			labels[i] = fmt.Sprintf(tr(c, "%d 分钟"), minute)
			if minute == selectedMinutes {
				selected = labels[i]
			}
		}
		ui.Column(c).FillWidth().Padding(16).Radius(8).Gap(6).Background(c.Theme().Surface).Label(title).Children(func() {
			settingsParameter(c, title, func() {
				if ui.Switch(c, &enabled).Label(switchLabel).Shrink(0).Changed() {
					next := a.behavior
					if session {
						next.SessionIdleEnabled = &enabled
					} else {
						next.IdleLockEnabled = &enabled
					}
					a.saveIdleSettings(c, next)
				}
			})
			ui.Text(c, explanation).FillWidth().FontSize(12).TextColor(c.Theme().TextMuted)
			settingsParameter(c, tr(c, "等待时间"), func() {
				if ui.Select(c, &selected, labels).Width(140).MinWidth(0).Label(selectLabel).Disabled(!enabled).Changed() {
					for i, label := range labels {
						if selected == label {
							next := a.behavior
							if session {
								next.SessionIdleMinutes = minutes[i]
							} else {
								next.IdleLockMinutes = minutes[i]
							}
							a.saveIdleSettings(c, next)
							break
						}
					}
				}
			})
		})
	}
}

func (a *app) saveIdleSettings(c *ui.Context, next behaviorPreferences) {
	if err := a.applyBehaviorPreferences(next); err != nil {
		a.status = fmt.Sprintf(tr(c, "无法保存闲置设置：%s"), err)
	} else {
		a.status = ""
	}
}
