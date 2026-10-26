package components

import (
	"bytes"
	"fmt"
	"testing"

	"github.com/egoist/mygo/ui"
)

func TestNavigationRailKeepsCompactChoicesAndNativeKeyboardActivation(t *testing.T) {
	selected := "workspace"
	tt := ui.NewTester(func(c *ui.Context) {
		ui.Row(c).Fill().Children(func() {
			NavigationRail(c, "分类窄栏", func() {
				for _, item := range []struct {
					id, label string
					icon      *ui.SVG
				}{{"workspace", "工作区", WorkspaceIcon}, {"security", "安全与解锁", ShieldIcon}} {
					if RailIcon(c, item.id, item.label, item.icon, selected == item.id).Clicked() {
						selected = item.id
					}
				}
			})
			ui.Column(c).Grow(1).FillHeight().Label("内容").Children(func() { Button(c, "内容操作") })
		})
	}, 560, 180)
	rail, ok := tt.Find("分类窄栏")
	if !ok || rail.X != 0 || rail.W != RailWidth || rail.H != 180 {
		t.Fatal("collapsed navigation lost its full-height 48px strip", rail)
	}
	content, _ := tt.Find("内容")
	if content.X != RailWidth || content.W != 560-RailWidth {
		t.Fatal("compact navigation did not reserve exactly its own width", content)
	}
	for _, label := range []string{"工作区", "安全与解锁"} {
		item, ok := tt.Find(label)
		if !ok || item.W != 36 || item.H != NavigationItemHeight || item.X < rail.X || item.X+item.W > rail.X+rail.W {
			t.Fatal("compact category lost its accessible 36px target", label, item)
		}
	}
	if err := tt.Click("工作区"); err != nil {
		t.Fatal(err)
	}
	tt.Key(0, ui.KeyDown)
	if !tt.Focused("安全与解锁") || selected != "workspace" {
		t.Fatal("vertical arrow did not move focus independently of activation", selected)
	}
	tt.Key(0, ui.KeyEnter)
	if selected != "security" {
		t.Fatal("compact category lost Enter activation")
	}
	tt.Key(0, ui.KeyUp)
	tt.Key(0, ui.KeySpace)
	if selected != "workspace" {
		t.Fatal("compact category lost Space activation")
	}
	tt.Key(0, ui.KeyTab)
	if !tt.Focused("内容操作") {
		t.Fatal("compact navigation did not release its single Tab stop")
	}
}

func TestNavigationRailScrollsIndependentlyOfPageContent(t *testing.T) {
	tt := ui.NewTester(func(c *ui.Context) {
		ui.Row(c).Fill().Children(func() {
			NavigationRail(c, "连接窄栏", func() {
				for i := range 20 {
					RailIcon(c, i, fmt.Sprintf("连接 %02d", i), PendingIcon, i == 0)
				}
			})
			ui.Box(c).Grow(1).FillHeight().Label("固定内容")
		})
	}, 560, 180)
	before, _ := tt.Find("固定内容")
	tt.Scroll(RailWidth/2, 90, 0, 10000)
	last, ok := tt.Find("连接 19")
	after, _ := tt.Find("固定内容")
	if !ok || last.Y < 0 || last.Y+last.H > 180 || before != after {
		t.Fatal("rail overflow did not scroll independently", last, before, after)
	}
}

func TestRailItemKeepsStableKeySelectionHoverAndFocusFeedback(t *testing.T) {
	for _, dark := range []bool{false, true} {
		selected := "second"
		tt := ui.NewTester(func(c *ui.Context) {
			ui.Column(c).Fill().Padding(12).Gap(4).Background(c.Theme().Surface).Children(func() {
				for _, key := range []string{"first", "second"} {
					RailIcon(c, key, key, WorkspaceIcon, selected == key)
				}
			})
		}, 140, 140)
		tt.SetDark(dark)
		tt.Move(130, 130)
		first, _ := tt.Find("first")
		second, _ := tt.Find("second")
		pixel := func(r ui.Rect) any {
			return tt.Image().RGBAAt(int(r.X+4), int(r.Y+r.H/2))
		}
		idle, chosen := pixel(first), pixel(second)
		focusPixel := func() any { return tt.Image().RGBAAt(int(second.X+5), int(second.Y+second.H/2)) }
		withoutFocus := focusPixel()
		if idle == chosen {
			t.Fatal("selected compact item has no visible background", dark)
		}
		tt.Move(first.X+first.W/2, first.Y+first.H/2)
		hover := pixel(first)
		if hover == idle || pixel(second) != chosen {
			t.Fatal("hover feedback did not follow its stable key", dark)
		}
		tt.Press(first.X+first.W/2, first.Y+first.H/2)
		if pixel(first) == hover {
			t.Fatal("compact target has no pressed feedback", dark)
		}
		tt.Release(first.X+first.W/2, first.Y+first.H/2)
		tt.Move(130, 130)
		tt.Key(0, ui.KeyTab)
		if !tt.Focused("second") || focusPixel() == withoutFocus {
			t.Fatal("keyboard focus has no visible color feedback", dark)
		}
		selected = "first"
		tt.Frame()
		if pixel(first) == idle {
			t.Fatal("selection feedback did not move to its new stable key", dark)
		}
	}
}

// Compact states must only fade the existing artwork: no extra frame or ring.
// Check actual painted pixels rather than the SVG or hit-target dimensions.
func TestCompactNavigationFeedbackStaysInsideVisibleCircle(t *testing.T) {
	for _, dark := range []bool{false, true} {
		for _, avatar := range []bool{false, true} {
			t.Run(fmt.Sprintf("dark=%t/avatar=%t", dark, avatar), func(t *testing.T) {
				selected := false
				tt := ui.NewTester(func(c *ui.Context) {
					ui.Column(c).Fill().Padding(6).Background(c.Theme().Surface).Children(func() {
						if avatar {
							NavigationItem(c, "target", "target", selected, true, func() {
								ConnectionAvatar(c, "synthetic-state-check")
							})
						} else {
							NavigationIcon(c, "target", "target", PlusIcon, selected, true).Height(36)
						}
					})
				}, 48, 52)
				tt.SetDark(dark)
				tt.Move(0, 0)
				target, ok := tt.Find("target")
				if !ok || target.W != NavigationIconSlotSize {
					t.Fatal("compact hit target lost its original geometry", target)
				}
				check := func(state string) []byte {
					t.Helper()
					now, _ := tt.Find("target")
					if now != target {
						t.Fatal("interaction changed the hit target", state, target, now)
					}
					pixels := tt.Image()
					background := pixels.RGBAAt(0, 0)
					left := int(target.X + (NavigationIconSlotSize-NavigationCircleSize)/2)
					top := int(target.Y + (target.H-NavigationCircleSize)/2)
					for y := int(target.Y); y < int(target.Y+target.H); y++ {
						for x := int(target.X); x < int(target.X+target.W); x++ {
							if x >= left && x < left+28 && y >= top && y < top+28 {
								continue
							}
							if pixels.RGBAAt(x, y) != background {
								t.Fatalf("%s painted outside the shared 28px circle at %d,%d", state, x, y)
							}
						}
					}
					return bytes.Clone(pixels.Pix)
				}
				idle := check("idle")
				selected = true
				tt.Frame()
				chosen := check("selected")
				if avatar {
					// Compare with the plain avatar, faded on its own: any extra
					// inner outline or overlay changes the pixels and fails here.
					plain := ui.NewTester(func(c *ui.Context) {
						ui.Box(c).Fill().Background(c.Theme().Surface).Children(func() {
							ui.Box(c).Size(28, 28).Opacity(.90).Children(func() {
								ConnectionAvatar(c, "synthetic-state-check")
							})
						})
					}, 28, 28)
					plain.SetDark(dark)
					got, want := tt.Image(), plain.Image()
					left := int(target.X + (NavigationIconSlotSize-NavigationCircleSize)/2)
					top := int(target.Y + (target.H-NavigationCircleSize)/2)
					for y := range 28 {
						for x := range 28 {
							if got.RGBAAt(left+x, top+y) != want.RGBAAt(x, y) {
								t.Fatalf("selected avatar added a ring or overlay at %d,%d", x, y)
							}
						}
					}
				}
				if bytes.Equal(idle, chosen) {
					t.Fatal("selection feedback disappeared")
				}
				x, y := target.X+target.W/2, target.Y+target.H/2
				tt.Move(x, y)
				hover := check("hover")
				if bytes.Equal(chosen, hover) {
					t.Fatal("hover feedback disappeared")
				}
				tt.Press(x, y)
				pressed := check("pressed")
				if bytes.Equal(hover, pressed) {
					t.Fatal("pressed feedback disappeared")
				}
				tt.Release(x, y)
				tt.Move(0, 0)
				tt.Key(0, ui.KeyTab)
				// Dismiss the accessible tooltip so its overlay is not mistaken
				// for a focus ring painted outside the button's circle.
				tt.Key(0, ui.KeyEscape)
				focused := check("keyboard focus")
				if !tt.Focused("target") || bytes.Equal(chosen, focused) {
					t.Fatal("keyboard focus feedback disappeared")
				}
			})
		}
	}
}

func TestConnectionAvatarsStayLocalDeterministicAndBounded(t *testing.T) {
	const id = "synthetic-stable-connection"
	avatar := connectionAvatarSVG(id)
	if again := connectionAvatarSVG(id); again != avatar {
		t.Fatal("repeated frames reparsed an existing connection avatar")
	}
	w, h := avatar.Size()
	if w != 28 || h != 28 {
		t.Fatal("connection avatar lost its 28px vector bounds", w, h)
	}
	render := func(id string) []byte {
		tt := ui.NewTester(func(c *ui.Context) { ConnectionAvatar(c, id) }, 28, 28)
		return bytes.Clone(tt.Image().Pix)
	}
	before := render(id)
	// Check painted pixels, not only the wrapper bounds: a smaller identicon
	// inside a 28px transparent box previously passed the layout checks.
	for _, point := range [][2]int{{1, 14}, {26, 14}, {14, 1}, {14, 26}} {
		offset := (point[1]*28 + point[0]) * 4
		if bytes.Equal(before[offset:offset+4], before[:4]) {
			t.Fatal("visible avatar no longer fills the same 28px circle as navigation actions", point)
		}
	}
	if bytes.Equal(before, render("another-synthetic-connection")) {
		t.Fatal("distinct connection IDs lost their visual identity")
	}
	for i := range connectionAvatarCacheLimit + 1 {
		connectionAvatarSVG(fmt.Sprintf("synthetic-cache-connection-%d", i))
	}
	connectionAvatars.Lock()
	count := len(connectionAvatars.images)
	connectionAvatars.Unlock()
	if count > connectionAvatarCacheLimit {
		t.Fatal("connection avatar cache grew beyond its bound", count)
	}
	if !bytes.Equal(before, render(id)) {
		t.Fatal("connection avatar changed after cache eviction")
	}
}

func TestNavigationSidebarKeepsItemIdentityIconGeometryAndInputAcrossWidths(t *testing.T) {
	compact := false
	selected := 0
	input := ""
	ids := map[string]uint64{}
	titles := []string{"工作区", "安全与解锁", "历史"}
	tt := ui.NewTester(func(c *ui.Context) {
		ui.Row(c).Fill().Children(func() {
			ids["navigation"] = NavigationSidebar(c, "stable-navigation", "分类", 152, compact, func() {
				for i, title := range titles {
					item := NavigationIcon(c, title, title, WorkspaceIcon, selected == i, compact)
					ids[title] = item.ID()
					if item.Clicked() {
						selected = i
					}
				}
			}).ID()
			c.Key("stable-content")
			ui.Column(c).Grow(1).FillHeight().Label("右侧内容").Children(func() {
				c.Key("stable-input")
				ids["input"] = ui.TextInput(c, &input).Label("保留输入").ID()
			})
		})
	}, 560, 180)
	type geometry struct{ item, icon ui.Rect }
	before := map[string]geometry{}
	for i, title := range titles {
		item, _ := tt.Find(title)
		icon, ok := tt.Find(title + "图标")
		if !ok || item.H != NavigationItemHeight || icon.W != 28 || icon.H != 28 || icon.X != item.X+4 || icon.Y != item.Y+(NavigationItemHeight-28)/2 {
			t.Fatal("navigation did not reserve its fixed navigation target and centered 28px icon", title, item, icon)
		}
		if i > 0 && item.Y-before[titles[i-1]].item.Y != NavigationItemHeight+NavigationItemGap {
			t.Fatal("navigation item spacing differs from its shared geometry", title, item)
		}
		before[title] = geometry{item, icon}
	}
	initialIDs := map[string]uint64{}
	for key, id := range ids {
		initialIDs[key] = id
	}
	if err := tt.Click("保留输入"); err != nil {
		t.Fatal(err)
	}
	tt.Type("synthetic input!")
	for _, next := range []bool{true, false, true, false} {
		compact = next
		// A changing selection must not add layout borders or move the icon.
		selected = (selected + 1) % len(titles)
		tt.Frame()
		for _, title := range titles {
			item, _ := tt.Find(title)
			icon, ok := tt.Find(title + "图标")
			if !ok || item.H != NavigationItemHeight || item.Y != before[title].item.Y || icon != before[title].icon {
				t.Fatal("width or selection change moved a navigation item or its icon", compact, title, before[title], item, icon)
			}
			if compact && item.W != NavigationIconSlotSize {
				t.Fatal("compact navigation target did not keep its 36px width", item)
			}
		}
		for key, id := range ids {
			if id != initialIDs[key] {
				t.Fatal("width change replaced a mounted navigation or input element", compact, key, id, initialIDs[key])
			}
		}
		if !tt.Focused("保留输入") || input != "synthetic input!" {
			t.Fatal("navigation width change lost the right content's input state or focus", compact, input)
		}
	}
	if err := tt.Click(titles[1]); err != nil {
		t.Fatal(err)
	}
	for _, next := range []bool{true, false} {
		compact = next
		tt.Frame()
		if !tt.Focused(titles[1]) || selected != 1 || ids[titles[1]] != initialIDs[titles[1]] {
			t.Fatal("navigation width change replaced the focused category", compact, selected)
		}
	}
	tt.Key(0, ui.KeyUp)
	if !tt.Focused(titles[0]) || selected != 1 {
		t.Fatal("expanded navigation arrows did not retain native focus-only behavior", selected)
	}
	tt.Key(0, ui.KeyEnter)
	if selected != 0 {
		t.Fatal("restored expanded navigation lost keyboard activation")
	}

}

func TestNavigationSidebarKeepsItsScrollPositionAcrossWidths(t *testing.T) {
	compact := false
	tt := ui.NewTester(func(c *ui.Context) {
		ui.Row(c).Fill().Children(func() {
			NavigationSidebar(c, "stable-scroll", "分类", 152, compact, func() {
				for i := range 20 {
					NavigationIcon(c, i, fmt.Sprintf("滚动分类 %02d", i), PendingIcon, i == 0, compact)
				}
			})
			ui.Box(c).Grow(1).FillHeight().Label("固定右内容")
		})
	}, 560, 180)
	tt.Scroll(24, 90, 0, 10000)
	before, ok := tt.Find("滚动分类 19")
	beforeIcon, _ := tt.Find("滚动分类 19图标")
	if !ok || before.Y < 0 || before.Y+before.H > 180 {
		t.Fatal("navigation cannot scroll to its final item", before)
	}
	for _, next := range []bool{true, false} {
		compact = next
		tt.Frame()
		after, ok := tt.Find("滚动分类 19")
		icon, _ := tt.Find("滚动分类 19图标")
		content, _ := tt.Find("固定右内容")
		wantX := float32(152)
		if compact {
			wantX = RailWidth
		}
		if !ok || after.Y != before.Y || after.H != before.H || icon != beforeIcon || content.X != wantX || content.H != 180 {
			t.Fatal("navigation width change reset its scroll or moved its final icon", compact, before, after, beforeIcon, icon, content)
		}
	}
}
