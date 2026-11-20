package components

import (
	"github.com/egoist/mygo/ui"
	"sshclient/docs/design"
)

var secretVisibilityIconSize = design.Length("size-secret-visibility-icon")

// SecretInput is one secret field with an eye that reveals what was typed.
// The caller owns the revealed flag, the value's lifetime and any trailing
// action. The row and input are returned so the caller can measure and focus.
func SecretInput(c *ui.Context, key string, value *string, label, placeholder string, revealed *bool, trailing ...func()) (ui.Element, ui.Element) {
	if key != "" {
		c.Key(key)
	}
	t := c.Theme()
	height := FormHeight
	if len(trailing) != 0 {
		height = design.Length("size-approval-password-with-system")
	}
	row := ui.Row(c).FillWidth().MinWidth(0).Height(height).Radius(t.Radius).
		Background(t.Surface).Border(1, t.Border).AlignItems(ui.Center)
	if row.FocusWithin() {
		row.BorderColor(t.Accent)
	}
	var input ui.Element
	row.Children(func() {
		input = ui.TextInputBase(c, value).Grow(1).MinWidth(0).Height(height-2).
			Padding(6, 10).Label(label).Placeholder(placeholder)
		if !*revealed {
			input.Password()
		}
		eye := Button(c, "", ButtonOptions{Variant: Quiet}).Size(FieldActionSize, ControlHeight).Padding(0).Label(label + tr(c, "明文显示"))
		eye.Children(func() {
			icon := EyeOffIcon
			if *revealed {
				icon = EyeIcon
			}
			ui.Icon(c, icon).Size(secretVisibilityIconSize, secretVisibilityIconSize).TextColor(t.TextMuted)
		})
		if eye.Clicked() {
			*revealed = !*revealed
			input.Focus()
			c.Invalidate()
		}
		for _, action := range trailing {
			action()
		}
	})
	return row, input
}
