package components

import "github.com/egoist/mygo/ui"

// SidebarSplit changes only widths and the grip's enabled state. Both panes
// keep the same parent and identity when navigation becomes compact.
func SidebarSplit(c *ui.Context, savedWidth *float32, compact bool, sidebar, content func()) ui.Element {
	windowWidth, _ := c.Size()
	maximum := min(float32(400), max(float32(140), windowWidth-301))
	width := *savedWidth
	if width <= 0 {
		width = 190
	}
	width = max(140, min(maximum, width))
	if compact {
		width = RailWidth
	}
	return ui.Row(c).FillWidth().Grow(1).MinHeight(0).AlignItems(ui.Stretch).Children(func() {
		left := ui.Column(c).Key("navigation-pane").Width(width).Shrink(0).FillHeight().MinHeight(0)
		left.Children(sidebar)
		lineWidth := float32(1)
		if compact {
			lineWidth = 0
		}
		ui.Box(c).Key("navigation-divider").Width(lineWidth).Shrink(0).FillHeight().Background(c.Theme().Border)
		ui.Column(c).Key("workspace-pane").FillHeight().Grow(1).MinWidth(0).MinHeight(0).Children(content)
		// Submit the hit target after both panes so its overlap remains usable
		// from either side of the divider rather than being covered by content.
		c.Key("navigation-resize-handle")
		grip := ui.Box(c).Absolute().Top(0).Bottom(0).Start(width - 3).Width(7).
			Disabled(compact).Label(tr(c, "调整连接栏宽度"))
		if compact {
			grip.PassThrough()
			return
		}
		grip.Focusable().Role(ui.RoleSplitter).Range(140, float64(maximum), float64(width)).Cursor(ui.CursorResizeEW)
		next := width
		if dx, _, dragging := grip.Dragged(); dragging {
			next += dx
		}
		if grip.Shortcut(0, ui.KeyLeft) {
			next -= 10
		}
		if grip.Shortcut(0, ui.KeyRight) {
			next += 10
		}
		next = max(140, min(maximum, next))
		if next != width {
			*savedWidth = next
			left.Width(next)
			grip.Start(next - 3)
			c.Invalidate()
		}
	})
}
