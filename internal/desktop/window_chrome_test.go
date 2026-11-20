package desktop

import (
	"testing"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"
)

func TestMainWindowOptionsTitleBar(t *testing.T) {
	a := &app{}
	for _, goos := range []string{"darwin", "windows", "linux"} {
		for _, background := range []bool{false, true} {
			opts := a.mainWindowOptions(background, goos)
			style := mygo.TitleBarDefault
			if goos == "darwin" {
				style = mygo.TitleBarHidden
			}
			if opts.TitleBarStyle != style || opts.Hidden != background || opts.Title != applicationName || opts.Width != 1280 || opts.Height != 900 || opts.MinWidth != 560 || opts.MinHeight != 760 || opts.Content == nil || opts.Frameless {
				t.Fatalf("%s background=%v: unexpected window options: %+v", goos, background, opts)
			}
			if goos == "darwin" {
				if opts.TrafficLightPosition == nil || *opts.TrafficLightPosition != (mygo.Point{X: 16, Y: 17}) {
					t.Fatal("native window controls lost their inset", opts.TrafficLightPosition)
				}
			} else if opts.TrafficLightPosition != nil {
				t.Fatal("macOS window-control placement applied to another platform", goos)
			}
		}
	}
}

func TestWindowChromeCompactKeyboardAndActions(t *testing.T) {
	a := fileFollowDemo()
	tt := ui.NewTester(a.workspaceHeader, 560, 70)
	tt.SetTitleBar(ui.TitleBar{Height: 54, Left: 80, Right: 60})
	tt.SetLocale("en")
	for _, label := range []string{"Settings", "Activity center", "Hide tool panel"} {
		r, ok := tt.Find(label)
		if !ok || r.X < 80 || r.X+r.W > 500 {
			t.Fatalf("English compact control %s overlaps title bar: %v", label, r)
		}
	}
	if err := tt.Click("终端 A"); err != nil {
		t.Fatal(err)
	}
	tt.Key(0, ui.KeyRight)
	if a.currentTerminal() != a.sessions[1] {
		t.Fatal("keyboard did not select the next terminal")
	}
	if err := tt.Click("Activity center"); err != nil || !a.transfersOpen {
		t.Fatalf("transfer click: %v open=%v", err, a.transfersOpen)
	}
	if err := tt.Click("Close Activity center"); err != nil || a.transfersOpen {
		t.Fatalf("transfer close: %v open=%v", err, a.transfersOpen)
	}
	if err := tt.Click("Hide tool panel"); err != nil || !a.toolsHidden || a.toolsRight {
		t.Fatalf("hide tools changed dock preference: %v", err)
	}
	if err := tt.Click("Show tool panel"); err != nil || a.toolsHidden || a.toolsRight {
		t.Fatalf("show tools changed dock preference: %v", err)
	}
}

func TestWindowChromeKeepsNavigationClear(t *testing.T) {
	for _, width := range []int{560, 1280} {
		for _, zoom := range []float32{1, 1.25} {
			a := fileFollowDemo()
			a.zoom = zoom
			tt := ui.NewTester(a.view, width, 900)
			bar := ui.TitleBar{Height: 40, Left: 80, Right: 60}
			tt.SetTitleBar(bar)
			for _, label := range []string{"收窄连接侧栏", "设置", "任务中心", "隐藏工具面板"} {
				r, ok := tt.Find(label)
				if !ok || r.X < bar.Left || r.X+r.W > float32(width)-bar.Right {
					t.Fatalf("width=%d zoom=%v %s overlaps controls: %v", width, zoom, label, r)
				}
			}
			if err := tt.Click("设置"); err != nil || !a.settingsOpen || a.page != 1 {
				t.Fatalf("settings click: %v page=%d", err, a.page)
			}
			if err := tt.Click("收窄连接侧栏"); err != nil || !a.sidebarHidden {
				t.Fatalf("sidebar click: %v hidden=%v", err, a.sidebarHidden)
			}
		}
	}
}

func TestWindowChromeLockedAndZeroTitleBar(t *testing.T) {
	a := fileFollowDemo()
	tt := ui.NewTester(a.view, 560, 900)
	header, ok := tt.Find("全局导航栏")
	if !ok || header.H != 48 {
		t.Fatal("default header height changed", header)
	}
	locked := &app{create: true}
	lt := ui.NewTester(locked.view, 560, 900)
	if _, ok := lt.Find("窗口拖动区域"); ok {
		t.Fatal("zero title bar added a locked header")
	}
	lt.SetTitleBar(ui.TitleBar{Height: 40, Left: 80, Right: 60})
	card, ok := lt.Find("保险库卡片")
	if !ok || card.Y < 40 {
		t.Fatal("locked card overlaps the title bar", card)
	}
	if _, ok := lt.Find("窗口拖动区域"); !ok {
		t.Fatal("locked header is missing its drag area")
	}
}
