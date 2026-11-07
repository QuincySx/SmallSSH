package desktop

import (
	"fmt"
	"github.com/egoist/mygo/ui"
	"sshclient/internal/desktop/components"
)

// Transient notices overlay content instead of reserving a permanent bottom bar.
func (a *app) workspaceStatusNotice(c *ui.Context) {
	pending := 0
	for _, r := range a.requests {
		if r.Status == "pending" && !r.OwnerIssued {
			pending++
		}
	}
	if a.status == "" && pending == 0 {
		return
	}
	width, _ := c.Size()
	ui.Row(c).Absolute().Bottom(16).End(16).MaxWidth(max(0, width-32)).Padding(8, 12).Gap(8).AlignItems(ui.Center).
		Radius(8).Background(c.Theme().Surface).Border(1, c.Theme().Border).Shadow(0, 4, 16, 0, ui.RGBA(0, 0, 0, .12)).Children(func() {
		if a.status != "" {
			ui.Text(c, reviewText(a.status)).SingleLine().MinWidth(0).FontSize(11).Tooltip(reviewText(a.status))
		}
		if pending > 0 && components.Button(c, fmt.Sprintf(tr(c, "待授权 %d"), pending)).Clicked() {
			a.openSettingsWindow(settingsApprovals)
		}
		if a.status != "" && components.SVGButton(c, components.CloseIcon, tr(c, "关闭提示")).Clicked() {
			a.status = ""
		}
	})
}
