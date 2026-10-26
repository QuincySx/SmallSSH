package desktop

import (
	"fmt"
	"slices"

	"github.com/egoist/mygo/ui"
	"sshclient/internal/sshconn"
)

const maxFilePanes = 8

// A leaf owns one browser. Branches own only split direction and proportion;
// rearranging a tree never replaces browser state or transfer targets.
type fileLayoutNode struct {
	pane          *filePane
	first, second *fileLayoutNode
	vertical      bool
	share         float32
}

func (n *fileLayoutNode) panes() []*filePane {
	if n == nil {
		return nil
	}
	if n.pane != nil {
		return []*filePane{n.pane}
	}
	return append(n.first.panes(), n.second.panes()...)
}

func (a *app) allFilePanes() []*filePane {
	var panes []*filePane
	if a.fileLayout != nil {
		panes = a.fileLayout.panes()
	}
	for _, cached := range []*filePane{a.localFilePane, a.primaryFilePane} {
		if cached != nil && !slices.Contains(panes, cached) {
			panes = append(panes, cached)
		}
	}
	return panes
}

func (a *app) ensureFileLayout() {
	a.ensurePanes()
	if a.fileLayout == nil {
		a.fileLayout = a.fileLeaf(a.primaryFilePane)
	}
	if !slices.Contains(a.fileLayout.panes(), a.activeFilePane) {
		a.activeFilePane = a.fileLayout.panes()[0]
	}
}

func (a *app) fileLeaf(p *filePane) *fileLayoutNode {
	a.ensureFilePaneID(p)
	return &fileLayoutNode{pane: p}
}

func (a *app) ensureFilePaneID(p *filePane) {
	if p.id == 0 {
		a.filePaneSequence++
		p.id = a.filePaneSequence
	}
}

func replaceFileNode(n *fileLayoutNode, target *filePane, replacement *fileLayoutNode) *fileLayoutNode {
	if n == nil {
		return nil
	}
	if n.pane == target {
		return replacement
	}
	if n.pane != nil {
		return n
	}
	first, second := replaceFileNode(n.first, target, replacement), replaceFileNode(n.second, target, replacement)
	if first == nil {
		return second
	}
	if second == nil {
		return first
	}
	if first == n.first && second == n.second {
		return n
	}
	copy := *n
	copy.first, copy.second = first, second
	return &copy
}

func cloneFilePane(source *filePane) *filePane {
	// A new window starts at the same location, independently pinned. Copy
	// loaded listings, not in-flight requests, selections or mutable tables.
	p := newFilePane(source.endpoint, false)
	p.treeWidth = source.treeWidth
	from, to := source.browser(), p.browser()
	to.dir, to.address = from.dir, from.dir
	for dir, files := range from.cache {
		to.cache[dir] = append([]sshconn.File(nil), files...)
	}
	for dir, opened := range from.open {
		value := *opened
		to.open[dir] = &value
	}
	to.reveal(p.endpoint == localEndpoint, to.dir)
	return p
}

func (a *app) splitFilePane(p *filePane, vertical bool, useLocal bool) *filePane {
	a.ensureFileLayout()
	panes := a.fileLayout.panes()
	if len(panes) >= maxFilePanes || !slices.Contains(panes, p) {
		return nil
	}
	added := cloneFilePane(p)
	if useLocal && len(panes) == 1 && !slices.Contains(panes, a.localFilePane) {
		added = a.localFilePane
	}
	return a.insertFilePane(p, added, vertical)
}

func (a *app) addLocalFileWindow(p *filePane) *filePane {
	a.ensureFileLayout()
	panes := a.fileLayout.panes()
	if len(panes) >= maxFilePanes || !slices.Contains(panes, p) {
		return nil
	}
	added := a.localFilePane
	if added.endpoint != localEndpoint {
		source := *added
		source.endpoint = localEndpoint
		added = cloneFilePane(&source)
	} else if slices.Contains(panes, added) {
		added = cloneFilePane(added)
	}
	added.follow = false
	return a.insertFilePane(p, added, false)
}

// Callers validate the destination and limit before inserting a new leaf.
func (a *app) insertFilePane(p, added *filePane, vertical bool) *filePane {
	replacement := &fileLayoutNode{first: a.fileLeaf(p), second: a.fileLeaf(added), vertical: vertical, share: 50}
	a.fileLayout = replaceFileNode(a.fileLayout, p, replacement)
	a.activeFilePane = added
	return added
}

func (a *app) closeFilePane(p *filePane) {
	a.ensureFileLayout()
	panes := a.fileLayout.panes()
	if len(panes) < 2 || !slices.Contains(panes, p) {
		return
	}
	cancelPaneLoads(p)
	a.fileLayout = replaceFileNode(a.fileLayout, p, nil)
	remaining := a.fileLayout.panes()
	if a.primaryFilePane == p {
		a.primaryFilePane = remaining[0]
	}
	if a.activeFilePane == p {
		a.activeFilePane = remaining[0]
	}
}

func (a *app) keepFilePane(p *filePane) {
	a.ensureFileLayout()
	if !slices.Contains(a.fileLayout.panes(), p) {
		return
	}
	for _, other := range a.fileLayout.panes() {
		if other != p {
			cancelPaneLoads(other)
		}
	}
	a.fileLayout, a.activeFilePane, a.primaryFilePane = a.fileLeaf(p), p, p
}

func joinFileNodes(nodes []*fileLayoutNode, vertical bool) *fileLayoutNode {
	if len(nodes) == 1 {
		return nodes[0]
	}
	middle := (len(nodes) + 1) / 2
	return &fileLayoutNode{first: joinFileNodes(nodes[:middle], vertical), second: joinFileNodes(nodes[middle:], vertical), vertical: vertical, share: float32(middle) * 100 / float32(len(nodes))}
}

func (a *app) arrangeFilePanes(mode string) {
	a.ensureFileLayout()
	minimum := 2
	if mode == "grid" {
		minimum = 4
	}
	for len(a.fileLayout.panes()) < minimum {
		a.splitFilePane(a.activeFilePane, false, true)
	}
	var leaves []*fileLayoutNode
	for _, p := range a.fileLayout.panes() {
		leaves = append(leaves, a.fileLeaf(p))
	}
	if mode != "grid" {
		a.fileLayout = joinFileNodes(leaves, mode == "vertical")
		return
	}
	var rows []*fileLayoutNode
	for len(leaves) > 0 {
		count := min(2, len(leaves))
		rows = append(rows, joinFileNodes(leaves[:count], false))
		leaves = leaves[count:]
	}
	a.fileLayout = joinFileNodes(rows, true)
}

func (a *app) fileWindowsMenu(m *ui.Menu, p *filePane) {
	panes := a.fileLayout.panes()
	if m.Item(tr(nil, "向右拆分")).Disabled(len(panes) >= maxFilePanes).Chosen() {
		a.splitFilePane(p, false, false)
	}
	if m.Item(tr(nil, "向下拆分")).Disabled(len(panes) >= maxFilePanes).Chosen() {
		a.splitFilePane(p, true, false)
	}
	if m.Item(tr(nil, "添加本机窗口")).Disabled(len(panes) >= maxFilePanes).Chosen() {
		a.addLocalFileWindow(p)
	}
	m.Separator()
	for _, item := range []struct{ label, mode string }{{tr(nil, "左右排列"), "horizontal"}, {tr(nil, "上下排列"), "vertical"}, {tr(nil, "网格排列"), "grid"}} {
		if m.Item(item.label).Chosen() {
			a.arrangeFilePanes(item.mode)
		}
	}
	m.Separator()
	if m.Item(tr(nil, "仅保留此窗口")).Disabled(len(panes) == 1).Chosen() {
		a.keepFilePane(p)
	}
	if m.Item(tr(nil, "关闭窗口")).Disabled(len(panes) == 1).Chosen() {
		a.closeFilePane(p)
	}
}

func (a *app) fileWindows(c *ui.Context, n *fileLayoutNode) {
	if n.pane != nil {
		a.browserPane(c, n.pane)
		return
	}
	first, second := n.first, n.second
	if n.vertical {
		ui.SplitVerticalPercent(c, &n.share, func() { a.fileWindows(c, first) }, func() { a.fileWindows(c, second) }).FillWidth().Grow(1).MinHeight(0)
	} else {
		ui.SplitPercent(c, &n.share, func() { a.fileWindows(c, first) }, func() { a.fileWindows(c, second) }).FillWidth().Grow(1).MinHeight(0)
	}
}

func (a *app) filePaneLabel(p *filePane) string { return fmt.Sprintf(tr(nil, "窗口%d"), p.id) }

func (a *app) filePaneVisible(p *filePane) bool {
	return a.fileLayout == nil || slices.Contains(a.fileLayout.panes(), p)
}
