package components

import (
	"strings"

	"github.com/egoist/mygo/ui"
)

// EditableChoice keeps typed values on blur, unlike a selection-only combobox.
// Native ComboboxBase owns popup focus, arrows, Escape and option selection.
func EditableChoice(c *ui.Context, value *string, choices []string, label string, disabled bool) ui.Element {
	var cb ui.ComboboxParts
	field := ui.Row(c).FillWidth().MinWidth(0).Height(FormHeight).Radius(5).
		Background(c.Theme().Surface).Border(1, c.Theme().Border).AlignItems(ui.Center).Children(func() {
		cb = ui.ComboboxBase(c, value)
		cb.Input.Grow(1).MinWidth(0).Height(FormHeight-2).Padding(6, 9).Label(label).Disabled(disabled)
		if SVGButton(c, ChevronDownIcon, tr(c, "选择")+label).Size(30, FormHeight-4).Disabled(disabled).Clicked() {
			cb.SetOpen(!cb.Open())
			cb.Input.Focus()
		}
	})
	if cb.Input.Focused() {
		field.Border(1, c.Theme().Accent)
	}
	cb.Popup(func(panel ui.Element) {
		panel.Padding(3).MaxHeight(210).Background(c.Theme().Background).Border(1, c.Theme().Border).Radius(5)
		ui.Scroll(c).FillWidth().MaxHeight(200).Children(func() {
			ui.Column(c).FillWidth().Gap(0).Children(func() {
				query := strings.ToLower(*value)
				for _, choice := range choices {
					if cb.Filtering() && !strings.Contains(strings.ToLower(choice), query) {
						continue
					}
					item := cb.Item(choice).Padding(6, 9).FillWidth()
					if item.Highlighted() {
						item.Background(c.Theme().Accent.Alpha(.12))
					}
					item.Children(func() { ui.Text(c, choice).SingleLine().FontSize(BodyFont) })
				}
			})
		})
	})
	if selected, ok := cb.Chosen(); ok {
		*value = selected
	}
	return cb.Input
}
