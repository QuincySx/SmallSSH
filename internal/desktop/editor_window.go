package desktop

import (
	"bytes"
	"fmt"
	"io"
	"path"
	"path/filepath"
	"runtime"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"
	"github.com/egoist/mygo/ui/editor"
	"sshclient/internal/desktop/components"
)

// The close request and final close are separate: a native close must first
// pass the same unsaved-document check as the toolbar action.
type editorWindowFactory func(mygo.WindowOptions, func(), func()) toolsWindow

// Close after confirmation must bypass the native close-request hook.
type nativeEditorWindow struct{ *mygo.Window }

func (w nativeEditorWindow) Close() { w.Destroy() }

func editorBufferText(buffer *editor.Buffer) string {
	var out bytes.Buffer
	_, _ = buffer.WriteTo(&out) // bytes.Buffer writes cannot fail.
	return out.String()
}

func editorBufferSize(buffer *editor.Buffer) int {
	// MyGo preserves per-line endings, including mixed CRLF/LF files.
	n, _ := buffer.WriteTo(io.Discard)
	return int(n)
}

func (a *app) editorPathParts() (string, string) {
	if a.editLocal {
		return filepath.Base(a.editPath), filepath.Dir(a.editPath)
	}
	return path.Base(a.editPath), path.Dir(a.editPath)
}

func (a *app) refreshEditorWindowTitle() {
	if a.editorWin != nil {
		name, _ := a.editorPathParts()
		a.editorWin.SetTitle(name)
	}
}

func (a *app) editorWindowFocused() bool {
	return a.renderingEditorWindow || a.editorWin != nil && a.editorWin.IsFocused()
}

func (a *app) openEditorWindow() {
	factory := a.newEditorWindow
	if factory == nil {
		if a.win == nil {
			return
		}
		factory = func(options mygo.WindowOptions, requestClose, closed func()) toolsWindow {
			w := mygo.NewWindow(options)
			w.OnClose(func(e *mygo.CloseEvent) { e.PreventDefault(); requestClose() })
			w.OnClosed(closed)
			return nativeEditorWindow{w}
		}
	}
	a.editorWindowFocus = true
	if a.editorWin != nil {
		a.refreshEditorWindowTitle()
		a.showPresenceWindow(a.editorWin)
		a.editorWin.Invalidate()
		return
	}
	if a.editorWindowPending {
		return
	}
	a.editorWindowSequence++
	sequence := a.editorWindowSequence
	a.editorWindowPending = true
	create := func() {
		if a.editorWindowSequence != sequence || a.editPath == "" {
			return
		}
		a.editorWindowPending = false
		name, _ := a.editorPathParts()
		options := mygo.WindowOptions{Title: name, Width: 900, Height: 680,
			MinWidth: 560, MinHeight: 360, Hidden: true, Content: ui.View(a.editorWindowView)}
		applyHiddenWindowChrome(&options, runtime.GOOS)
		w := factory(options, func() {
			if a.editorWindowSequence == sequence {
				old := a.renderingEditorWindow
				a.renderingEditorWindow = true
				a.closeEditor()
				a.renderingEditorWindow = old
			}
		}, func() {
			if a.editorWindowSequence == sequence {
				a.editorWin = nil
			}
		})
		if a.editorWindowSequence != sequence || a.editPath == "" {
			if w != nil {
				w.Hide()
				w.Update(w.Close)
			}
			return
		}
		if w != nil {
			a.editorWin = w
			a.showPresenceWindow(w)
		} else {
			a.status = tr(nil, "无法打开编辑器窗口")
		}
		a.invalidateWindows()
	}
	if a.win != nil || a.dispatch != nil {
		a.postUI(create)
	} else {
		create()
	}
}

func (a *app) closeEditorWindow() {
	w := a.editorWin
	a.editorWin = nil
	a.editorWindowPending = false
	a.editorWindowFocus = false
	a.editorWindowSequence++
	if w != nil {
		w.Hide()
		w.Update(w.Close)
	}
}

func (a *app) editorWindowView(c *ui.Context) {
	previous := a.renderingEditorWindow
	a.renderingEditorWindow = true
	defer func() { a.renderingEditorWindow = previous }()
	c.SetZoom(a.viewZoom())
	components.ApplyTheme(c, false)
	if a.edit == nil || a.editPath == "" {
		return
	}
	name, dir := a.editorPathParts()
	components.WindowFrame(c, components.WindowFrameOptions{
		Title:         reviewText(name),
		Target:        a.endpointName(a.editHost) + " · " + reviewText(dir),
		Close:         a.closeEditor,
		CloseDisabled: a.editorSaving == a.edit,
		Actions: func() {
			disabled := a.editorRead != nil || a.busy || a.editorSaving == a.edit || !a.edit.Buffer.Modified()
			if components.SVGButton(c, components.SaveIcon, tr(c, "保存")).Disabled(disabled).Clicked() {
				a.saveEditor()
			}
			components.SVGButton(c, components.MoreIcon, tr(c, "编辑操作")).Menu(func(m *ui.Menu) {
				if m.Item(tr(c, "另存为")).Disabled(a.editorRead != nil || a.busy).Chosen() {
					a.saveEditorAs()
				}
			})
		},
		Footer: func() {
			if a.status != "" {
				ui.Text(c, reviewText(a.status)).FillWidth().Padding(3, 10).SingleLine().FontSize(11).TextColor(c.Theme().TextMuted)
			}
			if a.editorRead != nil {
				return
			}
			pos := a.edit.CursorPos()
			ending := "LF"
			if a.edit.Buffer.LineEnding() == "\r\n" {
				ending = "CRLF"
			}
			ui.Row(c).FillWidth().Padding(5, 10).Gap(12).Children(func() {
				ui.Text(c, fmt.Sprintf(tr(c, "%d 行 · 第 %d 行，第 %d 列"), a.edit.Buffer.LineCount(), pos.Line+1, pos.Col+1)).FontSize(11).TextColor(c.Theme().TextMuted)
				ui.Spacer(c).Grow(1)
				ui.Text(c, fmt.Sprintf("UTF-8 · %s · %d B", ending, editorBufferSize(a.edit.Buffer))).FontSize(11).TextColor(c.Theme().TextMuted)
			})
		},
	}, func() {
		if a.editorRead != nil {
			a.editorReadView(c, a.editorRead)
		} else {
			view := editor.View(c, a.edit).FillWidth().Grow(1).MinHeight(0).Label(tr(c, "文件编辑器"))
			if a.editorWindowFocus {
				view.Focus()
				a.editorWindowFocus = false
			}
			if view.Shortcut(ui.Cmd, ui.KeyS) {
				a.saveEditor()
			}
		}
		a.promptForHost(c, dialogHostEditor)
	})
}
