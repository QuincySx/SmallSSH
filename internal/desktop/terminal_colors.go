package desktop

import (
	"github.com/egoist/mygo/plugins/terminal"
	"github.com/egoist/mygo/ui"
)

func workspaceTerminalPadding() terminal.Padding {
	return terminal.Padding{X: 16, Y: 2}
}

func workspaceTerminalColors() (*terminal.Theme, *terminal.Theme) {
	light, dark := terminal.LightTheme(), terminal.DarkTheme()
	light.Background, light.Foreground = ui.Hex("#FFFFFF"), ui.Hex("#272A34")
	dark.Background, dark.Foreground = ui.Hex("#1C1F26"), ui.Hex("#EAECF3")
	return light, dark
}
