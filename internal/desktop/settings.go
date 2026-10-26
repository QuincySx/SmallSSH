package desktop

import (
	"github.com/egoist/mygo/ui"
	"sshclient/internal/desktop/components"
)

const (
	settingsWorkspace = "workspace"
	settingsSecurity  = "security"
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
		if previous.id == settingsSecurity && a.authCancel != nil {
			a.authCancel()
		}
	}
	a.settingsSection = next.id
}

func (a *app) settingsView(c *ui.Context) {
	category := settingsCategoryFor(a.settingsSection)
	a.settingsSection = category.id
	ui.Row(c).FillWidth().Grow(1).MinHeight(0).Gap(0).Label(tr(c, "设置页面")).Children(func() {
		selected := category.id
		compact := a.sidebarHidden
		navigationLabel := tr(c, "设置分类")
		if compact {
			navigationLabel += tr(c, "窄栏")
		}
		components.NavigationSidebar(c, "settings-category-navigation", navigationLabel, 152, compact, func() {
			for _, item := range settingsCategories {
				if components.NavigationIcon(c, "settings-category:"+item.id, tr(c, item.title), item.icon, selected == item.id, compact).
					Label(tr(c, "设置分类：") + tr(c, item.title)).Clicked() {
					selected = item.id
				}
			}
		})
		if selected != category.id {
			a.selectSettingsSection(selected)
			category = settingsCategoryFor(a.settingsSection)
		}
		// Keep the content and its input state stable when its sidebar sibling
		// changes between expanded and compact; the key must precede Scroll's construction.
		c.Key("settings-content:" + category.id)
		ui.Scroll(c).Grow(1).FillHeight().MinWidth(0).Label(tr(c, "设置内容")).Children(func() {
			ui.Column(c).FillWidth().AlignItems(ui.Center).Padding(24).Children(func() {
				ui.Column(c).FillWidth().MaxWidth(720).Gap(20).Label(tr(c, "设置内容容器")).Children(func() {
					ui.Text(c, tr(c, category.title)).FontSize(22).Bold().Label(tr(c, "设置分类标题"))
					category.render(a, c)
				})
			})
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

func settingsParameter(c *ui.Context, label string, last bool, control func()) {
	row := ui.Row(c).FillWidth().MinHeight(54).Gap(12).AlignItems(ui.Center)
	if !last {
		row.BorderWidth(0, 0, 1, 0).BorderColor(c.Theme().Border.Alpha(.55))
	}
	row.Children(func() {
		ui.Text(c, label).FontSize(13).Grow(1)
		control()
	})
}

func (a *app) workspaceSettings(c *ui.Context) {
	a.languageSettings(c)
	ui.Column(c).FillWidth().Padding(16, 16, 4, 16).Radius(8).Gap(0).
		Background(c.Theme().Surface).Label(tr(c, "工作区布局设置")).Children(func() {
		settingsParameter(c, tr(c, "连接侧栏"), false, func() {
			shown := !a.sidebarHidden
			if ui.Switch(c, &shown).Label(tr(c, "工作区侧栏设置")).Shrink(0).Changed() {
				a.sidebarHidden = !shown
			}
		})
		settingsParameter(c, tr(c, "工具面板"), false, func() {
			shown := !a.toolsHidden
			if ui.Switch(c, &shown).Label(tr(c, "工作区工具设置")).Shrink(0).Changed() {
				a.applySettingsLayout(func() { a.setToolsHidden(!shown) })
			}
		})
		settingsParameter(c, tr(c, "停靠位置"), false, func() {
			dock := 0
			if a.toolsRight {
				dock = 1
			}
			if ui.Segmented(c, &dock, tr(c, "底部"), tr(c, "右侧")).Height(32).Label(tr(c, "工作区工具停靠位置")).Changed() {
				a.applySettingsLayout(func() { a.setToolsDock(dock == 1) })
			}
		})
		settingsParameter(c, tr(c, "默认布局"), true, func() {
			if components.Button(c, tr(c, "恢复默认布局")).Clicked() {
				a.applySettingsLayout(a.resetWorkspaceLayout)
			}
		})
	})
}
