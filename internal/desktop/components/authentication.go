package components

import (
	"runtime"

	"github.com/egoist/mygo/ui"
)

// SystemAuthIcon is the platform's own sign-in mark: a fingerprint on macOS,
// Windows Hello's face on Windows, a key elsewhere. The native Touch ID view
// paints over it when the system provides an embedded control.
func SystemAuthIcon() *ui.SVG {
	switch runtime.GOOS {
	case "darwin":
		return FingerprintIcon
	case "windows":
		return FaceIcon
	}
	return KeyIcon
}

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
	row := ui.Row(c).FillWidth().MinWidth(0).Height(44).Radius(t.Radius).
		Background(t.Surface).Border(1, t.Border).AlignItems(ui.Center).Disabled(disabled)
	if row.FocusWithin() {
		row.BorderColor(t.Accent)
	}
	row.Children(func() {
		input = ui.TextInputBase(c, value).Password().Grow(1).MinWidth(0).Height(42).Padding(6, 10).Label(label).Disabled(disabled)
		if showSystem {
			ui.Box(c).Width(1).Height(20).Background(t.Border)
			action = Button(c, "", ButtonOptions{Variant: Quiet, Disabled: disabled}).Key("authentication-system-action").Size(42, 40).Padding(0).Label(systemLabel)
			if !nativeIcon {
				action.Children(func() { ui.Icon(c, SystemAuthIcon()).Size(24, 24).TextColor(t.Accent) })
			}
		}
	})
	return input, action
}
