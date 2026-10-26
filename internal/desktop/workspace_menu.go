package desktop

import (
	"github.com/egoist/mygo/ui"
	"sshclient/internal/desktop/components"
)

func (a *app) workspaceNavigationPage() int {
	page := a.page
	if page == 0 {
		page = a.connection.returnPage
	}
	if page != 3 && page != 4 {
		page = 1
	}
	return page
}

// Primary pages share navigation, not the workbench's connection sidebar.
func (a *app) workspaceHeader(c *ui.Context) {
	components.HeaderBar(c).Key("workspace-header").Label(tr(c, "全局导航栏")).Children(func() {
		a.sidebarToggle(c)
		selected := 0
		page := a.workspaceNavigationPage()
		items := []struct {
			label string
			page  int
		}{{tr(c, "工作台"), 1}, {tr(c, "授权中心"), 3}, {tr(c, "设置"), 4}}
		for i, item := range items {
			if item.page == page {
				selected = i
			}
		}
		clicked := -1
		c.Key("primary-navigation")
		tabs := ui.TabsBase(c, &selected, len(items))
		tabs.List.Height(components.HeaderHeight).Shrink(0).Gap(0).AlignItems(ui.Center).Label(tr(c, "功能导航")).Children(func() {
			for i, item := range items {
				tab := tabs.Tab(i).Height(components.HeaderHeight).Padding(0, 10).Label(item.label).
					TextColor(c.Theme().TextMuted).BorderWidth(0, 0, 2, 0).BorderColor(ui.Transparent)
				if item.page == page {
					tab.TextColor(c.Theme().Accent).Background(c.Theme().Accent.Alpha(.07)).BorderColor(c.Theme().Accent)
				} else if tab.Hovered() {
					tab.TextColor(c.Theme().Text).Background(c.Theme().SurfaceHover)
				}
				tab.Children(func() { components.HeaderLabel(c, item.label).FontSize(12) })
				if tab.Clicked() {
					clicked = i
				}
			}
		})
		if clicked >= 0 {
			selected = clicked
		}
		if (clicked >= 0 || tabs.List.Changed()) && selected >= 0 && selected < len(items) {
			a.navigatePrimaryPage(items[selected].page)
		}
		ui.Spacer(c).Grow(1).MinWidth(0)
		a.transferActivity(c)
		if page == 1 {
			a.toolsToggle(c)
		}
		c.Key("workspace-lock")
		if components.SVGButton(c, components.LockIcon, tr(c, "锁定")).Tooltip(tr(c, "锁定保险库 · Cmd/Ctrl+Shift+L")).Clicked() {
			a.runMenu(a.confirmLock)
		}
	})
}

func (a *app) navigatePrimaryPage(page int) {
	if page != 1 && page != 3 && page != 4 {
		return
	}
	a.runMenu(func() {
		a.page = page
		a.contentFocus = page == 1 && !a.toolsDetached && !a.fileExpanded && !a.prompt.open
	})
}
