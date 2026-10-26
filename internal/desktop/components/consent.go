package components

import "github.com/egoist/mygo/ui"

type ConsentOptions struct {
	Icon                   ui.ImageSource
	Title, Applicant, Note string
}

// ConsentPanel keeps an identity decision compact and distinct from a settings
// form. Only the review scrolls; verification and refusal stay in view.
// It presents no OS authentication UI and never makes an approval decision.
func ConsentPanel(c *ui.Context, o ConsentOptions, review, actions func()) ui.Element {
	return ui.Column(c).Fill().Padding(24, 28, 20).Gap(16).Children(func() {
		ui.Scroll(c).FillWidth().Grow(1).MinHeight(0).Children(func() {
			ui.Column(c).FillWidth().Padding(0, 3).Gap(18).Children(func() {
				ui.Column(c).FillWidth().AlignItems(ui.Center).Gap(8).Children(func() {
					if o.Icon != nil {
						ui.Image(c, o.Icon).Size(56, 56).Margin(0, 0, 4, 0).Label(tr(c, "应用图标"))
					}
					ui.Text(c, o.Title).FillWidth().TextAlign(ui.Center).FontSize(18).Bold()
					ui.Text(c, o.Applicant).FillWidth().TextAlign(ui.Center).FontSize(13).Selectable()
					if o.Note != "" {
						ui.Text(c, o.Note).FillWidth().TextAlign(ui.Center).FontSize(11).TextColor(c.Theme().TextMuted)
					}
				})
				if review != nil {
					review()
				}
			})
		})
		if actions != nil {
			ui.Column(c).FillWidth().Shrink(0).Gap(8).Children(actions)
		}
	})
}
