package components

import (
	"testing"

	"github.com/egoist/mygo/ui"
)

func TestSidebarSplitPreservesContentIdentityAndInputAcrossWidths(t *testing.T) {
	width, compact, text := float32(220), false, ""
	var contentID uint64
	tt := ui.NewTester(func(c *ui.Context) {
		ApplyTheme(c, false)
		ui.Column(c).Fill().Children(func() {
			SidebarSplit(c, &width, compact, func() { ui.Text(c, "navigation") }, func() {
				contentID = ui.TextInput(c, &text).FillWidth().Label("work input").ID()
			})
		})
	}, 1000, 500)
	id := contentID
	if err := tt.Click("work input"); err != nil {
		t.Fatal(err)
	}
	for _, slim := range []bool{true, false, true, false} {
		compact = slim
		tt.Frame()
		if contentID != id || width != 220 || !tt.Focused("work input") {
			t.Fatal("changing sidebar width rebuilt the content, stole focus or lost the saved width", slim, contentID, id, width)
		}
		tt.Type("x")
		input, _ := tt.Find("work input")
		wantX := float32(221)
		if compact {
			wantX = RailWidth
		}
		if input.X != wantX || input.W != 1000-wantX {
			t.Fatal("content did not use exactly the remaining space", slim, input)
		}
	}
	if text != "xxxx" {
		t.Fatal("content input did not remain live", text)
	}
}

func TestSidebarSplitDisablesOnlyCompactGripAndClampsDrag(t *testing.T) {
	width, compact, clicks := float32(220), false, 0
	tt := ui.NewTester(func(c *ui.Context) {
		ApplyTheme(c, false)
		ui.Column(c).Fill().Children(func() {
			SidebarSplit(c, &width, compact, func() {
				if ui.Button(c, "nav action").Fill().Clicked() {
					clicks++
				}
			}, func() {
				if ui.Button(c, "content action").Fill().Clicked() {
					clicks++
				}
			})
		})
	}, 1000, 500)
	// The overlap on the content side must capture the resize press before
	// the content button under it, including after a width change.
	for i := 0; i < 2; i++ {
		grip, _ := tt.Find("调整连接栏宽度")
		x, y := grip.X+grip.W-1, grip.Y+grip.H/2
		before := width
		tt.Press(x, y)
		tt.Move(x+20, y)
		tt.Release(x+20, y)
		if width != before+20 || clicks != 0 {
			t.Fatal("content covered the divider's right-side hit area", width, before, clicks)
		}
	}
	grip, _ := tt.Find("调整连接栏宽度")
	x, y := grip.X+grip.W/2, grip.Y+grip.H/2
	tt.Press(x, y)
	tt.Move(x+1000, y)
	if width != 400 {
		t.Fatal("drag did not stop at the maximum", width)
	}
	tt.Move(x+999, y)
	if width != 399 {
		t.Fatal("reverse movement jumped or stayed stuck after the limit", width)
	}
	tt.Release(x+999, y)
	if err := tt.Click("调整连接栏宽度"); err != nil {
		t.Fatal(err)
	}
	tt.Key(0, ui.KeyLeft)
	if width != 389 {
		t.Fatal("expanded grip lost keyboard resizing", width)
	}
	compact = true
	tt.Frame()
	grip, _ = tt.Find("调整连接栏宽度")
	x, y = grip.X+grip.W/2, grip.Y+grip.H/2
	tt.Press(x, y)
	tt.Move(x+100, y)
	tt.Release(x+100, y)
	if width != 389 {
		t.Fatal("compact grip changed the expanded width", width)
	}
	beforeClicks := clicks
	tt.ClickAt(RailWidth+2, 250)
	tt.ClickAt(RailWidth-2, 250)
	if clicks != beforeClicks+2 {
		t.Fatal("compact grip intercepted clicks on either pane edge", clicks, beforeClicks)
	}
	for _, label := range []string{"nav action", "content action"} {
		if err := tt.Click(label); err != nil {
			t.Fatal(err)
		}
	}
	if clicks != beforeClicks+4 {
		t.Fatal("disabling the grip disabled pane contents", clicks)
	}
}
