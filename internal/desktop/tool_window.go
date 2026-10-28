package desktop

import (
	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"
	"sshclient/internal/desktop/components"
)

// The same tools have one owner window at a time. The native adapter is
// replaceable in tests; headless tests must never create OS windows.
type toolsWindow interface {
	Show()
	Hide()
	Close()
	Update(func())
	Invalidate()
	SetTitle(string)
	IsFocused() bool
}

type toolsWindowFactory func(mygo.WindowOptions, func()) toolsWindow

type toolsSharedState struct {
	online, detached, hidden, right, expanded         bool
	page, dock                                        int
	active                                            *session
	editShown, prompt, snippet, transfers             bool
	promptDetached, snippetDetached, transferDetached bool
	status, title                                     string
}

func (a *app) sharedToolsState() toolsSharedState {
	return toolsSharedState{a.online, a.toolsDetached, a.toolsHidden, a.toolsRight, a.fileExpanded,
		a.page, a.dock, a.serverStateSession(), a.editShown, a.prompt.open, a.snippetOpen, a.transfersOpen,
		a.prompt.detached, a.snippetDetached, a.transfersDetached, a.status, a.toolsWindowTitle()}
}

func (a *app) invalidateWindows() {
	if a.win != nil {
		a.win.Invalidate()
	}
	if a.toolsWin != nil {
		a.toolsWin.Invalidate()
	}
}

// Only shared state changes request the other window's frame. Terminal output
// frames alone must not make the resource window redraw at terminal speed.
func (a *app) publishToolsState() {
	// The first frame starts the idle lock's clock; it keeps its own time so a
	// window nobody touches still locks the vault.
	a.watchIdle()
	state := a.sharedToolsState()
	if state != a.toolsPublished {
		a.toolsPublished = state
		if a.toolsWin != nil {
			a.toolsWin.SetTitle(a.toolsWindowTitle())
		}
		a.invalidateWindows()
	}
}

func (a *app) toolsWindowTitle() string {
	if s := a.serverStateSession(); s != nil {
		return tr(nil, "SSH 工具 · ") + reviewText(sessionDisplayName(nil, s))
	}
	return tr(nil, "SSH 工具")
}

func (a *app) toolsWindowFocused() bool {
	return a.toolsDetached && (a.renderingToolsWindow || a.toolsWin != nil && a.toolsWin.IsFocused())
}

func (a *app) detachTools() {
	if !a.online || !a.startup.mayPopup() {
		return
	}
	factory := a.newToolsWindow
	if factory == nil {
		if a.win == nil {
			return // No native application exists in a headless preview.
		}
		factory = func(options mygo.WindowOptions, closed func()) toolsWindow {
			w := mygo.NewWindow(options)
			w.OnClosed(closed)
			return w
		}
	}
	a.toolsLayoutOpen, a.fileExpanded, a.toolsHidden = false, false, false
	if !a.toolsDetached {
		a.resetToolPresentation()
	}
	a.toolsDetached = true
	if a.toolsWin == nil {
		if a.toolsWindowPending {
			return
		}
		a.toolsWindowSequence++
		sequence := a.toolsWindowSequence
		a.toolsWindowPending = true
		create := func() {
			if a.toolsWindowSequence != sequence || !a.online || !a.toolsDetached || a.toolsHidden {
				return
			}
			a.toolsWindowPending = false
			w := factory(mygo.WindowOptions{Title: a.toolsWindowTitle(), Width: 960, Height: 720,
				MinWidth: 640, MinHeight: 360, Hidden: true, Content: ui.View(a.detachedToolsView)}, func() {
				if a.toolsWindowSequence != sequence {
					return
				}
				a.toolsWin = nil
				a.toolsHidden = true
				a.toolsLayoutOpen, a.transfersOpen = false, false
				a.moveToolDialogsToMain()
				a.invalidateWindows()
			})
			if a.toolsWindowSequence != sequence || !a.online || !a.toolsDetached || a.toolsHidden {
				if w != nil {
					w.Hide()
					w.Update(w.Close)
				}
				return
			}
			if w == nil {
				a.toolsDetached = false
				a.status = tr(nil, "无法打开工具窗口")
			} else {
				a.toolsWin = w
				w.Show()
			}
			a.invalidateWindows()
		}
		// Native window creation can dispatch nested platform events. Defer
		// it until the current rendering callback has returned, as for close.
		if a.win != nil || a.dispatch != nil {
			a.postUI(create)
		} else {
			create() // Injected headless factory: no OS calls or event loop.
		}
	} else {
		a.toolsWin.Show()
	}
	// Called only by an explicit user action, never during startup or polling.
	a.contentFocus = false
	a.invalidateWindows()
}

func (a *app) closeToolsWindow() {
	w := a.toolsWin
	a.toolsWin = nil
	a.toolsWindowPending = false
	a.toolsWindowSequence++ // A delayed close cannot affect a replacement.
	if w != nil {
		w.Hide()
		// A dock button can run inside this window's render callback. Destroy
		// its surface after that frame, rather than underneath the UI engine.
		w.Update(w.Close)
	}
}

func (a *app) moveToolDialogsToMain() {
	if a.prompt.detached {
		a.rebindDockerLogs()
	}
	a.prompt.detached, a.snippetDetached = false, false
	if a.serverNotes != nil {
		a.serverNotes.detached = false
	}
	a.setTransferWindow(false)
}

func (a *app) detachedToolsView(c *ui.Context) {
	previous := a.renderingToolsWindow
	a.renderingToolsWindow = true
	defer func() {
		a.renderingToolsWindow = previous
		a.publishToolsState()
	}()
	components.ApplyTheme(c, false)
	ui.Column(c).Fill().Background(c.Theme().Background).Label(tr(c, "独立工具窗口")).Children(func() {
		if !a.online || !a.toolsDetached || a.toolsHidden {
			return
		}
		a.syncFollowingFilePanes()
		a.fileArea(c)
		if a.status != "" {
			ui.Text(c, reviewText(a.status)).FillWidth().Padding(4, 8).FontSize(11).SingleLine().TextColor(c.Theme().TextMuted)
		}
		if a.prompt.detached {
			a.promptView(c)
		}
		if a.snippetDetached {
			a.snippetDialog(c)
		}
		a.serverNotesDialog(c)
	})
}

// UI list/grid runtimes belong to their rendering window. Move only their
// presentation bindings, retaining cached data, keys, choices, column layout
// and the first visible row. This also avoids per-engine frame ID collisions.
func rebindToolList(s *ui.ListState) {
	first, _ := s.Visible()
	*s = ui.ListState{Handle: s.Handle, Key: s.Key, KeepOffset: s.KeepOffset, Selected: s.Selected, Selection: s.Selection,
		Label: s.Label, Sort: s.Sort, SortBy: s.SortBy, Columns: s.Columns}
	if first > 0 {
		s.ScrollTo(first, ui.Start)
	}
}

func (a *app) resetToolPresentation() {
	a.rebindDockerLogs()
	for _, p := range a.allFilePanes() {
		for _, b := range p.states {
			rebindToolList(&b.table)
			if b.tree != nil {
				rebindToolList(&b.tree.list)
			}
		}
	}
	rebindToolList(&a.snippetTable)
	rebindToolList(&a.transferTable)
	for _, sess := range a.sessions {
		if state := sess.management; state != nil {
			if state.containerDetails != nil && state.containerDetails.log != nil {
				rebindToolList(&state.containerDetails.log.list)
			}
			for _, list := range []*ui.ListState{&state.processTable, &state.serviceTable, &state.containerTable,
				&state.imageTable, &state.networkList} {
				rebindToolList(list)
			}
			for _, grid := range []*ui.GridState{&state.containerGrid, &state.composeGrid, &state.composeContainers} {
				*grid = ui.GridState{Handle: grid.Handle, Key: grid.Key, Selected: grid.Selected, Selection: grid.Selection, Label: grid.Label}
			}
			state.resourceViewportHeight = 0
			state.resourceViewportWidth = 0
		}
	}
}

func (a *app) rebindDockerLogs() {
	if s := a.prompt.docker; s != nil {
		if s.log != nil {
			rebindToolList(&s.log.list)
		}
		if s.job != nil && s.job.log != nil {
			rebindToolList(&s.job.log.list)
		}
	}
}
