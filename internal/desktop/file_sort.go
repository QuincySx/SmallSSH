package desktop

import (
	"cmp"
	"slices"
	"strings"

	"sshclient/internal/sshdata"
)

// List data and selection share the same order, independently of the tree.
func (b *directoryBrowser) indexFiles() {
	b.fileRows = b.cache[b.dir]
	order := b.fileSort
	b.indexedSort = order
	if order.Column != "" {
		b.fileRows = slices.Clone(b.fileRows)
		slices.SortStableFunc(b.fileRows, func(a, b sshdata.File) int {
			if a.Directory != b.Directory {
				if a.Directory {
					return -1
				}
				return 1
			}
			result := 0
			switch order.Column {
			case "size":
				if !a.Directory {
					result = cmp.Compare(a.Size, b.Size)
				}
			case "modified":
				result = a.Modified.Compare(b.Modified)
			case "name":
				result = strings.Compare(strings.ToLower(a.Name), strings.ToLower(b.Name))
			}
			if order.Descending {
				return -result
			}
			return result
		})
	}
	b.filesByName = make(map[string]int, len(b.fileRows))
	for i, f := range b.fileRows {
		b.filesByName[f.Name] = i
	}
}

func (b *directoryBrowser) displayedFiles() []sshdata.File {
	if b.indexedSort != b.fileSort || len(b.fileRows) != len(b.cache[b.dir]) {
		nameAt := func(i int) string {
			if i >= 0 && i < len(b.fileRows) {
				return b.fileRows[i].Name
			}
			return ""
		}
		selected, anchor := nameAt(b.selected), nameAt(b.anchor)
		b.indexFiles()
		indexOf := func(name string) int {
			if i, ok := b.filesByName[name]; ok {
				return i
			}
			return -1
		}
		b.selected, b.anchor = indexOf(selected), indexOf(anchor)
	}
	return b.fileRows
}
