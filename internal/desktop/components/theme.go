// Package components contains the workbench's shared presentation components.
// It deliberately knows nothing about connections, credentials or permissions.
package components

import (
	"github.com/egoist/mygo/ui"
	"sshclient/docs/design"
)

var (
	ControlHeight   = design.Length("size-control")
	FormHeight      = design.Length("size-field")
	FieldActionSize = design.Length("size-native-field-action")
	ActionIconSize  = design.Length("text-action-icon")
)

const (
	FormGap      = 8
	SectionGap   = 16
	IconSize     = 26
	RowHeight    = 25
	ActionGap    = 8
	BodyFont     = 12
	DetailFont   = 11
	HeaderHeight = 36
	HeaderInset  = 6
)

const PanelHeaderHeight, PanelInset = 32, 12
const ToolHeaderHeight, ToolInset = 36, 8
const CanvasToolHeaderHeight, CanvasToolBarHeight = 32, 28
const WorkspaceInset float32 = 8

// ApplyTheme supplies the shared native palette, including system appearance.
func ApplyTheme(c *ui.Context, spacious bool) {
	t := *c.Theme()
	color := func(name string) ui.Color { return ui.Hex(design.Color(t.Dark, name)) }
	t.Background = color("color-canvas")
	t.Surface = color("color-surface")
	t.Border = color("color-border")
	t.Text = color("color-text")
	t.TextMuted = color("color-muted")
	t.Accent = color("color-accent")
	t.AccentText = color("color-onaccent")
	t.Success = color("color-success")
	t.Warning = color("color-warning")
	t.Danger = color("color-danger")
	if t.Dark {
		t.SurfaceHover, t.SurfacePressed = ui.Hex("#2C313C"), ui.Hex("#333947")
	} else {
		t.SurfaceHover, t.SurfacePressed = ui.Hex("#E9EBF0"), ui.Hex("#DFE2E9")
	}
	t.Focus, t.Selection = t.Accent, t.Accent.Alpha(.18)
	t.AccentHover, t.AccentPressed = t.Accent.Mix(t.Text, .12), t.Accent.Mix(t.Text, .22)
	t.FontSize, t.Spacing, t.Radius = 13, 4, 8
	if spacious {
		t.Spacing = 6
	}
	c.SetTheme(&t)
}

const WorkspaceGap float32 = 8
const PanelRadius float32 = 16
