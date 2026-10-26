package components

import (
	"math"

	"github.com/egoist/mygo/ui"
)

// Surface and the metric primitives are shared by host and container resources.
func Surface(c *ui.Context, label string) ui.Element {
	return ui.Column(c).FillWidth().MinWidth(0).Padding(9).Gap(7).Radius(7).
		Background(c.Theme().Surface).Border(1, c.Theme().Border.Alpha(.6)).Label(label)
}

// ResourceColumns fits complete cards, including their gaps, before adding a
// column. ResourceGrid also limits columns to the number of actual cards.
func ResourceColumns(width, minimumWidth float32, maximum int) int {
	return min(max(1, maximum), max(1, int((width+8)/(max(1, minimumWidth)+8))))
}

// ResourceLayout gives each kind of card its own readable width and density
// limit. Static and virtual grids share the same policy.
type ResourceLayout struct {
	MinimumWidth   float32
	MaximumColumns int
}

func (l ResourceLayout) Columns(width float32) int {
	return ResourceColumns(width, l.MinimumWidth, l.MaximumColumns)
}

func (l ResourceLayout) ColumnsFor(width float32, count int) int {
	return min(max(1, count), l.Columns(width))
}

func (l ResourceLayout) CellWidth(width float32, count int) float32 {
	columns := l.ColumnsFor(width, count)
	return max(0, width-8*float32(columns-1)) / float32(columns)
}

func (l ResourceLayout) Grid(c *ui.Context, width float32, count int, cell func(int)) ui.Element {
	return ResourceGrid(c, width, l.MinimumWidth, l.MaximumColumns, count, cell)
}

// ResourceGrid contains each percentage-width Surface in a grid cell so its
// width resolves against that cell, rather than the full grid.
func ResourceGrid(c *ui.Context, width, minimumWidth float32, maximum, count int, cell func(int)) ui.Element {
	columns := min(max(1, count), ResourceColumns(width, minimumWidth, maximum))
	return ResourceRow(c, columns, count, cell)
}

// ResourceRow renders a virtualized collection's row using the column count
// calculated for the whole collection, so its last row keeps the same alignment.
func ResourceRow(c *ui.Context, columns, count int, cell func(int)) ui.Element {
	return ui.Grid(c).FillWidth().MinWidth(0).Columns(max(1, columns)).Gap(8).Children(func() {
		for i := range count {
			ui.Column(c).MinWidth(0).Children(func() { cell(i) })
		}
	})
}

func Metric(c *ui.Context, label, value string) ui.Element {
	return ui.Column(c).MinWidth(0).Gap(4).Children(func() {
		ui.Text(c, label).FontSize(10).TextColor(c.Theme().TextMuted)
		ui.Text(c, value).FontSize(12).SingleLine().Tooltip(value)
	})
}

type MeterPart struct {
	Value float64
	Color ui.Color
}

func SegmentedMeter(c *ui.Context, parts []MeterPart) ui.Element {
	bg := c.Theme().Border
	return ui.Box(c).FillWidth().Height(9).Draw(func(p *ui.Painter, r ui.Rect) {
		const count = 24
		step := r.W / count
		for n := range count {
			color := bg
			threshold := float64(n) * 100 / count
			var total float64
			for _, part := range parts {
				total += max(0, part.Value)
				if threshold < total {
					color = part.Color
					break
				}
			}
			p.Fill(ui.Rect{X: r.X + float32(n)*step, Y: r.Y, W: max(1, step-2), H: r.H}, color, 2)
		}
	})
}

func Ring(c *ui.Context, value float64, label string, color ui.Color) ui.Element {
	bg := c.Theme().Border
	return ui.Box(c).Size(56, 56).Center().Draw(func(p *ui.Painter, r ui.Rect) {
		cx, cy, radius := r.X+r.W/2, r.Y+r.H/2, min(r.W, r.H)/2-4
		var circle ui.Path
		circle.Circle(cx, cy, radius)
		p.StrokePath(&circle, 4, bg)
		if value <= 0 || math.IsNaN(value) || math.IsInf(value, 0) {
			return
		}
		var arc ui.Path
		for n := 0; n <= 64; n++ {
			angle := -math.Pi/2 + 2*math.Pi*min(100, value)/100*float64(n)/64
			x, y := cx+radius*float32(math.Cos(angle)), cy+radius*float32(math.Sin(angle))
			if n == 0 {
				arc.MoveTo(x, y)
			} else {
				arc.LineTo(x, y)
			}
		}
		p.StrokePath(&arc, 4, color)
	}).Children(func() { ui.Text(c, label).FontSize(11) })
}

func (l ResourceLayout) CardGrid(c *ui.Context, state *ui.GridState, width float32, count int, height float32, cell func(int)) ui.Element {
	// GridView fits columns from its previous measured width. Bound both
	// sides of a resize, using the current pane width and committed bounds.
	// Keep the native virtual grid in charge of scrolling and selection.
	committedWidth := state.Bounds(c).W
	if committedWidth <= 0 {
		// GridView itself starts from the window width before it has rows.
		committedWidth, _ = c.Size()
	}
	width = max(width, committedWidth)
	gap := c.Theme().Space(2)
	limit := min(max(1, count), max(1, l.MaximumColumns))
	// Stay just above the threshold for one column beyond the limit. Unlike
	// forcing an exact column width, this tolerates pane padding and rounding
	// and still lets GridView request a new frame when the columns change.
	minimum := max(max(1, l.MinimumWidth), (width+gap)/float32(limit+1)-gap+1)
	return ui.GridView(c, state, count, minimum, height, cell).Gap(10).MinHeight(0)
}
