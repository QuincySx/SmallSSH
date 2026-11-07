package desktop

import (
	"github.com/egoist/mygo/ui"
	"sshclient/internal/desktop/components"
)

const (
	settingsWorkspace = "workspace"
	settingsSecurity  = "security"
	settingsPresence  = "presence"
	settingsApprovals = "approvals"
)

type settingsCategory struct {
	id, title string
	icon      *ui.SVG
	render    func(*app, *ui.Context)
}

// A category owns its navigation entry, title and content in one place.
var settingsCategories = []settingsCategory{
	{settingsWorkspace, "工作区", components.WorkspaceIcon, (*app).workspaceSettings},
	{settingsSecurity, "安全与解锁", components.ShieldIcon, (*app).securityView},
	{settingsPresence, "运行与后台", components.WorkspaceIcon, (*app).presenceSettings},
	{settingsApprovals, "授权中心", components.ShieldIcon, (*app).approvalView},
}

func settingsCategoryFor(id string) settingsCategory {
	for _, category := range settingsCategories {
		if category.id == id {
			return category
		}
	}
	return settingsCategories[0]
}

func (a *app) selectSettingsSection(id string) {
	previous := settingsCategoryFor(a.settingsSection)
	next := settingsCategoryFor(id)
	if previous.id != next.id {
		a.ownerPass = ""
		a.clearInlineApproval()
		if previous.id == settingsSecurity && a.authCancel != nil {
			a.authCancel()
		}
	}
	a.settingsSection = next.id
}

func (a *app) settingsView(c *ui.Context) {
	category := settingsCategoryFor(a.settingsSection)
	a.settingsSection = category.id
	bar := c.TitleBar()
	headerHeight := max(float32(32), bar.Height-16)
	ui.Row(c).Fill().Padding(8).Gap(8).Background(c.Theme().Background).Label(tr(c, "设置页面")).Children(func() {
		ui.Column(c).Width(200).FillHeight().Shrink(0).Padding(0, 12, 12, 12).Gap(4).Radius(12).Background(c.Theme().Surface).Children(func() {
			ui.Row(c).FillWidth().Height(headerHeight).Padding(0, 0, 0, max(0, bar.Left-12)).AlignItems(ui.Center).DragWindow().Children(func() {
				ui.Text(c, tr(c, "设置")).FontSize(14).Bold()
				ui.Spacer(c).Grow(1).Height(32).DragWindow()
			})
			ui.Column(c).FillWidth().Grow(1).MinHeight(0).FocusGroup(ui.Vertical).Label(tr(c, "设置分类")).Children(func() {
				for _, item := range settingsCategories {
					entry := components.Button(c, tr(c, item.title), components.ButtonOptions{Variant: components.Quiet}).FillWidth().Justify(ui.Start).Label(tr(c, "设置分类：") + tr(c, item.title))
					if item.id == category.id {
						entry.Background(c.Theme().Accent.Alpha(.12)).TextColor(c.Theme().Accent)
					}
					if entry.Clicked() {
						a.selectSettingsSection(item.id)
						category = settingsCategoryFor(a.settingsSection)
					}
				}
			})
		})
		ui.Column(c).Grow(1).FillHeight().MinWidth(0).MinHeight(0).Padding(0, 12, 12, 12).Gap(8).Radius(12).Background(c.Theme().Surface).Children(func() {
			ui.Row(c).FillWidth().Height(headerHeight).AlignItems(ui.Center).DragWindow().Children(func() {
				ui.Text(c, tr(c, category.title)).FontSize(14).Bold().Grow(1).DragWindow().Label(tr(c, "设置分类标题"))
				ui.Spacer(c).Width(40).Shrink(0)
			})
			c.Key("settings-content:" + category.id)
			if category.id == settingsApprovals {
				category.render(a, c)
			} else {
				ui.Scroll(c).FillWidth().Grow(1).MinHeight(0).Label(tr(c, "设置内容")).Children(func() {
					ui.Column(c).FillWidth().AlignItems(ui.Center).Children(func() {
						ui.Column(c).FillWidth().MaxWidth(720).Gap(12).Label(tr(c, "设置内容容器")).Children(func() { category.render(a, c) })
					})
				})
			}
			if components.SVGButton(c, components.CloseIcon, tr(c, "关闭设置"), "").Absolute().Top((headerHeight - 32) / 2).End(12).Clicked() {
				a.closeSettingsWindow()
			}
		})
	})
}

// Layout actions may request terminal focus in the workbench. Changing a
// preference while settings is visible must keep the current keyboard focus.
func (a *app) applySettingsLayout(change func()) {
	focus := a.contentFocus
	change()
	a.contentFocus = focus
}

func settingsParameter(c *ui.Context, label string, control func()) {
	row := ui.Row(c).FillWidth().MinHeight(54).Gap(12).AlignItems(ui.Center)
	row.Children(func() {
		ui.Text(c, label).FontSize(13).Grow(1)
		control()
	})
}

func (a *app) workspaceSettings(c *ui.Context) {
	a.languageSettings(c)
	ui.Column(c).FillWidth().Padding(16, 16, 4, 16).Radius(8).Gap(0).
		Background(c.Theme().Surface).Label(tr(c, "工作区布局设置")).Children(func() {
		settingsParameter(c, tr(c, "连接侧栏"), func() {
			shown := !a.sidebarHidden
			if ui.Switch(c, &shown).Label(tr(c, "工作区侧栏设置")).Shrink(0).Changed() {
				a.sidebarHidden = !shown
			}
		})
		settingsParameter(c, tr(c, "工具面板"), func() {
			shown := !a.toolsHidden
			if ui.Switch(c, &shown).Label(tr(c, "工作区工具设置")).Shrink(0).Changed() {
				a.applySettingsLayout(func() { a.setToolsHidden(!shown) })
			}
		})
		settingsParameter(c, tr(c, "停靠位置"), func() {
			dock := 0
			sides := []string{"bottom", "right", "top", "left"}
			for i, side := range sides {
				if side == a.savedToolsDockSide() {
					dock = i
				}
			}
			if ui.Segmented(c, &dock, tr(c, "底部"), tr(c, "右侧"), tr(c, "顶部"), tr(c, "左侧")).Height(32).Label(tr(c, "工作区工具停靠位置")).Changed() {
				a.applySettingsLayout(func() { a.setToolsDockSide(sides[dock]) })
			}
		})
		settingsParameter(c, tr(c, "默认布局"), func() {
			if components.Button(c, tr(c, "恢复默认布局")).Clicked() {
				a.applySettingsLayout(a.resetWorkspaceLayout)
			}
		})
	})
}
