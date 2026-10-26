package desktop

import (
	"fmt"
	"math"
	"os"
	"testing"

	"github.com/egoist/mygo/ui"
)

func TestDockMinimumRestoresWithPinnedFilesAndSessions(t *testing.T) {
	a := fileFollowDemo()
	a.dir = t.TempDir()
	if err := os.Chmod(a.dir, 0700); err != nil {
		t.Fatal(err)
	}
	a.toolsRight, a.terminalWidthShare = true, 65
	a.chooseFileEndpoint(a.primaryFilePane, "c")
	pane, sess := a.primaryFilePane, a.sessions[0]
	tt := ui.NewTester(a.view, 1280, 900)
	term, _ := tt.Find("终端区域")
	x, y := term.X+term.W, term.Y+term.H/2
	tt.Press(x, y)
	tt.Move(x-700, y)
	tt.Release(x-700, y)
	atLimit, _ := tt.Find("终端区域")
	if a.terminalWidthShare >= 35 || a.primaryFilePane != pane || pane.follow || pane.endpoint != "c" || pane.browser().dir != "/srv/c" || a.sessions[0] != sess {
		t.Fatal("resizing reset the fitted layout or its workspace state", a.terminalWidthShare)
	}
	if err := a.saveLayout(); err != nil {
		t.Fatal(err)
	}
	b := fileFollowDemo()
	b.dir = a.dir
	b.loadLayout()
	restored := ui.NewTester(b.view, 1280, 900)
	got, _ := restored.Find("终端区域")
	if math.Abs(float64(got.W-atLimit.W)) > 1 || b.terminalWidthShare != a.terminalWidthShare {
		t.Fatal("reopening reset a valid fitted width", atLimit, got, a.terminalWidthShare, b.terminalWidthShare)
	}
}

func TestDockDividerStopsAtBoundaryWithoutResetting(t *testing.T) {
	for _, right := range []bool{false, true} {
		t.Run(fmt.Sprintf("right=%v", right), func(t *testing.T) {
			a := statusDemo()
			a.toolsRight, a.terminalWidthShare = right, 65
			tt := ui.NewTester(a.view, 1000, 760)
			term, _ := tt.Find("终端区域")
			x, y := term.X+term.W/2, term.Y+term.H
			position := func(r ui.Rect) float32 { return r.Y + r.H }
			if right {
				x, y = term.X+term.W, term.Y+term.H/2
				position = func(r ui.Rect) float32 { return r.X + r.W }
			}
			tt.Press(x, y)
			previous := position(term)
			for offset := float32(20); offset <= 480; offset += 20 {
				if right {
					tt.Move(x-offset, y)
				} else {
					tt.Move(x, y-offset)
				}
				current, _ := tt.Find("终端区域")
				if position(current) > previous+1 {
					t.Fatalf("divider jumped backwards while dragging toward its minimum: %v -> %v", previous, position(current))
				}
				previous = position(current)
			}
			atLimit, _ := tt.Find("终端区域")
			tt.Frame()
			still, _ := tt.Find("终端区域")
			if math.Abs(float64(position(still)-position(atLimit))) > 1 {
				t.Fatal("divider did not remain at its limit", atLimit, still)
			}
			// Reverse while still holding the divider: it must move immediately.
			if right {
				tt.Move(x-460, y)
				tt.Release(x-460, y)
			} else {
				tt.Move(x, y-460)
				tt.Release(x, y-460)
			}
			reversed, _ := tt.Find("终端区域")
			if delta := position(reversed) - position(atLimit); delta < 15 || delta > 25 {
				t.Fatal("divider did not reverse smoothly from the boundary", delta)
			}
			// The opposite bound must behave the same way, including release.
			x, y = reversed.X+reversed.W/2, reversed.Y+reversed.H
			if right {
				x, y = reversed.X+reversed.W, reversed.Y+reversed.H/2
			}
			tt.Press(x, y)
			previous = position(reversed)
			for offset := float32(20); offset <= 600; offset += 20 {
				if right {
					tt.Move(x+offset, y)
				} else {
					tt.Move(x, y+offset)
				}
				current, _ := tt.Find("终端区域")
				if position(current) < previous-1 {
					t.Fatal("divider jumped backwards at the opposite bound", previous, position(current))
				}
				previous = position(current)
			}
			if right {
				tt.Release(x+600, y)
			} else {
				tt.Release(x, y+600)
			}
			current, _ := tt.Find("终端区域")
			if math.Abs(float64(position(current)-previous)) > 1 {
				t.Fatal("release moved the divider away from its bound", previous, current)
			}
			// Keyboard resizing uses the same limits as pointer dragging.
			back, forth := ui.KeyUp, ui.KeyDown
			if right {
				back, forth = ui.KeyLeft, ui.KeyRight
			}
			tt.Key(0, back)
			moved, _ := tt.Find("终端区域")
			if position(moved) >= position(current)-5 {
				t.Fatal("focused divider did not move by keyboard", current, moved)
			}
			for range 5 {
				tt.Key(0, forth)
			}
			current, _ = tt.Find("终端区域")
			if math.Abs(float64(position(current)-previous)) > 1 {
				t.Fatal("keyboard crossed the pointer's bound", previous, current)
			}
			tt.SetSize(1000, 480)
			tt.Frame()
			panel, _ := tt.Find("工具面板")
			if !right && panel.H < 159 {
				t.Fatal("resizing the window clipped the dock controls", panel)
			}
		})
	}
}

func TestVisualDockMenuKeepsActionsVisibleAndWorkspaceState(t *testing.T) {
	for _, width := range []int{1000, 1280} {
		for _, dark := range []bool{false, true} {
			t.Run(fmt.Sprintf("%d/dark=%v", width, dark), func(t *testing.T) {
				a := fileFollowDemo()
				a.sidebarWidth = 196
				a.chooseFileEndpoint(a.primaryFilePane, "c")
				pane, session := a.primaryFilePane, a.sessions[0]
				tt := ui.NewTester(a.view, width, 760)
				tt.SetDark(dark)
				for _, right := range []bool{true, false} {
					if err := tt.Click("工具面板布局"); err != nil {
						t.Fatal(err)
					}
					popup, ok := tt.Find("工具面板布局菜单")
					if !ok || popup.X < 0 || popup.X+popup.W > float32(width) || popup.Y < 0 || popup.Y+popup.H > 760 || !tt.HasText("停靠侧") {
						t.Fatal("visual dock selector is outside the window", popup)
					}
					label := "放到底部"
					if right {
						label = "放到右侧"
					}
					if err := tt.Click(label); err != nil {
						t.Fatal(err)
					}
					tt.Frame()
					if a.toolsRight != right || a.toolsLayoutOpen || a.toolsHidden || a.fileExpanded || a.primaryFilePane != pane || a.primaryFilePane.endpoint != "c" || a.primaryFilePane.follow || a.sessions[0] != session {
						t.Fatal("docking changed the active session or pinned files")
					}
					term, _ := tt.Find("终端区域")
					panel, _ := tt.Find("工具面板")
					if right && panel.X < term.X+term.W || !right && panel.Y < term.Y+term.H {
						t.Fatal("dock placement did not match its icon", term, panel)
					}
					for _, action := range []string{"工具面板布局", "收起工具面板"} {
						r, ok := tt.Find(action)
						if !ok || r.X < panel.X || r.X+r.W > float32(width) || r.Y < 0 {
							t.Fatal("dock action was clipped", action, r, panel)
						}
					}
				}
				if err := tt.Click("工具面板布局"); err != nil {
					t.Fatal(err)
				}
				tt.Key(0, ui.KeyEscape)
				if a.toolsLayoutOpen || a.toolsRight || a.toolsHidden {
					t.Fatal("Escape changed layout instead of dismissing the menu")
				}
				if err := tt.Click("工具面板布局"); err != nil {
					t.Fatal(err)
				}
				if err := tt.Click("放大工具面板"); err != nil {
					t.Fatal(err)
				}
				if !a.fileExpanded || a.toolsLayoutOpen {
					t.Fatal("expand action failed to dismiss the menu")
				}
				if err := tt.Click("工具面板布局"); err != nil {
					t.Fatal(err)
				}
				if err := tt.Click("放到右侧"); err != nil {
					t.Fatal(err)
				}
				if a.fileExpanded || !a.toolsRight {
					t.Fatal("choosing a dock did not restore the terminal split")
				}
				if err := tt.Click("收起工具面板"); err != nil {
					t.Fatal(err)
				}
				if err := tt.Click("显示工具面板"); err != nil {
					t.Fatal(err)
				}
				if !a.toolsRight || a.toolsHidden {
					t.Fatal("restoring a hidden panel lost its dock")
				}
				if err := tt.Click("工具面板布局"); err != nil {
					t.Fatal(err)
				}
				if err := tt.Click("收起面板"); err != nil {
					t.Fatal(err)
				}
				if !a.toolsHidden || a.toolsLayoutOpen || a.primaryFilePane != pane || pane.endpoint != "c" || a.sessions[0] != session {
					t.Fatal("menu hide action changed workspace state or left its popup open")
				}
				if err := tt.Click("显示工具面板"); err != nil {
					t.Fatal(err)
				}
				tt.SetSize(1280, 760)
				term, _ := tt.Find("终端区域")
				share := a.terminalWidthShare
				tt.Press(term.X+term.W, term.Y+100)
				tt.Move(term.X+term.W-60, term.Y+100)
				tt.Release(term.X+term.W-60, term.Y+100)
				resized, _ := tt.Find("终端区域")
				if a.terminalWidthShare >= share || resized.W >= term.W-30 {
					t.Fatal("dragging the dock divider lost its new width", term, resized)
				}
				tt.SetSize(1000, 760)
				close, _ := tt.Find("收起工具面板")
				if close.W < 28 || close.X+close.W > 1000 {
					t.Fatal("resizing clipped the close control", close)
				}
			})
		}
	}
}
