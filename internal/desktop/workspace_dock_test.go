package desktop

import (
	"os"
	"testing"

	"github.com/egoist/mygo/ui"
)

func TestWorkspaceDockFourSidesKeepViewsAndNarrowFallback(t *testing.T) {
	for _, side := range []string{"left", "right", "top", "bottom"} {
		t.Run(side, func(t *testing.T) {
			a := fileFollowDemo()
			pane, source := a.primaryFilePane, a.currentTerminal()
			a.setToolsDockSide(side)
			tt := ui.NewTester(a.view, 1280, 900)
			term, termOK := tt.Find("终端区域")
			tools, toolsOK := tt.Find("工具面板")
			if !termOK || !toolsOK || term.W < 200 || tools.W < 200 || term.H < 160 || tools.H < 160 {
				t.Fatal("dock lost a usable view", side, term, tools)
			}
			correct := side == "left" && tools.X+tools.W <= term.X+1 || side == "right" && term.X+term.W <= tools.X+1 ||
				side == "top" && tools.Y+tools.H <= term.Y+1 || side == "bottom" && term.Y+term.H <= tools.Y+1
			if !correct || a.primaryFilePane != pane || a.currentTerminal() != source {
				t.Fatal("dock placement changed content identity", side, term, tools)
			}
			tt.SetSize(560, 760)
			tt.Frame()
			if a.savedToolsDockSide() != side {
				t.Fatal("responsive fallback overwrote the requested side")
			}
			if side == "left" || side == "right" {
				term, _ = tt.Find("终端区域")
				tools, _ = tt.Find("工具面板")
				if tools.Y < term.Y+term.H {
					t.Fatal("narrow horizontal dock did not fall back to bottom", term, tools)
				}
			}
		})
	}
}

func TestWorkspaceDockSidePersistsWithoutContent(t *testing.T) {
	for _, side := range []string{"left", "top", "right", "bottom"} {
		a := fileFollowDemo()
		a.dir = t.TempDir()
		if err := os.Chmod(a.dir, 0700); err != nil {
			t.Fatal(err)
		}
		a.setToolsDockSide(side)
		if err := a.saveLayout(); err != nil {
			t.Fatal(err)
		}
		b := &app{dir: a.dir}
		b.loadLayout()
		if b.savedToolsDockSide() != side {
			t.Fatal("saved side did not round trip", side, b.savedToolsDockSide())
		}
	}
}
