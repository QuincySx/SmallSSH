package components

import "github.com/egoist/mygo/ui"

// Table preserves keyed selection, column resizing/reordering, keyboard
// navigation and virtualization. The caller supplies domain-specific ordering
// through state.Sort; header changes always request a newly sorted frame.
func Table(c *ui.Context, state *ui.ListState, columns []ui.TableColumn, count int, cell func(int, int)) ui.Element {
	// Scope the border-free header to the table chrome. Cells retain their
	// normal palette, including embedded controls and keyboard focus rings.
	original := c.Theme()
	chrome := *original
	chrome.Border = ui.Transparent
	c.SetTheme(&chrome)
	defer c.SetTheme(original)
	before := ui.SortOrder{}
	if state.Sort != nil {
		before = *state.Sort
	}
	table := ui.Table(c, state, columns, count, func(row, column int) {
		c.SetTheme(original)
		defer c.SetTheme(&chrome)
		cell(row, column)
	}).MinHeight(0)
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

// ReadableSelection keeps secondary audit/file metadata readable on the chosen
// row while native list selection, keyboard focus and accessibility stay intact.
func ReadableSelection(c *ui.Context) func() {
	original := c.Theme()
	theme := *original
	amount := float32(.89)
	if theme.Dark {
		amount = .76
	}
	theme.Accent = original.Accent.Mix(original.Background, amount)
	theme.AccentText = original.Text
	theme.InactiveAccent = theme.Accent
	c.SetTheme(&theme)
	return func() { c.SetTheme(original) }
}
