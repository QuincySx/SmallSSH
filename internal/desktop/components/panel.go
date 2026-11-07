package components

import "github.com/egoist/mygo/ui"

type PanelTab struct {
	ID    int
	Title string
}

type PanelBarOptions struct {
	Leading func()
	Tab     func(ui.Element, int)
}

// HeaderBar gives adjacent panes the same baseline and keeps leading actions
// at the same pointer position when a pane is collapsed or restored.
func HeaderBar(c *ui.Context) ui.Element {
	return ui.Row(c).FillWidth().Height(HeaderHeight).Shrink(0).Padding(0, HeaderInset).Gap(0).
		AlignItems(ui.Center)
}

// HeaderLabel optically aligns the visible glyphs with the SVG actions.
// The native line box includes descender space below these short labels;
// adjusting the paint position preserves layout, focus and hit targets.
func HeaderLabel(c *ui.Context, text string) ui.Element {
	return ui.Text(c, text).FontSize(BodyFont).SingleLine().Top(-1)
}

// PanelBar keeps the familiar DevTools text-tab layout while retaining native
// tab semantics, arrow keys and focus. Persistent IDs need not be contiguous.
func PanelBar(c *ui.Context, selected *int, items []PanelTab, actions func(), options ...PanelBarOptions) ui.Element {
	return HeaderBar(c).Gap(2).Label(tr(c, "工具栏标题")).Children(func() {
		if len(options) > 0 && options[0].Leading != nil {
			options[0].Leading()
		}
		index := 0
		for i, item := range items {
			if item.ID == *selected {
				index = i
			}
		}
		tabs := ui.TabsBase(c, &index, len(items))
		tabs.List.Grow(1).Height(HeaderHeight).Gap(0).AlignItems(ui.Center).Label(tr(c, "工具选项卡")).Children(func() {
			for i, item := range items {
				tab := tabs.Tab(i).Height(28).Padding(0, 12).Radius(8).Label(item.Title).
					TextColor(c.Theme().TextMuted)
				if i == index {
					tab.TextColor(c.Theme().Accent).Background(c.Theme().Accent.Alpha(.10))
				} else if tab.Hovered() {
					tab.TextColor(c.Theme().Text).Background(c.Theme().Border.Alpha(.35))
				}
				tab.Children(func() { HeaderLabel(c, item.Title) })
				if len(options) > 0 && options[0].Tab != nil {
					options[0].Tab(tab, item.ID)
				}
			}
		})
		if tabs.List.Changed() && index >= 0 && index < len(items) {
			*selected = items[index].ID
		}
		if actions != nil {
			ui.Row(c).Shrink(0).Gap(2).AlignItems(ui.Center).Children(actions)
		}
	})
}
