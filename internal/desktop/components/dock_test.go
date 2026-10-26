package components

import (
	"fmt"
	"math"
	"testing"

	"github.com/egoist/mygo/ui"
)

func TestVisualDockSelectorKeyboardAndDismissal(t *testing.T) {
	open, right := false, false
	choices := 0
	tt := ui.NewTester(func(c *ui.Context) {
		ui.Row(c).FillWidth().Children(func() {
			ui.Spacer(c).Grow(1)
			switch PanelLayoutMenu(c, &open, right, false) {
			case PanelDockRight:
				right = true
				choices++
			case PanelDockBottom:
				right = false
				choices++
			}
		})
	}, 330, 240)
	if err := tt.Click("工具面板布局"); err != nil {
		t.Fatal(err)
	}
	if !open || !tt.Focused("放到底部") || choices != 0 {
		t.Fatal("opening did not focus the current dock without changing it")
	}
	for _, step := range []struct {
		key  ui.Key
		want string
	}{{ui.KeyDown, "放大工具面板"}, {ui.KeyDown, "收起面板"}, {ui.KeyDown, "放到底部"}, {ui.KeyUp, "收起面板"}, {ui.KeyUp, "放大工具面板"}, {ui.KeyUp, "放到底部"}} {
		tt.Key(0, step.key)
		if !tt.Focused(step.want) || !open || choices != 0 {
			t.Fatal("vertical menu navigation changed layout or lost focus", step.want, open, choices)
		}
	}
	tt.Key(0, ui.KeyRight)
	tt.Key(0, ui.KeyEnter)
	if !right || open || choices != 1 {
		t.Fatal("toolbar arrow/Enter did not select and dismiss")
	}
	if err := tt.Click("工具面板布局"); err != nil {
		t.Fatal(err)
	}
	tt.Key(0, ui.KeyEscape)
	if open || !right || choices != 1 {
		t.Fatal("Escape changed the selected dock")
	}
	if !tt.Focused("工具面板布局") {
		t.Fatal("dismissing the menu did not restore its trigger focus")
	}
	if err := tt.Click("工具面板布局"); err != nil {
		t.Fatal(err)
	}
	tt.ClickAt(10, 210)
	if open || !right || choices != 1 {
		t.Fatal("outside click changed the selected dock")
	}
}

func TestDockMenuUsesCompactSingleRowAndVisibleSelection(t *testing.T) {
	for _, dark := range []bool{false, true} {
		for _, right := range []bool{false, true} {
			t.Run(fmt.Sprintf("dark=%v/right=%v", dark, right), func(t *testing.T) {
				open := false
				tt := ui.NewTester(func(c *ui.Context) {
					ApplyTheme(c, false)
					ui.Row(c).FillWidth().Children(func() {
						ui.Spacer(c).Grow(1)
						PanelLayoutMenu(c, &open, right, false)
					})
				}, 330, 240)
				tt.SetDark(dark)
				if err := tt.Click("工具面板布局"); err != nil {
					t.Fatal(err)
				}
				popup, _ := tt.Find("工具面板布局菜单")
				heading, ok := tt.Find("停靠侧")
				bottom, b := tt.Find("放到底部")
				rightChoice, r := tt.Find("放到右侧")
				if !ok || !b || !r || popup.H > 130 || bottom.W != 32 || bottom.H != 32 || rightChoice.Y != bottom.Y || rightChoice.X-bottom.X-bottom.W != 4 {
					t.Fatal("dock choices became separate cards or vertical rows", popup, heading, bottom, rightChoice)
				}
				if math.Abs(float64(heading.Y+heading.H/2-bottom.Y-bottom.H/2)) > 1 || heading.X+heading.W > bottom.X {
					t.Fatal("heading and dock icons are not on one row", heading, bottom)
				}
				for _, label := range []string{"放大工具面板", "收起面板"} {
					row, found := tt.Find(label)
					if !found || row.Y < bottom.Y+bottom.H || row.X < popup.X || row.X+row.W > popup.X+popup.W {
						t.Fatal("menu row was clipped or mixed with dock choices", label, row, popup)
					}
				}
				selected, other := bottom, rightChoice
				if right {
					selected, other = rightChoice, bottom
				}
				im := tt.Image()
				if im.RGBAAt(int(selected.X+5), int(selected.Y+5)) == im.RGBAAt(int(other.X+5), int(other.Y+5)) {
					t.Fatal("selected placement has no visible indicator", right, dark)
				}
			})
		}
	}
}

func TestDockMenuActionsActivateByKeyboardAndDismiss(t *testing.T) {
	open, expanded := false, false
	last := PanelLayoutUnchanged
	tt := ui.NewTester(func(c *ui.Context) {
		ui.Row(c).FillWidth().Children(func() {
			ui.Spacer(c).Grow(1)
			action := PanelLayoutMenu(c, &open, false, expanded)
			if action != PanelLayoutUnchanged {
				last = action
				if action == PanelToggleExpanded {
					expanded = !expanded
				}
			}
		})
	}, 330, 240)
	for _, want := range []PanelLayoutAction{PanelToggleExpanded, PanelToggleExpanded, PanelHide} {
		if err := tt.Click("工具面板布局"); err != nil {
			t.Fatal(err)
		}
		tt.Key(0, ui.KeyDown)
		if want == PanelHide {
			tt.Key(0, ui.KeyDown)
		} else if expanded && !tt.Focused("恢复分栏") {
			t.Fatal("expanded menu did not offer the restore action")
		}
		tt.Key(0, ui.KeyEnter)
		if open || last != want {
			t.Fatal("menu action did not activate and close", want, last, open)
		}
	}
}

func TestDockMenuOutsideClickKeepsFocusOnClickedContent(t *testing.T) {
	open, input := false, ""
	tt := ui.NewTester(func(c *ui.Context) {
		ui.Column(c).Fill().Children(func() {
			ui.Row(c).FillWidth().Children(func() {
				ui.Spacer(c).Grow(1)
				PanelLayoutMenu(c, &open, false, false)
			})
			ui.Spacer(c).Height(170)
			ui.TextInput(c, &input).Width(220).Label("背景输入")
		})
	}, 330, 300)
	if err := tt.Click("工具面板布局"); err != nil {
		t.Fatal(err)
	}
	if err := tt.Click("背景输入"); err != nil {
		t.Fatal(err)
	}
	tt.Type("hello")
	if open || !tt.Focused("背景输入") || input != "hello" {
		t.Fatal("outside click restored menu focus instead of the clicked content", open, input)
	}
}

func TestDetachedDockSelectorKeyboardAndApplicableActions(t *testing.T) {
	open := false
	chosen := PanelLayoutUnchanged
	tt := ui.NewTester(func(c *ui.Context) {
		ui.Row(c).FillWidth().Children(func() {
			ui.Spacer(c).Grow(1)
			if action := PanelLayoutMenu(c, &open, true, false, true); action != PanelLayoutUnchanged {
				chosen = action
			}
		})
	}, 330, 240)
	if err := tt.Click("工具面板布局"); err != nil {
		t.Fatal(err)
	}
	if !tt.Focused("独立窗口") || tt.HasText("放大工具面板") {
		t.Fatal("undocked mode is not selected or contains pane-only controls")
	}
	tt.Key(0, ui.KeyDown)
	if !tt.Focused("收起面板") {
		t.Fatal("Down did not skip the inapplicable resize action")
	}
	tt.Key(0, ui.KeyUp)
	tt.Key(0, ui.KeyRight)
	if !tt.Focused("放到底部") {
		t.Fatal("Right did not enter the bottom dock choice")
	}
	tt.Key(0, ui.KeyEnter)
	if open || chosen != PanelDockBottom {
		t.Fatal("keyboard redocking did not activate and dismiss")
	}
}
