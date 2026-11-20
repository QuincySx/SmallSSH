package desktop

import (
	"path/filepath"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"
	"sshclient/internal/connectcfg"
)

func (a *app) canUploadTo(p *filePane) bool {
	return a.online && !a.busy && a.transfers != nil && p.endpoint != "" && p.endpoint != localEndpoint
}

func (a *app) promptPaneUpload(p *filePane) {
	parent := a.win
	if a.toolsWindowFocused() {
		if window, ok := a.toolsWin.(*mygo.Window); ok {
			parent = window
		}
	}
	a.selectPaneUpload(p, func() ([]string, error) { return chooseUpload(parent) })
}

func (a *app) selectPaneUpload(p *filePane, pick func() ([]string, error)) {
	if !a.canUploadTo(p) {
		return
	}
	// Keep the destination and its route from the original pane. A native
	// dialog must run outside the render callback.
	endpoint, dir, route := p.endpoint, p.browser().dir, p.route
	a.work(func() (func(), error) {
		files, err := pick()
		return func() { a.queueUploadPaths(endpoint, dir, route, files) }, err
	})
}

func (a *app) nativeFileDropTarget(c *ui.Context, element ui.Element, p *filePane, dir string) {
	if !a.canUploadTo(p) {
		return
	}
	if element.FileDragOver() {
		element.Border(1, c.Theme().Accent)
	}
	if paths := element.DroppedFiles(); len(paths) > 0 {
		a.queueUploadPaths(p.endpoint, dir, p.route, paths)
	}
}

func (a *app) queueUploadPaths(endpoint, dir string, route connectcfg.RouteSelection, paths []string) {
	if len(paths) == 0 {
		return
	}
	items := make([]fileTransferItem, 0, len(paths))
	for _, source := range paths {
		items = append(items, fileTransferItem{name: filepath.Base(source), source: source})
	}
	a.queueFileTransfers(endpoint, "upload", dir, route, items)
}
