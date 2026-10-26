//go:build preview_qa && terminal_integration

package desktop

import (
	"image/png"
	"os"
	"path/filepath"
	"testing"

	"github.com/egoist/mygo/ui"
)

func TestFilePropertiesSnapshotsWithoutWindows(t *testing.T) {
	if os.Getenv("SSH_CLIENT_PREVIEW_QA") != "1" {
		t.Skip("opt-in offline preview")
	}
	dir := os.Getenv("SSH_CLIENT_UI_SNAPSHOT_DIR")
	if !filepath.IsAbs(dir) {
		t.Fatal("absolute snapshot path required")
	}
	if err := os.MkdirAll(dir, 0700); err != nil {
		t.Fatal(err)
	}
	for _, variant := range []string{"light", "dark", "compact", "compact-scrolled", "error"} {
		t.Run(variant, func(t *testing.T) {
			a := filePresentationDemo()
			a.renderingToolsWindow = true
			a.openFileProperties(a.primaryFilePane, "/srv/site")
			a.renderingToolsWindow = false
			s := a.prompt.properties
			s.loaded(testPropertiesInfo())
			width, height := 960, 720
			if variant == "compact" || variant == "compact-scrolled" {
				width, height = 640, 420
			}
			if variant == "error" {
				s.error = "所属用户和组保存失败：权限不足"
			}
			tt := ui.NewTester(a.detachedToolsView, width, height)
			tt.SetDark(variant == "dark")
			if variant == "compact-scrolled" {
				tt.Scroll(320, 240, 0, 1000)
				for _, label := range []string{"所属用户", "所属组"} {
					r, ok := tt.Find(label)
					if !ok || r.Y < 120 || r.Y+r.H > 330 {
						t.Fatal("compact property fields cannot scroll into view", label, r)
					}
				}
			}
			for _, label := range []string{"权限 / 属性对话框", "取消", "保存属性"} {
				r, ok := tt.Find(label)
				if !ok || r.W <= 0 || r.H <= 0 || r.X < 0 || r.Y < 0 || r.X+r.W > float32(width)+1 || r.Y+r.H > float32(height)+1 {
					t.Fatal("properties control clipped", label, r)
				}
			}
			if variant != "compact" {
				for _, label := range []string{"所属用户", "所属组", "所有者 · 执行", "其他用户 · 写"} {
					if _, ok := tt.Find(label); !ok {
						t.Fatal("properties field missing", label)
					}
				}
			}
			if variant == "error" && !tt.HasText("重新读取") {
				t.Fatal("failed save cannot be reloaded")
			}
			f, err := os.Create(filepath.Join(dir, "file-properties-"+variant+".png"))
			if err != nil {
				t.Fatal(err)
			}
			err = png.Encode(f, tt.Image())
			closeErr := f.Close()
			if err != nil || closeErr != nil {
				t.Fatal(err, closeErr)
			}
		})
	}
}
