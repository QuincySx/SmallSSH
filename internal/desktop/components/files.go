package components

import (
	"fmt"
	"time"

	"github.com/egoist/mygo/ui"
)

// FileEntry is presentation data, independent of SFTP and local filesystem IO.
type FileEntry struct {
	ID        string
	Name      string
	Directory bool
	Bytes     uint64
	Modified  time.Time
}

type FileListOptions struct {
	Label        string
	ShowModified bool
	// Decorate binds context menus, selection, drag/drop and double-clicks to
	// the actual cell. The caller snapshots its operation target as usual.
	Decorate func(ui.Element, int)
}

func FileList(c *ui.Context, state *ui.ListState, count int, entry func(int) FileEntry, o FileListOptions) ui.Element {
	defer fileViewTheme(c)()
	state.Key = func(i int) any {
		f := entry(i)
		if f.ID != "" {
			return f.ID
		}
		return f.Name
	}
	state.Label = func(i int) string { return entry(i).Name }
	columns := []ui.TableColumn{{ID: "name", Title: tr(c, "名称"), MinWidth: 100}, {ID: "size", Title: tr(c, "大小"), Width: 72, Align: ui.End}}
	if o.ShowModified {
		columns = append(columns, ui.TableColumn{ID: "modified", Title: tr(c, "修改时间"), Width: 130})
	}
	return Table(c, state, columns, count, func(i, col int) {
		f := entry(i)
		selected := state.Selected != nil && *state.Selected == i
		if selection, ok := state.Selection.(*ui.Selection[string]); ok {
			key := f.ID
			if key == "" {
				key = f.Name
			}
			selected = selection.Has(key)
		}
		cell := ui.Row(c).Fill().MinWidth(0).AlignItems(ui.Center)
		if col == 1 {
			cell.Justify(ui.End)
		}
		cell.Children(func() {
			switch col {
			case 0:
				FileName(c, f, FileNameOptions{Selected: selected})
			case 1:
				text := "—"
				if !f.Directory {
					text = Bytes(f.Bytes)
				}
				CellText(c, text).TextColor(c.Theme().TextMuted)
			case 2:
				text := "—"
				if !f.Modified.IsZero() {
					text = f.Modified.Local().Format("2006-01-02 15:04")
				}
				CellText(c, text).TextColor(c.Theme().TextMuted)
			}
		})
		if o.Decorate != nil {
			o.Decorate(cell, i)
		}
	}).Fill().Label(o.Label)
}

func Bytes(n uint64) string {
	for _, unit := range []struct {
		threshold uint64
		label     string
	}{{1 << 30, "GiB"}, {1 << 20, "MiB"}, {1 << 10, "KiB"}} {
		if n >= unit.threshold {
			return fmt.Sprintf("%.1f %s", float64(n)/float64(unit.threshold), unit.label)
		}
	}
	return fmt.Sprintf("%d B", n)
}
