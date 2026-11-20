package components

import "github.com/egoist/mygo/ui"

// Segmented keeps MyGo's native radio-group roles, roving focus and arrow
// keys, but marks the chosen option with the workbench's accent selection
// (the same tint and text colour as Tabs and the settings categories). The
// stock pressed grey was barely distinguishable from the track in either
// theme, so the current choice could not be read at a glance.
func Segmented(c *ui.Context, selected *int, labels ...string) ui.Element {
	parts := ui.SegmentedBase(c, selected, len(labels))
	track := parts.Track.AlignItems(ui.Stretch).Padding(2).Gap(2).Radius(8).
		Background(c.Theme().Surface).Border(1, c.Theme().Border)
	track.Children(func() {
		for i, label := range labels {
			segment := parts.Segment(i).Padding(0, 12).Radius(6).AlignItems(ui.Center).Justify(ui.Center).
				TextColor(c.Theme().TextMuted)
			switch {
			case i == *selected:
				segment.TextColor(c.Theme().Accent).Background(c.Theme().Accent.Alpha(.10))
			case segment.Pressed():
				segment.Background(c.Theme().SurfacePressed)
			case segment.Hovered():
				segment.Background(c.Theme().SurfaceHover)
			}
			// The theme's text size, matching the select and button beside it.
			segment.Children(func() { ui.Text(c, label).SingleLine() })
		}
	})
	return track
}
