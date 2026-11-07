package components

import "github.com/egoist/mygo/ui"

// SecretInput is one secret field with an eye that reveals what was typed.
// The caller owns the revealed flag and the value's lifetime; it returns the
// row and the input, so the caller can watch Submitted and focus.
func SecretInput(c *ui.Context, key string, value *string, label, placeholder string, revealed *bool) (ui.Element, ui.Element) {
	if key != "" {
		c.Key(key)
	}
	t := c.Theme()
	row := ui.Row(c).FillWidth().MinWidth(0).Height(FormHeight).Radius(t.Radius).
		Background(t.Surface).Border(1, t.Border).AlignItems(ui.Center)
	if row.FocusWithin() {
		row.BorderColor(t.Accent)
	}
	var input ui.Element
	row.Children(func() {
		input = ui.TextInputBase(c, value).Grow(1).MinWidth(0).Height(FormHeight-2).
			Padding(6, 10).Label(label).Placeholder(placeholder)
		if !*revealed {
			input.Password()
		}
		eye := Button(c, "", ButtonOptions{Variant: Quiet}).Size(30, FormHeight-4).Padding(0).Label(label + tr(c, "明文显示"))
		eye.Children(func() {
			icon := EyeOffIcon
			if *revealed {
				icon = EyeIcon
			}
			ui.Icon(c, icon).Size(17, 17).TextColor(t.TextMuted)
		})
		if eye.Clicked() {
			*revealed = !*revealed
			input.Focus()
			c.Invalidate()
		}
	})
	return row, input
}
