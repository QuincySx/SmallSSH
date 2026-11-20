package components

import (
	"github.com/egoist/mygo/ui"
	"sshclient/docs/design"
)

// Tabs shares the workbench's rounded selection without an underline, while
// retaining native tab roles, roving focus and arrow-key selection.
func Tabs(c *ui.Context, selected *int, labels ...string) ui.Element {
	tabs := ui.TabsBase(c, selected, len(labels))
	tabs.List.Gap(2).AlignItems(ui.Center).Children(func() {
		for i, label := range labels {
			tab := tabs.Tab(i).Height(design.Length("size-task-tab")).Padding(0, 10).Radius(8).Label(label).
				TextColor(c.Theme().TextMuted)
			if i == *selected {
				tab.TextColor(c.Theme().Accent).Background(ui.Hex(design.Color(c.Theme().Dark, "color-native-tab-selected")))
			} else if tab.Hovered() {
				tab.Background(c.Theme().SurfaceHover)
			}
			tab.Children(func() { HeaderLabel(c, label) })
		}
	})
	return tabs.List
}
