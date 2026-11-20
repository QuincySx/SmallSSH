package desktop

import "github.com/egoist/mygo/ui"

type dialogHost uint8

const (
	dialogHostMain dialogHost = iota
	dialogHostTools
	dialogHostSettings
	dialogHostEditor
)

func (a *app) currentDialogHost() dialogHost {
	if a.renderingEditorWindow || a.editorWindowFocused() {
		return dialogHostEditor
	}
	if a.renderingSettingsWindow || a.settingsWin != nil && a.settingsWin.IsFocused() {
		return dialogHostSettings
	}
	if a.toolsWindowFocused() {
		return dialogHostTools
	}
	return dialogHostMain
}

func (a *app) promptForHost(c *ui.Context, host dialogHost) {
	a.showNextUploadConflict()
	owner := a.prompt.host
	if owner == dialogHostMain && a.prompt.detached && a.toolsDetached && !a.toolsHidden {
		owner = dialogHostTools
	}
	if owner == dialogHostEditor && a.editorWin == nil && !a.editorWindowPending {
		owner = dialogHostMain
	}
	if owner == dialogHostSettings && !a.settingsOpen {
		owner = dialogHostMain
	}
	if owner == host {
		a.promptView(c)
	}
}
