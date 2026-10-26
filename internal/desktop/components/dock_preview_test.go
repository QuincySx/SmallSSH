package components

import (
	"image/png"
	"os"
	"path/filepath"
	"testing"

	"github.com/egoist/mygo/ui"
)

func TestDockMenuSnapshotsWithoutWindows(t *testing.T) {
	if os.Getenv("SSH_CLIENT_PREVIEW_QA") != "1" {
		t.Skip("opt-in offline preview")
	}
	dir := os.Getenv("SSH_CLIENT_UI_SNAPSHOT_DIR")
	if !filepath.IsAbs(dir) {
		t.Fatal("absolute output path required")
	}
	if err := os.MkdirAll(dir, 0700); err != nil {
		t.Fatal(err)
	}
	for _, variant := range []string{"light", "dark", "right", "expanded", "detached"} {
		t.Run(variant, func(t *testing.T) {
			open, selected := false, 2
			tt := ui.NewTester(func(c *ui.Context) {
				ApplyTheme(c, false)
				ui.Column(c).Fill().Background(c.Theme().Background).Children(func() {
					PanelBar(c, &selected, []PanelTab{{0, "文件"}, {1, "命令"}, {2, "服务器状态"}}, func() {
						PanelLayoutMenu(c, &open, variant == "right", variant == "expanded", variant == "detached")
						SVGButton(c, CloseIcon, "收起工具面板")
					})
				})
			}, 420, 220)
			tt.SetDark(variant == "dark")
			if err := tt.Click("工具面板布局"); err != nil {
				t.Fatal(err)
			}
			file, err := os.Create(filepath.Join(dir, "dock-selector-"+variant+".png"))
			if err != nil {
				t.Fatal(err)
			}
			err = png.Encode(file, tt.Image())
			closeErr := file.Close()
			if err != nil || closeErr != nil {
				t.Fatal(err, closeErr)
			}
		})
	}
}
