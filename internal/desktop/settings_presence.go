package desktop

import (
	"fmt"

	"github.com/egoist/mygo/ui"
	"sshclient/internal/desktop/components"
)

func (a *app) presenceSettings(c *ui.Context) {
	labels := []string{tr(c, "关闭窗口时退出"), tr(c, "关闭窗口后驻留")}
	selected := labels[0]
	if a.behavior.BackgroundMode == "application" {
		selected = labels[1]
	}
	ui.Column(c).FillWidth().Padding(16).Radius(8).Gap(8).Background(c.Theme().Surface).Children(func() {
		settingsParameter(c, tr(c, "主窗口关闭行为"), func() {
			if ui.Select(c, &selected, labels).Width(180).MinWidth(0).Label(tr(c, "后台运行方式选择")).Changed() {
				next := a.behavior
				next.BackgroundMode = "off"
				if selected == labels[1] {
					next.BackgroundMode = "application"
				}
				a.savePresenceSetting(c, next)
			}
		})
		ui.Text(c, tr(c, "驻留隐藏相关窗口；退出会锁定连接。")).FillWidth().FontSize(12).TextColor(c.Theme().TextMuted)
	})
	if e := a.presence.effects; e != nil {
		ui.Column(c).FillWidth().Padding(16).Radius(8).Gap(0).Background(c.Theme().Surface).Children(func() {
			if e.dockSupported {
				settingsParameter(c, tr(c, "驻留时显示 Dock"), func() {
					show := behaviorEnabled(a.behavior.ShowDockIcon)
					if ui.Switch(c, &show).Label(tr(c, "Dock 图标开关")).Shrink(0).Changed() {
						next := a.behavior
						next.ShowDockIcon = &show
						a.savePresenceSetting(c, next)
					}
				})
				ui.Text(c, tr(c, "窗口打开或最小化时显示 Dock。")).FillWidth().FontSize(12).TextColor(c.Theme().TextMuted)
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
	// Only this page's own save failure belongs here; the app-wide status
	// (transfer done, terminal hints…) used to leak in as a red error.
	if a.presence.saveError != "" {
		ui.Row(c).FillWidth().Gap(8).AlignItems(ui.Start).Label(tr(c, "后台设置反馈")).Children(func() {
			ui.Text(c, reviewText(a.presence.saveError)).Grow(1).MinWidth(0).FontSize(12).TextColor(c.Theme().Danger)
			if components.IconButton(c, "×", tr(c, "关闭后台设置提示")).Size(20, 20).Clicked() {
				a.presence.saveError = ""
			}
		})
	}
}
func (a *app) savePresenceSetting(c *ui.Context, next behaviorPreferences) {
	if err := a.applyPresence(next); err != nil {
		a.presence.saveError = fmt.Sprintf(tr(c, "无法保存后台设置：%s"), err)
	} else {
		a.presence.saveError = ""
	}
}
