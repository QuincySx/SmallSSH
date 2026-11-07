package desktop

import (
	"os"
	"testing"

	"github.com/egoist/mygo/ui"
)

func TestWorkspaceDockOnlyBottomOrRightAndNarrowFallback(t *testing.T) {
	for _, side := range []string{"left", "right", "top", "bottom"} {
		t.Run(side, func(t *testing.T) {
			a := fileFollowDemo()
			pane, source := a.primaryFilePane, a.currentTerminal()
			a.setToolsDockSide(side)
			want := side
			if want == "left" || want == "top" {
				want = "bottom"
			}
			tt := ui.NewTester(a.view, 1280, 900)
			term, termOK := tt.Find("终端区域")
			tools, toolsOK := tt.Find("工具面板")
			if !termOK || !toolsOK || term.W < 200 || tools.W < 200 || term.H < 160 || tools.H < 160 {
				t.Fatal("dock lost a usable view", side, term, tools)
			}
			correct := want == "right" && term.X+term.W <= tools.X+1 || want == "bottom" && term.Y+term.H <= tools.Y+1
			if !correct || a.primaryFilePane != pane || a.currentTerminal() != source {
				t.Fatal("dock placement changed content identity", side, term, tools)
			}
			tt.SetSize(560, 760)
			tt.Frame()
			if a.savedToolsDockSide() != want {
				t.Fatal("responsive fallback overwrote the requested side")
			}
			if want == "right" {
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
		want := side
		if want == "left" || want == "top" {
			want = "bottom"
		}
		b := &app{dir: a.dir}
		b.loadLayout()
		if b.savedToolsDockSide() != want {
			t.Fatal("saved side did not round trip", side, b.savedToolsDockSide())
		}
	}
}
