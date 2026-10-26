package components

import (
	"runtime"

	"github.com/egoist/mygo/ui"
)

type PanelLayoutAction uint8

const (
	PanelLayoutUnchanged PanelLayoutAction = iota
	PanelDockBottom
	PanelDockRight
	PanelToggleExpanded
	PanelHide
	PanelDetach
)

// PanelLayoutMenu follows the DevTools dock selector: visual placement choices
// live in the overflow popup, separate from the persistent close action.
func PanelLayoutMenu(c *ui.Context, open *bool, right, expanded bool, floating ...bool) PanelLayoutAction {
	detached := len(floating) > 0 && floating[0]
	action := PanelLayoutUnchanged
	button := SVGButton(c, MoreIcon, tr(c, "工具面板布局")).Tooltip(tr(c, "切换停靠位置与面板大小"))
	if button.Clicked() {
		*open = !*open
	}
	ui.PopoverBase(c, button, open, func(panel ui.Element) {
		panel.AttachTo(button, ui.AnchorBottomEnd, ui.AnchorTopEnd).Margin(4, 0, 0, 0).
			Width(244).Padding(4, 0).Radius(8).Background(c.Theme().Background).
			Border(1, c.Theme().Border).Shadow(0, 6, 20, 0, ui.RGBA(0, 0, 0, .18)).
			Label(tr(c, "工具面板布局菜单"))
		ui.Column(c).FillWidth().Gap(0).Children(func() {
			var dock, currentDock ui.Element
			ui.Row(c).FillWidth().Padding(4, 10, 6, 12).Gap(8).AlignItems(ui.Center).Label(tr(c, "停靠选择行")).Children(func() {
				ui.Text(c, tr(c, "停靠侧")).FontSize(12).Grow(1)
				dock = ButtonGroup(c, func() {
					for _, item := range []struct {
						accessible string
						icon       *ui.SVG
						active     bool
						action     PanelLayoutAction
					}{{tr(c, "独立窗口"), DetachPanelIcon, detached, PanelDetach}, {tr(c, "放到底部"), BottomPanelIcon, !right && !detached, PanelDockBottom}, {tr(c, "放到右侧"), RightPanelIcon, right && !detached, PanelDockRight}} {
						choice := SVGButton(c, item.icon, item.accessible).Size(32, 32).Radius(4).
							Role(ui.RoleRadio).Checked(item.active)
						if item.active {
							choice.Background(c.Theme().Accent.Alpha(.1)).TextColor(c.Theme().Accent).AutoFocus()
							currentDock = choice
						}
						if choice.Clicked() {
							action = item.action
							*open = false
						}
					}
				}).Padding(0).Gap(4).Shrink(0).Role(ui.RoleRadioGroup).Label(tr(c, "停靠位置"))
			})
			ui.Box(c).FillWidth().Height(1).Margin(0, 0, 4, 0).Background(c.Theme().Border.Alpha(.65))
			label := tr(c, "放大工具面板")
			icon := ExpandPanelIcon
			if expanded {
				label = tr(c, "恢复分栏")
				icon = RestorePanelIcon
			}
			var resize ui.Element
			if !detached {
				resize = panelMenuItem(c, icon, label, "")
				if resize.Clicked() {
					action = PanelToggleExpanded
					*open = false
				}
			}
			shortcut := "Ctrl+Shift+J"
			if runtime.GOOS == "darwin" {
				shortcut = "⌘⇧J"
			}
			hide := panelMenuItem(c, CloseIcon, tr(c, "收起面板"), shortcut)
			if hide.Clicked() {
				action = PanelHide
				*open = false
			}
			// Horizontal arrows stay in the dock row; vertical arrows traverse
			// the menu rows. Native buttons retain Enter, Space and Tab.
			if detached {
				if dock.Shortcut(0, ui.KeyDown) || dock.Shortcut(0, ui.KeyUp) {
					hide.Focus()
				} else if hide.Shortcut(0, ui.KeyDown) || hide.Shortcut(0, ui.KeyUp) {
					currentDock.Focus()
				}
			} else {
				switch {
				case dock.Shortcut(0, ui.KeyDown), hide.Shortcut(0, ui.KeyUp):
					resize.Focus()
				case dock.Shortcut(0, ui.KeyUp), resize.Shortcut(0, ui.KeyDown):
					hide.Focus()
				case resize.Shortcut(0, ui.KeyUp), hide.Shortcut(0, ui.KeyDown):
					currentDock.Focus()
				}
			}
		})
		if panel.Shortcut(0, ui.KeyEscape) {
			*open = false
			button.Focus()
		}
	})
	return action
}

func panelMenuItem(c *ui.Context, icon *ui.SVG, label, shortcut string) ui.Element {
	return Button(c, "", ButtonOptions{Variant: Quiet}).FillWidth().Height(34).Radius(0).
		Padding(6, 12).Gap(10).AlignItems(ui.Center).Label(label).Role(ui.RoleMenuItem).Children(func() {
		ui.Icon(c, icon).Size(16, 16)
		ui.Text(c, label).FontSize(12).SingleLine().Grow(1)
		if shortcut != "" {
			ui.Text(c, shortcut).FontSize(11).SingleLine().TextColor(c.Theme().TextMuted)
		}
	})
}
