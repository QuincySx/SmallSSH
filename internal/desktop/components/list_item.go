package components

import "github.com/egoist/mygo/ui"

// ListItem is a naturally sized, keyboard-activatable choice with room for
// multi-line content. It does not inherit the fixed height of an action button.
// The stable key is bound before reading pointer state; callers must not re-key
// the returned element. The caller supplies content and the selection action.
func ListItem(c *ui.Context, key any, label string, selected bool) ui.Element {
	t := c.Theme()
	item := ui.ButtonBase(c).Key(key).FillWidth().MinWidth(0).MinHeight(56).Padding(10, 12).
		Justify(ui.Start).AlignItems(ui.Center).Radius(8).Role(ui.RoleListItem).
		Selected(selected).Label(label)
	background := ui.Transparent
	if selected {
		background = t.Accent.Alpha(.09)
	}
	if item.Pressed() {
		background = t.SurfacePressed
	} else if item.Hovered() {
		background = t.SurfaceHover
	}
	return item.Background(background).TextColor(t.Text)
}
