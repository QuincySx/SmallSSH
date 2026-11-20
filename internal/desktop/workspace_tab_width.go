package desktop

import "sshclient/internal/desktop/components"

const (
	workspaceTabMaxWidth = 210
	// About four CJK characters or eight Latin ones stay readable.
	workspaceTabMinWidth = 72
	// Each tab also carries its row padding, the gap before its close
	// button, the button itself and the strip gap.
	workspaceTabChrome = 2*2 + 2 + components.IconSize + 3
)

// workspaceTabWidth shares the strip between the open tabs, as browsers do,
// so a narrow window still shows that several tabs are open instead of one
// tab filling the strip. Past the minimum the strip scrolls and keeps
// revealing the selected tab, as before; a single tab keeps the old width.
func workspaceTabWidth(available float32, count int) float32 {
	available = max(20, available)
	width := min(workspaceTabMaxWidth, available)
	if count > 1 {
		width = min(width, max(workspaceTabMinWidth, available/float32(count)-workspaceTabChrome))
	}
	return width
}
