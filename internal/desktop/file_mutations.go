package desktop

import (
	"path/filepath"
	"strings"
)

func directoryContains(local bool, parent, name string) bool {
	if !local {
		return name == parent || strings.HasPrefix(name, strings.TrimSuffix(parent, "/")+"/")
	}
	rel, err := filepath.Rel(parent, name)
	return err == nil && !filepath.IsAbs(rel) && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}

// Reconcile every browser of the same endpoint, including fixed extra panes.
// Cached descendants of moved/removed directories cannot remain navigable.
func (a *app) fileMutationCompleted(endpoint string, origin *directoryBrowser, dir, kind, name, dest string, mode uint32, isDir bool) {
	local := endpoint == localEndpoint
	seen := map[*directoryBrowser]bool{}
	browsers := []*directoryBrowser{origin}
	for _, p := range a.allFilePanes() {
		if b := p.states[endpoint]; b != nil {
			browsers = append(browsers, b)
		}
	}
	for _, b := range browsers {
		if b == nil || seen[b] {
			continue
		}
		seen[b] = true
		destinationParent := parentDir(local, dest)
		for path, request := range b.loading {
			if path == dir || kind == "rename" && path == destinationParent || isDir && (kind == "remove" || kind == "rename") && directoryContains(local, name, path) {
				request.cancel()
				delete(b.loading, path)
			}
		}
		// A fixed second pane may only have loaded the destination. Without
		// source metadata, reload its listing instead of inventing an entry.
		_, sourceLoaded := b.cache[dir]
		if kind == "rename" && dir != destinationParent && !sourceLoaded {
			delete(b.cache, destinationParent)
			b.tree = nil
			b.indexFiles()
		}
		for path := range b.loadError {
			if path == dir || kind == "rename" && path == destinationParent || isDir && (kind == "remove" || kind == "rename") && directoryContains(local, name, path) {
				delete(b.loadError, path)
			}
		}
		if _, loaded := b.cache[dir]; loaded {
			updateMutationCache(b, local, dir, kind, name, dest, mode)
		}
		if isDir && (kind == "remove" || kind == "rename") {
			for path := range b.cache {
				if directoryContains(local, name, path) {
					delete(b.cache, path)
				}
			}
			for path := range b.open {
				if directoryContains(local, name, path) {
					delete(b.open, path)
				}
			}
			if directoryContains(local, name, b.dir) {
				current := dir
				if kind == "rename" {
					rel := strings.TrimPrefix(b.dir, name)
					current = dest + rel
				}
				for path, request := range b.loading {
					if request.navigate {
						request.cancel()
						delete(b.loading, path)
					}
				}
				b.seq++
				b.dir, b.address = current, current
				b.chosen.Clear()
				b.selected, b.anchor = -1, -1
			}
			b.tree = nil
			b.reveal(local, b.dir)
		}
	}
}
