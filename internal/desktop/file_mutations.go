package desktop

import (
	"fmt"
	"io"
	"path/filepath"
	"strings"

	"sshclient/internal/approval"
	"sshclient/internal/client"
	"sshclient/internal/sshconn"
	"sshclient/internal/vault"
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

// confirmRemoveFiles snapshots both the endpoint and each entry before a
// confirmation can be displayed or another pane can change selection.
func (a *app) confirmRemoveFiles(endpoint string, browser *directoryBrowser, dir string, files []sshconn.File) {
	if len(files) == 0 {
		return
	}
	local := endpoint == localEndpoint
	entries := append([]sshconn.File(nil), files...)
	names := make([]string, 0, len(entries))
	for _, file := range entries {
		if file.Name == "" || file.Name == "." || file.Name == ".." || strings.ContainsAny(file.Name, "/\\\x00") {
			a.status = tr(nil, "文件名称无效")
			return
		}
		target := joinPath(local, dir, file.Name)
		if parentDir(local, target) == target {
			a.status = tr(nil, "不能修改或删除根目录")
			return
		}
		names = append(names, reviewText(file.Name))
	}
	message := fmt.Sprintf(tr(nil, "将删除 %d 项；目录必须为空。"), len(entries)) + "\n" + strings.Join(names, "\n") + "\n\n" + tr(nil, "删除后无法从客户端恢复；目录必须为空。")
	target := dir
	if len(entries) == 1 {
		target = joinPath(local, dir, entries[0].Name)
	}
	a.ask(tr(nil, "删除文件或空文件夹？"), a.endpointName(endpoint)+" · "+reviewText(target), message, "", "", tr(nil, "删除"), func(string) {
		if a.busy {
			return
		}
		ctx, gen := a.operationContext(), a.generation
		var owner *vault.Session
		if !local {
			var err error
			owner, err = a.unlockedOwner()
			if err != nil {
				a.status = err.Error()
				return
			}
		}
		cli := a.client
		a.work(func() (func(), error) {
			type result struct {
				file sshconn.File
				err  error
			}
			results := make([]result, 0, len(entries))
			for _, file := range entries {
				name := joinPath(local, dir, file.Name)
				var err error
				if err = ctx.Err(); err != nil {
					// Stop issuing new operations after the session ends.
				} else if local {
					err = mutateLocal("remove", name, "", 0)
				} else {
					var stream *client.Stream
					stream, err = cli.StartWithOwner(ctx, approval.Operation{Host: endpoint, Kind: "remove", Path: name}, owner)
					if err == nil {
						_, _, err = stream.Copy(io.Discard, io.Discard)
						stream.Close()
					}
				}
				results = append(results, result{file, err})
			}
			return func() {
				if a.generation != gen {
					return
				}
				var failures []string
				success := 0
				for _, result := range results {
					if result.err != nil {
						failures = append(failures, reviewText(result.file.Name)+": "+reviewText(fileMutationError(result.err)))
						continue
					}
					success++
					name := joinPath(local, dir, result.file.Name)
					a.fileMutationCompleted(endpoint, browser, dir, "remove", name, "", 0, result.file.Directory)
					if browser.dir == dir {
						browser.chosen.Remove(result.file.Name)
					}
				}
				if browser.dir == dir {
					browser.selected = -1
					if remaining := chosenFiles(browser); len(remaining) > 0 {
						browser.selected = browser.filesByName[remaining[0].Name]
					}
				}
				a.status = fmt.Sprintf(tr(nil, "已删除 %d/%d 项"), success, len(results))
				if len(failures) > 0 {
					a.status += " · " + tr(nil, "删除失败") + ": " + strings.Join(failures, "; ")
				}
			}, nil
		})
	})
	a.prompt.danger = true
}

func fileMutationError(err error) string {
	text := err.Error()
	lower := strings.ToLower(text)
	if strings.Contains(lower, "directory not empty") || strings.Contains(lower, "directory is not empty") || strings.Contains(text, "目录不是空的") {
		return tr(nil, "目录不是空的")
	}
	if strings.Contains(text, "无法删除目录，请检查目录是否为空及权限") {
		return tr(nil, "无法删除目录，请检查目录是否为空及权限")
	}
	return text
}
