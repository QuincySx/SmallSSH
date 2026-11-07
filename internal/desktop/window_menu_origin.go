package desktop

import "github.com/egoist/mygo/ui/editor"

type windowMenuOrigin struct {
	host       dialogHost
	sequence   uint64
	generation int
	terminal   *session
	document   *editor.State
	files      bool
	tool       *workspaceToolTab
}

func (a *app) captureWindowMenuOrigin() windowMenuOrigin {
	origin := windowMenuOrigin{host: a.currentDialogHost(), generation: a.generation, terminal: a.currentTerminal(), files: a.filesTabSelected}
	if a.activeTerminalGroup != nil {
		origin.tool = a.activeTerminalGroup.selectedTool
	}
	if a.editShown {
		origin.document = a.edit
	}
	switch origin.host {
	case dialogHostTools:
		origin.sequence = a.toolsWindowSequence
	case dialogHostSettings:
		origin.sequence = a.settingsWindowSequence
	case dialogHostEditor:
		origin.sequence = a.editorWindowSequence
		origin.document = a.edit
	}
	return origin
}
func (a *app) runHostedWindowMenuAction(action menuAction, origin windowMenuOrigin) {
	if origin.generation != a.generation {
		return
	}
	close := action.shortcut == "Cmd+W" || action.shortcut == "Ctrl+Shift+W"
	if !close {
		a.runMenu(action.run)
		return
	}
	switch origin.host {
	case dialogHostEditor:
		if origin.sequence == a.editorWindowSequence && origin.document == a.edit && a.editPath != "" {
			old := a.renderingEditorWindow
			a.renderingEditorWindow = true
			a.closeEditor()
			a.renderingEditorWindow = old
		}
	case dialogHostSettings:
		if origin.sequence == a.settingsWindowSequence {
			a.closeSettingsWindow()
		}
	case dialogHostTools:
		a.runWindowMenuAction(action, true, origin.sequence)
	default:
		if a.serverNotes != nil && !a.serverNotes.detached {
			a.requestCloseNotes(a.serverNotes)
			return
		}
		if a.page == 0 {
			a.runMenu(action.run)
			return
		}
		if origin.document != nil {
			if a.edit == origin.document {
				a.closeEditor()
			}
		} else if origin.tool != nil {
			if a.workspaceTools[origin.tool.kind] == origin.tool {
				a.closeWorkspaceTab(workspaceTab{tool: origin.tool})
			}
		} else if origin.files {
			if a.filesTabOpen {
				a.closeFileTab()
			}
		} else if origin.terminal != nil {
			a.closeSessionAt(a.sessionIndex(origin.terminal))
		}
	}
}
