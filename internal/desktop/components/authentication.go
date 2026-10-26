package components

import "github.com/egoist/mygo/ui"

// AuthenticationPassword is one password field with an optional direct system
// verification action. It owns no authentication state or method switching.
func AuthenticationPassword(c *ui.Context, value *string, label, systemLabel string, showSystem, disabled bool) (input ui.Element, systemClicked bool) {
	input, action := AuthenticationPasswordWithAnchor(c, value, label, systemLabel, showSystem, disabled, false)
	return input, showSystem && action.Clicked()
}

// AuthenticationPasswordWithAnchor exposes the system action's window-space
// anchor for a native authentication view. The native view replaces only the
// painted icon; the button keeps focus, keyboard and pointer handling.
func AuthenticationPasswordWithAnchor(c *ui.Context, value *string, label, systemLabel string, showSystem, disabled, nativeIcon bool) (input, action ui.Element) {
	t := c.Theme()
	row := ui.Row(c).FillWidth().MinWidth(0).Height(38).Radius(t.Radius).
		Background(t.Surface).Border(1, t.Border).AlignItems(ui.Center).Disabled(disabled)
	if row.FocusWithin() {
		row.BorderColor(t.Accent)
	}
	row.Children(func() {
		input = ui.TextInputBase(c, value).Password().Grow(1).MinWidth(0).Height(36).Padding(6, 10).Label(label).Disabled(disabled)
		if showSystem {
			ui.Box(c).Width(1).Height(20).Background(t.Border)
			action = Button(c, "", ButtonOptions{Variant: Quiet, Disabled: disabled}).Key("authentication-system-action").Size(36, 34).Padding(0).Label(systemLabel)
			if !nativeIcon {
				action.Children(func() { ui.Icon(c, FingerprintIcon).Size(22, 22).TextColor(t.Accent) })
			}
		}
	})
	return input, action
}
