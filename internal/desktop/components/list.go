package components

import "github.com/egoist/mygo/ui"

// Table preserves keyed selection, column resizing/reordering, keyboard
// navigation and virtualization. The caller supplies domain-specific ordering
// through state.Sort; header changes always request a newly sorted frame.
func Table(c *ui.Context, state *ui.ListState, columns []ui.TableColumn, count int, cell func(int, int)) ui.Element {
	before := ui.SortOrder{}
	if state.Sort != nil {
		before = *state.Sort
	}
	table := ui.Table(c, state, columns, count, cell).MinHeight(0)
	if state.Sort != nil && before != *state.Sort {
		state.ScrollTo(0, ui.Start)
		c.Invalidate()
	}
	return table
}

func List(c *ui.Context, state *ui.ListState, count int, row func(int)) ui.Element {
	return ui.List(c, state, count, row).MinHeight(0)
}

func CellText(c *ui.Context, text string) ui.Element {
	return ui.Text(c, text).SingleLine().FontSize(DetailFont).Tooltip(text)
}
