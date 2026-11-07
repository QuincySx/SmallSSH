package components

import "github.com/egoist/mygo/ui"

// FormSection groups fields without creating a separate label alignment scope.
func FormSection(c *ui.Context, title string, body func()) {
	ui.Column(c).FillWidth().Gap(FormGap).Children(func() {
		ui.Text(c, title).FontSize(13).Bold().Role(ui.RoleHeading)
		ui.Column(c).FillWidth().Gap(FormGap).Children(body)
	})
}
