package desktop

import (
	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"
	"sshclient/internal/desktop/components"
)

func (a *app) mainWindowOptions(background bool, goos string) mygo.WindowOptions {
	opts := mygo.WindowOptions{Title: applicationName, Width: 1280, Height: 900, MinWidth: 560, MinHeight: 760, Hidden: background, Content: ui.View(a.view), TitleBarStyle: mygo.TitleBarDefault}
	applyHiddenWindowChrome(&opts, goos)
	return opts
}

// Keep the native controls centered in the 48-point custom title bar rather
// than inheriting AppKit's compact, edge-aligned hidden-title-bar placement.
func applyHiddenWindowChrome(opts *mygo.WindowOptions, goos string) {
	if goos == "darwin" {
		opts.TitleBarStyle = mygo.TitleBarHidden
		opts.TrafficLightPosition = &mygo.Point{X: 16, Y: 17}
	}
}

func windowChromeMetrics(c *ui.Context) (ui.TitleBar, float32) {
	bar := c.TitleBar()
	height := float32(components.HeaderHeight)
	if bar.Height > height {
		height = bar.Height
	}
	return bar, height
}

// headerControl leaves only the blank bands above and below a control draggable.
func headerControl(c *ui.Context, height, controlHeight float32, content func()) {
	band := (height - controlHeight) / 2
	ui.Column(c).Height(height).Shrink(0).Children(func() {
		if band > 0 {
			ui.Spacer(c).FillWidth().Height(band).Grow(0).Shrink(0).DragWindow()
		}
		content()
		if band > 0 {
			ui.Spacer(c).FillWidth().Height(band).Grow(0).Shrink(0).DragWindow()
		}
	})
}

func lockedWindowChrome(c *ui.Context) {
	bar, _ := windowChromeMetrics(c)
	if bar.Height <= 0 {
		return
	}
	ui.Row(c).FillWidth().Height(bar.Height).Shrink(0).Padding(0, bar.Right, 0, bar.Left).DragWindow().Children(func() {
		ui.Spacer(c).Grow(1).MinWidth(0).Height(bar.Height).DragWindow().Label(tr(c, "窗口拖动区域"))
	})
}
