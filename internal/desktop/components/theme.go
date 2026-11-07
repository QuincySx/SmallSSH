// Package components contains the workbench's shared presentation components.
// It deliberately knows nothing about connections, credentials or permissions.
package components

import "github.com/egoist/mygo/ui"

const (
	ControlHeight = 32
	FormHeight    = 36
	FormGap       = 8
	SectionGap    = 16
	IconSize      = 26
	RowHeight     = 25
	ActionGap     = 8
	BodyFont      = 12
	DetailFont    = 11
	HeaderHeight  = 36
	HeaderInset   = 6
)

// ApplyTheme supplies the shared native palette, including system appearance.
func ApplyTheme(c *ui.Context, spacious bool) {
	t := *c.Theme()
	if t.Dark {
		t.Background, t.Surface, t.SurfaceHover, t.SurfacePressed, t.Border = ui.Hex("#14161B"), ui.Hex("#1C1F26"), ui.Hex("#2C313C"), ui.Hex("#333947"), ui.Hex("#303640")
		t.Text, t.TextMuted, t.Accent, t.AccentText = ui.Hex("#EAECF3"), ui.Hex("#A6ADBA"), ui.Hex("#AAA1FF"), ui.Hex("#231F46")
	} else {
		t.Background, t.Surface, t.SurfaceHover, t.SurfacePressed, t.Border = ui.Hex("#F3F4F7"), ui.Hex("#FFFFFF"), ui.Hex("#E9EBF0"), ui.Hex("#DFE2E9"), ui.Hex("#E2E5EB")
		t.Text, t.TextMuted, t.Accent, t.AccentText = ui.Hex("#272A34"), ui.Hex("#626B7A"), ui.Hex("#6555CD"), ui.Hex("#FFFFFF")
	}
	t.AccentHover, t.AccentPressed = t.Accent.Mix(t.Text, .12), t.Accent.Mix(t.Text, .22)
	t.FontSize, t.Spacing, t.Radius = 13, 4, 8
	if spacious {
		t.Spacing = 6
	}
	c.SetTheme(&t)
}

const WorkspaceGap float32 = 8
const PanelRadius float32 = 16
