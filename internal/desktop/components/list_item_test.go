package components

import (
	"testing"

	"github.com/egoist/mygo/ui"
)

func TestListItemFitsTwoLinesAndKeepsNativeActivation(t *testing.T) {
	for _, dark := range []bool{false, true} {
		clicks := 0
		tt := ui.NewTester(func(c *ui.Context) {
			ui.Column(c).FillWidth().Padding(8).Gap(8).Children(func() {
				if ListItem(c, "client", "选择客户端", true).Children(func() {
					ui.Column(c).FillWidth().MinWidth(0).Gap(4).Children(func() {
						ui.Text(c, "我的 AI 助手").SingleLine().FontSize(13).Bold()
						ui.Text(c, "执行命令 · 测试服务器").SingleLine().FontSize(11)
					})
				}).Clicked() {
					clicks++
				}
				Button(c, "普通操作")
			})
		}, 240, 180)
		tt.SetDark(dark)
		row, ok := tt.Find("选择客户端")
		if !ok || row.H < 56 {
			t.Fatal("multi-line choice inherited an action-button height", row)
		}
		for _, label := range []string{"我的 AI 助手", "执行命令 · 测试服务器"} {
			text, ok := tt.Find(label)
			if !ok || text.X < row.X || text.Y < row.Y || text.X+text.W > row.X+row.W+1 || text.Y+text.H > row.Y+row.H+1 {
				t.Fatal("choice clipped its content", label, row, text)
			}
		}
		ordinary, _ := tt.Find("普通操作")
		if ordinary.H != ControlHeight {
			t.Fatal("multi-line choices changed ordinary button density", ordinary)
		}
		if err := tt.Click("选择客户端"); err != nil {
			t.Fatal(err)
		}
		if !tt.Focused("选择客户端") {
			t.Fatal("choice did not retain keyboard focus")
		}
		tt.Key(0, ui.KeyEnter)
		tt.Key(0, ui.KeySpace)
		if clicks != 3 {
			t.Fatal("choice lost pointer, Enter or Space activation", clicks)
		}
	}
}

func TestListItemKeepsFeedbackBoundToItsStableKey(t *testing.T) {
	for _, dark := range []bool{false, true} {
		selected := ""
		tt := ui.NewTester(func(c *ui.Context) {
			ui.Column(c).Fill().Padding(12).Gap(8).Background(c.Theme().Background).Children(func() {
				for _, key := range []string{"first", "second"} {
					ListItem(c, key, key, selected == key).Children(func() {
						ui.Text(c, "客户端 "+key).FontSize(13)
					})
				}
			})
		}, 240, 180)
		tt.SetDark(dark)
		tt.Move(230, 170)
		first, ok := tt.Find("first")
		if !ok {
			t.Fatal("first item is missing")
		}
		second, ok := tt.Find("second")
		if !ok {
			t.Fatal("second item is missing")
		}
		pixel := func(row ui.Rect) any {
			return tt.Image().RGBAAt(int(row.X+5), int(row.Y+row.H/2))
		}
		firstIdle, secondIdle := pixel(first), pixel(second)
		tt.Move(first.X+first.W/2, first.Y+first.H/2)
		hover := pixel(first)
		if hover == firstIdle || pixel(second) != secondIdle {
			t.Fatal("hover feedback was missing or applied to a different key", dark)
		}
		tt.Press(first.X+first.W/2, first.Y+first.H/2)
		pressed := pixel(first)
		if pressed == hover || pressed == firstIdle || pixel(second) != secondIdle {
			t.Fatal("pressed feedback was missing or applied to a different key", dark)
		}
		tt.Release(first.X+first.W/2, first.Y+first.H/2)
		tt.Move(230, 170)
		selected = "second"
		tt.Frame()
		if pixel(first) != firstIdle || pixel(second) == secondIdle {
			t.Fatal("selection feedback did not follow its stable key", dark)
		}
	}
}
