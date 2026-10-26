package components

import (
	"fmt"
	"github.com/egoist/mygo/ui"
)

type DialogOptions struct {
	Title, Target, Description string
	Width, Height              float32
	Close                      func()
	CloseDisabled              bool
}

// Dialog owns presentation and MyGo's focus trap/Escape behavior. Validation,
// cancellation, secret lifetime and permission checks belong to the caller.
// It never implicitly accepts an action or closes after a failed validation.
func Dialog(c *ui.Context, open *bool, o DialogOptions, body, actions func()) ui.Element {
	width := o.Width
	if width <= 0 {
		width = 520
	}
	w, h := c.Size()
	return ui.DialogBase(c, open, func(backdrop, panel ui.Element) {
		backdrop.Background(ui.RGBA(0, 0, 0, .4))
		panel.Width(min(width, max(0, w-48))).MaxHeight(max(0, h-48)).Padding(24).Gap(18).
			Radius(12).Background(c.Theme().Background).Border(1, c.Theme().Border).
			Shadow(0, 10, 30, 0, ui.RGBA(0, 0, 0, .3)).Label(o.Title + tr(c, "对话框"))
		if o.Height > 0 {
			panel.Height(min(o.Height, max(0, h-48)))
		}
		ui.Row(c).FillWidth().Shrink(0).AlignItems(ui.Center).Children(func() {
			ui.Text(c, o.Title).FontSize(21).Bold().Grow(1)
			if o.Close != nil {
				// A focus-triggered tooltip consumes Escape before the dialog
				// can run its unsaved-change guard. The close icon has a label.
				if Button(c, "×", ButtonOptions{Variant: Quiet}).Size(IconSize, IconSize).Padding(0).FontSize(16).
					Label(fmt.Sprintf(tr(c, "关闭%s"), o.Title)).Disabled(o.CloseDisabled).Clicked() {
					o.Close()
				}
			}
		})
		if o.Target != "" {
			ui.Text(c, o.Target).FillWidth().Selectable().FontSize(13).TextColor(c.Theme().TextMuted)
		}
		if o.Description != "" {
			ui.Text(c, o.Description).FillWidth().FontSize(13)
		}
		if body != nil {
			if o.Height > 0 {
				ui.Scroll(c).FillWidth().Grow(1).MinHeight(0).Children(func() {
					ui.Column(c).FillWidth().Padding(0, 2).Children(body)
				})
			} else {
				body()
			}
		}
		if actions != nil {
			ActionGroup(c, ui.End, actions).Shrink(0)
		}
	})
}
