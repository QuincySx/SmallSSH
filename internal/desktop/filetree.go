package desktop

import (
	"sort"
	"sshclient/internal/desktop/components"

	"github.com/egoist/mygo/ui"
)

// The flattened rows are rebuilt only when directory data or expansion changes.
// Scrolling uses the native virtual list and never walks the directory graph.
type directoryTreeIndex struct {
	children  map[string][]string
	ancestry  map[string]string
	rows      []directoryTreeRow
	positions map[string]int
	root      string
	dirty     bool
	selected  int
	list      ui.ListState
	builds    uint64
}
type directoryTreeRow struct {
	path          string
	depth, parent int
	branch        bool
}

func (b *directoryBrowser) treeIndex() *directoryTreeIndex {
	if b.tree == nil {
		b.tree = &directoryTreeIndex{children: map[string][]string{}, ancestry: map[string]string{}, positions: map[string]int{}, dirty: true, selected: -1}
	}
	return b.tree
}
func (b *directoryBrowser) indexDirectory(local bool, dir string) {
	t := b.treeIndex()
	children := make([]string, 0)
	for _, f := range b.cache[dir] {
		if f.Directory {
			children = append(children, joinPath(local, dir, f.Name))
		}
	}
	t.children[dir] = children
	t.dirty = true
	if dir == b.dir {
		b.indexFiles()
	}
}
func (b *directoryBrowser) indexFiles() {
	b.filesByName = make(map[string]int, len(b.cache[b.dir]))
	for i, f := range b.cache[b.dir] {
		b.filesByName[f.Name] = i
	}
}
func (b *directoryBrowser) treeLocation(local bool) {
	t := b.treeIndex()
	clear(t.ancestry)
	root := b.dir
	for {
		parent := parentDir(local, root)
		if parent == root {
			break
		}
		t.ancestry[parent] = root
		root = parent
	}
	t.root = root
	t.dirty = true
	b.indexFiles()
}
func (b *directoryBrowser) directoryChildren(dir string) []string {
	t := b.treeIndex()
	children, exists := t.children[dir]
	if !exists {
		children = nil
	}
	child := t.ancestry[dir]
	if child == "" {
		return children
	}
	for _, entry := range children {
		if entry == child {
			return children
		}
	}
	// Inferred ancestors are usually a single child; only a missing ancestor
	// requires a merged slice. The result is cached with the directory index.
	merged := append([]string(nil), children...)
	merged = append(merged, child)
	sort.Strings(merged)
	return merged
}
func (b *directoryBrowser) visibleTree(local bool) []directoryTreeRow {
	t := b.treeIndex()
	if !t.dirty {
		return t.rows
	}
	if t.root == "" {
		b.treeLocation(local)
	}
	// Compatibility for restored caches: indexing happens once, never per frame.
	for dir := range b.cache {
		if _, ok := t.children[dir]; !ok {
			b.indexDirectory(local, dir)
		}
	}
	t.rows = t.rows[:0]
	clear(t.positions)
	var appendNode func(string, int, int)
	appendNode = func(dir string, depth, parent int) {
		if depth > 128 {
			return
		}
		children := b.directoryChildren(dir)
		_, loaded := b.cache[dir]
		index := len(t.rows)
		t.positions[dir] = index
		t.rows = append(t.rows, directoryTreeRow{path: dir, depth: depth, parent: parent, branch: !loaded || len(children) > 0})
		if open := b.open[dir]; open != nil && *open {
			for _, child := range children {
				appendNode(child, depth+1, index)
			}
		}
	}
	appendNode(t.root, 0, -1)
	if index, ok := t.positions[b.dir]; ok {
		t.selected = index
	} else {
		t.selected = -1
	}
	t.dirty = false
	t.builds++
	return t.rows
}
func (b *directoryBrowser) setExpanded(dir string, expanded bool) {
	if b.open[dir] == nil {
		value := expanded
		b.open[dir] = &value
	} else {
		*b.open[dir] = expanded
	}
	b.treeIndex().dirty = true
}

func (a *app) directoryTree(c *ui.Context, p *filePane, b *directoryBrowser) {
	local := p.endpoint == localEndpoint
	rows := b.visibleTree(local)
	t := b.treeIndex()
	t.list.Selected = &t.selected
	components.DirectoryTree(c, &t.list, len(rows), func(i int) components.TreeItem {
		r := rows[i]
		name := baseName(local, r.path)
		if r.depth == 0 {
			name = r.path
		}
		return components.TreeItem{ID: r.path, Name: reviewText(name), Depth: r.depth, Parent: r.parent, Branch: r.branch, Expanded: isExpanded(b, r.path)}
	}, components.TreeOptions{
		Label:    a.paneSide(p) + tr(c, "目录树"),
		OnSelect: func(i int) { a.navigateDirectory(p, rows[i].path) },
		OnToggle: func(i int) { a.toggleDirectory(p, b, rows[i].path) },
		Decorate: func(row ui.Element, i int) {
			path := rows[i].path
			row.ContextMenu(func(m *ui.Menu) { a.treeContextMenu(m, p, b, path) })
			a.fileDropTarget(c, row, p, path)
		},
	})
}
func isExpanded(b *directoryBrowser, dir string) bool { return b.open[dir] != nil && *b.open[dir] }
func (a *app) toggleDirectory(p *filePane, b *directoryBrowser, dir string) {
	open := b.open[dir] != nil && *b.open[dir]
	b.setExpanded(dir, !open)
	if !open {
		if _, loaded := b.cache[dir]; !loaded {
			a.loadDirectory(p, dir, false)
		}
	} else if pending := b.loading[dir]; pending != nil && !pending.navigate {
		pending.cancel()
		delete(b.loading, dir)
	}
}
