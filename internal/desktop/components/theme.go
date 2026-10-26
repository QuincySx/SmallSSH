// Package components contains the workbench's shared presentation components.
// It deliberately knows nothing about connections, credentials or permissions.
package components

import "github.com/egoist/mygo/ui"

const (
	ControlHeight = 32
	FormHeight    = 36
	IconSize      = 26
	RowHeight     = 25
	ActionGap     = 8
	BodyFont      = 12
	DetailFont    = 11
	HeaderHeight  = 36
	HeaderInset   = 6
)

// ApplyTheme keeps density decisions out of individual feature pages. Colors
// continue to come from MyGo, including changes of the system appearance.
func ApplyTheme(c *ui.Context, spacious bool) {
	t := *c.Theme()
	t.Spacing, t.Radius = 4, 6
	if spacious {
		t.Spacing, t.Radius = 6, 10
	}
	c.SetTheme(&t)
}
