package desktop

import (
	"context"
	"errors"
	"slices"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"
)

const toolVNCBase = 1000

type vncEmbeddedView interface {
	ui.NativeWebView
	Destroy()
}

func restrictVNCPage(page *mygo.Page, address string) {
	page.OnWillNavigate(func(event *mygo.NavigateEvent) {
		if event.URL != address {
			event.PreventDefault()
		}
	})
	page.SetWindowOpenHandler(func(mygo.WindowOpenRequest) *mygo.WindowOptions { return nil })
	page.OnPageTitleUpdated(func(event *mygo.TitleEvent) { event.PreventDefault() })
}

func (a *app) attachVNCTab(run *vncRun, address string) error {
	factory := a.newVNCView
	if factory == nil && a.win != nil {
		factory = func(options mygo.WebViewOptions) (vncEmbeddedView, error) {
			view, err := a.win.NewWebView(options)
			if err == nil {
				restrictVNCPage(view.Page(), options.URL)
			}
			return view, err
		}
	}
	if factory == nil {
		return errors.New(tr(nil, "无法创建 VNC 标签页"))
	}
	view, err := factory(mygo.WebViewOptions{URL: address})
	if err != nil {
		return err
	}
	a.ensureTerminalGroups()
	if a.workspaceTools == nil {
		a.workspaceTools = make(map[int]*workspaceToolTab)
	}
	a.vnc.sequence++
	tab := &workspaceToolTab{kind: toolVNCBase + a.vnc.sequence, group: a.activeTerminalGroup, vnc: run}
	run.view, run.tab = view, tab
	a.workspaceTools[tab.kind] = tab
	tab.group.tools = append(tab.group.tools, tab)
	a.placeCanvasTab(workspaceTab{tool: tab}, tab.group)
	a.activateWorkspaceTab(tab.group, workspaceTab{tool: tab})
	a.page = 1
	a.workspaceRevision++
	a.invalidateWindows()
	return nil
}

// Full screen keeps the same native WebView and RFB session alive. WebViews
// cannot be reparented between native windows.
func (a *app) toggleVNCFullscreen(run *vncRun) {
	if a.vnc.fullscreen == run {
		a.leaveVNCFullscreen(run)
		return
	}
	if run.view == nil {
		return
	}
	if a.vnc.fullscreen != nil {
		a.leaveVNCFullscreen(a.vnc.fullscreen)
	}
	a.vnc.fullscreen = run
	if a.win != nil {
		run.fullscreenOff = a.win.OnLeaveFullScreen(func() {
			if a.vnc.fullscreen == run {
				a.leaveVNCFullscreen(run)
			}
		})
		run.wasFullscreen = a.win.IsFullScreen()
		if !run.wasFullscreen {
			a.win.SetFullScreen(true)
		}
	}
	a.invalidateWindows()
}

func (a *app) leaveVNCFullscreen(run *vncRun) {
	if a.vnc.fullscreen != run {
		return
	}
	a.vnc.fullscreen = nil
	if run.fullscreenOff != nil {
		run.fullscreenOff()
		run.fullscreenOff = nil
	}
	if a.win != nil && !run.wasFullscreen {
		a.win.SetFullScreen(false)
	}
	a.invalidateWindows()
}

// Native viewer controls are scoped to a live run and marshalled to its UI.
func (a *app) vncControl(requestCtx, runCtx context.Context, run *vncRun, generation int, action func() (string, error)) (string, error) {
	type result struct {
		text string
		err  error
	}
	done := make(chan result, 1)
	a.postUI(func() {
		if requestCtx.Err() != nil || runCtx.Err() != nil || generation != a.generation || !a.online || !slices.Contains(a.vnc.runs, run) {
			done <- result{err: context.Canceled}
			return
		}
		text, err := action()
		done <- result{text, err}
	})
	select {
	case value := <-done:
		return value.text, value.err
	case <-requestCtx.Done():
		return "", requestCtx.Err()
	case <-runCtx.Done():
		return "", runCtx.Err()
	}
}
