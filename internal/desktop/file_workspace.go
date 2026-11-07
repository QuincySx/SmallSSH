package desktop

import (
	"github.com/egoist/mygo/ui"
	"sshclient/internal/desktop/components"
)

func (a *app) roundedWorkspacePanel(c *ui.Context, body func()) {
	ui.Column(c).Fill().MinWidth(0).MinHeight(0).Radius(components.PanelRadius).Clip().Background(c.Theme().Surface).Children(body)
}

func (a *app) openFileTab() {
	a.openWorkspaceTool(toolFiles)
}
func (a *app) closeFileTab() {
	a.resetToolPresentation()
	if file := a.workspaceTools[toolFiles]; file != nil {
		a.closeWorkspaceTab(workspaceTab{tool: file})
	}
	a.filesTabOpen, a.filesTabSelected = false, false
	a.contentFocus = a.terminalFocusWanted()
	a.invalidateWindows()
}
func (a *app) showFileDock() {
	a.closeFileTab()
	a.showTool(toolFiles)
}
func (a *app) fileWorkspaceView(c *ui.Context) {
	a.ensureFileLayout()
	a.roundedWorkspacePanel(c, func() { a.fileWindows(c, a.fileLayout) })
}
