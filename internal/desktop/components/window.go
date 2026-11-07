package components

import "github.com/egoist/mygo/ui"

type WindowFrameOptions struct {
	Title, Target   string
	Close           func()
	CloseDisabled   bool
	Actions, Footer func()
}

// WindowFrame shares a compact header and fixed footer without owning document
// lifecycle. The caller keeps save, cancellation and unsaved-change decisions.
func WindowFrame(c *ui.Context, o WindowFrameOptions, body func()) ui.Element {
	bar := c.TitleBar()
	return ui.Column(c).Fill().Background(c.Theme().Surface).Children(func() {
		ui.Row(c).FillWidth().Height(max(48, bar.Height)).Shrink(0).
			Padding(0, max(12, bar.Right), 0, max(12, bar.Left+8)).Gap(8).AlignItems(ui.Center).DragWindow().Children(func() {
			ui.Column(c).Grow(1).MinWidth(0).Gap(3).DragWindow().Children(func() {
				ui.Text(c, o.Title).FontSize(14).Bold().SingleLine()
				if o.Target != "" {
					ui.Text(c, o.Target).FontSize(11).TextColor(c.Theme().TextMuted).SingleLine()
				}
			})
			if o.Actions != nil {
				o.Actions()
			}
			if o.Close != nil && SVGButton(c, CloseIcon, tr(c, "关闭"), "").Disabled(o.CloseDisabled).Clicked() {
				o.Close()
			}
		})
		ui.Column(c).FillWidth().Grow(1).MinHeight(0).Children(body)
		if o.Footer != nil {
			o.Footer()
		}
	})
}
