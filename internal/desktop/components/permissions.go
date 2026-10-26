package components

import (
	"fmt"

	"github.com/egoist/mygo/ui"
)

func PermissionChecks(mode uint32) [9]bool {
	var checks [9]bool
	for i := range checks {
		checks[i] = mode&(1<<uint(8-i)) != 0
	}
	return checks
}

func PermissionMode(checks [9]bool) uint32 {
	var mode uint32
	for i, on := range checks {
		if on {
			mode |= 1 << uint(8-i)
		}
	}
	return mode
}

// PermissionMatrix is passive presentation; it never starts a file operation.
func PermissionMatrix(c *ui.Context, checks *[9]bool, disabled bool) ui.Element {
	return ui.Column(c).FillWidth().Gap(0).Label(tr(c, "权限勾选面板")).Children(func() {
		ui.Row(c).FillWidth().Height(30).AlignItems(ui.Center).Children(func() {
			ui.Text(c, tr(c, "访问权限")).Width(100).FontSize(BodyFont).Bold()
			for _, title := range []string{tr(c, "读"), tr(c, "写"), tr(c, "执行")} {
				ui.Text(c, title).Grow(1).TextAlign(ui.Center).FontSize(BodyFont).TextColor(c.Theme().TextMuted)
			}
		})
		for row, title := range []string{tr(c, "所有者"), tr(c, "用户组"), tr(c, "其他用户")} {
			ui.Row(c).FillWidth().Height(36).AlignItems(ui.Center).Children(func() {
				ui.Text(c, title).Width(100).FontSize(BodyFont)
				for col, name := range []string{tr(c, "读"), tr(c, "写"), tr(c, "执行")} {
					ui.Row(c).Grow(1).Justify(ui.Center).Children(func() {
						ui.Checkbox(c, &checks[row*3+col], "").Label(title + " · " + name).Disabled(disabled)
					})
				}
			})
		}
		ui.Text(c, fmt.Sprintf(tr(c, "权限值  %03o"), PermissionMode(*checks))).FontSize(DetailFont).TextColor(c.Theme().TextMuted).Padding(6, 0)
	})
}
