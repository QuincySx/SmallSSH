package components

import (
	"fmt"
	"math"
	"testing"

	"github.com/egoist/mygo/ui"
)

func TestResourceGridsFillWidthBeforeAddingColumnsAndAvoidEmptyColumns(t *testing.T) {
	for _, virtual := range []bool{false, true} {
		t.Run(fmt.Sprint(virtual), func(t *testing.T) {
			layout := ResourceLayout{MinimumWidth: 200, MaximumColumns: 3}
			count := 1
			var state ui.GridState
			label := func(i int) string { return fmt.Sprintf("card-%d", i) }
			tt := ui.NewTester(func(c *ui.Context) {
				width, _ := c.Size()
				cell := func(i int) {
					Surface(c, label(i)).Height(64).Children(func() { ui.Text(c, label(i)) })
				}
				ui.Column(c).Fill().Children(func() {
					if virtual {
						layout.CardGrid(c, &state, width, count, 80, cell).FillWidth().Grow(1).Label("collection")
					} else {
						layout.Grid(c, width, count, cell).Label("collection")
					}
				})
			}, 1600, 380)
			for _, config := range []struct{ width, count, columns int }{
				{1600, 1, 1}, {1600, 2, 2}, {1600, 3, 3},
				{407, 3, 1}, {408, 3, 2}, {615, 3, 2}, {616, 3, 3},
				{190, 3, 1}, {1600, 1, 1}, {1600, 0, 1}, {1600, 2, 2},
			} {
				count = config.count
				tt.SetSize(config.width, 380)
				if count == 0 {
					if _, found := tt.Find(label(0)); found {
						t.Fatal("empty collection retained a card")
					}
					continue
				}
				collection, _ := tt.Find("collection")
				first, _ := tt.Find(label(0))
				expected := (collection.W - 8*float32(config.columns-1)) / float32(config.columns)
				for i := range count {
					r, found := tt.Find(label(i))
					// The native virtual grid has two DIPs of cell padding per side.
					if !found || math.Abs(float64(r.W-expected)) > 5 || (r.Y == first.Y) != (i < config.columns) {
						t.Fatal("cards reserved empty columns or split before two minimum widths fit", config, i, collection, r)
					}
				}
			}
		})
	}
}

func TestResourceCardGridCapsDensityWithoutLosingVirtualizationOrSelection(t *testing.T) {
	layout := ResourceLayout{MinimumWidth: 400, MaximumColumns: 2}
	selected, accesses := -1, 0
	var chosen ui.Selection[int]
	state := ui.GridState{Selected: &selected, Selection: &chosen, Key: func(i int) any { return i }}
	label := func(i int) string { return fmt.Sprintf("resource-%06d", i) }
	tt := ui.NewTester(func(c *ui.Context) {
		width, _ := c.Size()
		ui.Row(c).Fill().Children(func() {
			ui.Box(c).Width(190)
			layout.CardGrid(c, &state, width-190, 100000, 120, func(i int) {
				accesses++
				Surface(c, label(i)).FillHeight().Children(func() { ui.Text(c, label(i)) })
			}).Grow(1).FillHeight()
		})
	}, 1280, 440)
	if err := tt.Click(label(0)); err != nil {
		t.Fatal(err)
	}
	tt.Key(0, ui.KeyRight)
	for _, config := range []struct{ width, columns int }{{2560, 2}, {997, 1}, {1006, 2}, {560, 1}, {1280, 2}} {
		tt.SetSize(config.width, 440)
		first, _ := tt.Find(label(0))
		for i := range 3 {
			r, found := tt.Find(label(i))
			if !found || r.W < 300 || r.X+r.W > float32(config.width)+1 || (r.Y == first.Y) != (i < config.columns) {
				t.Fatal("virtual cards ignored readable width or density limit", config, i, first, r)
			}
		}
		if selected != 1 || chosen.Len() != 1 || !chosen.Has(1) {
			t.Fatal("resize lost keyed selection", config, selected, chosen.Len())
		}
	}
	tt.Key(0, ui.KeyEnd)
	if selected != 99999 || !tt.HasText(label(99999)) {
		t.Fatal("keyboard navigation did not reveal the final virtual card", selected)
	}
	accesses = 0
	tt.Frame()
	if accesses > 200 {
		t.Fatal("card grid rendered the complete collection", accesses)
	}
}
