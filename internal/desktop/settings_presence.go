package desktop

import (
	"fmt"

	"github.com/egoist/mygo/ui"
)

func (a *app) presenceSettings(c *ui.Context) {
	labels := []string{tr(c, "关闭窗口时退出"), tr(c, "关闭窗口后驻留")}
	selected := labels[0]
	if a.behavior.BackgroundMode == "application" {
		selected = labels[1]
	}
	ui.Column(c).FillWidth().Padding(16).Radius(8).Gap(8).Background(c.Theme().Surface).Children(func() {
		settingsParameter(c, tr(c, "后台运行方式"), func() {
			if ui.Select(c, &selected, labels).Width(180).MinWidth(0).Label(tr(c, "后台运行方式选择")).Changed() {
				next := a.behavior
				next.BackgroundMode = "off"
				if selected == labels[1] {
					next.BackgroundMode = "application"
				}
				a.savePresenceSetting(c, next)
			}
		})
		ui.Text(c, tr(c, "驻留仍由本应用进程提供；彻底退出后授权和任务会结束。")).FillWidth().FontSize(12).TextColor(c.Theme().TextMuted)
		ui.Text(c, tr(c, "独立安全后台尚未接入。")).FillWidth().FontSize(12).TextColor(c.Theme().TextMuted)
	})
	if e := a.presence.effects; e != nil {
		ui.Column(c).FillWidth().Padding(16).Radius(8).Gap(0).Background(c.Theme().Surface).Children(func() {
			if e.dockSupported {
				settingsParameter(c, tr(c, "Dock 图标"), func() {
					show := behaviorEnabled(a.behavior.ShowDockIcon)
					if ui.Switch(c, &show).Label(tr(c, "Dock 图标开关")).Shrink(0).Changed() {
						next := a.behavior
						next.ShowDockIcon = &show
						a.savePresenceSetting(c, next)
					}
				})
			}
			if e.traySupported {
				trayLabel, switchLabel := tr(c, "菜单栏图标"), tr(c, "菜单栏图标开关")
				if !e.dockSupported {
					trayLabel, switchLabel = tr(c, "通知区域图标"), tr(c, "通知区域图标开关")
				}
				settingsParameter(c, trayLabel, func() {
					show := a.behavior.ShowTrayIcon
					if ui.Switch(c, &show).Label(switchLabel).Shrink(0).Changed() {
						next := a.behavior
						next.ShowTrayIcon = show
						a.savePresenceSetting(c, next)
					}
				})
			}
		})
	}
	if a.presence.warning != "" {
		ui.Column(c).FillWidth().Label(tr(c, "后台入口警告")).Children(func() {
			ui.Text(c, reviewText(a.presence.warning)).FillWidth().FontSize(12).TextColor(c.Theme().Danger)
		})
	}
	if a.status != "" {
		ui.Column(c).FillWidth().Label(tr(c, "后台设置反馈")).Children(func() {
			ui.Text(c, reviewText(a.status)).FillWidth().FontSize(12).TextColor(c.Theme().Danger)
		})
	}
}
func (a *app) savePresenceSetting(c *ui.Context, next behaviorPreferences) {
	if err := a.applyPresence(next); err != nil {
		a.status = fmt.Sprintf(tr(c, "无法保存后台设置：%s"), err)
	} else {
		a.status = ""
	}
}
