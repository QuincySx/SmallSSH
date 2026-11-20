//go:build preview_qa

package desktop

import (
	"image"
	"image/draw"
	"image/png"
	"math"
	"os"
	"path/filepath"
	"strconv"
	"testing"

	"github.com/egoist/mygo/ui"
	"sshclient/internal/desktop/components"
)

func TestSSHIdentitySettingsOfflineSnapshots(t *testing.T) {
	if os.Getenv("SSH_CLIENT_PREVIEW_QA") != "1" {
		t.Skip("opt-in offline snapshots")
	}
	dir := os.Getenv("SSH_CLIENT_UI_SNAPSHOT_DIR")
	if !filepath.IsAbs(dir) {
		t.Fatal("absolute snapshot directory required")
	}
	if err := os.MkdirAll(dir, 0700); err != nil {
		t.Fatal(err)
	}
	for _, dark := range []bool{false, true} {
		for _, width := range []int{720, 1080} {
			name := map[bool]string{false: "light", true: "dark"}[dark] + "-" + strconv.Itoa(width)
			t.Run(name, func(t *testing.T) {
				a := &app{settingsSection: settingsIdentities, identities: offlineIdentities()}
				tt := ui.NewTester(func(c *ui.Context) { components.ApplyTheme(c, false); a.settingsView(c) }, width, 900)
				tt.SetDark(dark)
				tt.SetTitleBar(ui.TitleBar{Height: 48, Left: 70})
				for _, label := range []string{"SSH 密钥名称", "签名验证选择", "复制公钥：开发机", "密钥限时授权：开发机", "删除 SSH 密钥：开发机"} {
					bounds, ok := tt.Find(label)
					if !ok || bounds.X < 0 || bounds.X+bounds.W > float32(width) {
						t.Fatal("identity control overflow", label, bounds)
					}
				}
				path := filepath.Join(dir, "ssh-identities-"+name+".png")
				f, err := os.Create(path)
				if err != nil {
					t.Fatal(err)
				}
				err = png.Encode(f, tt.Image())
				closeErr := f.Close()
				if err != nil || closeErr != nil {
					t.Fatal(err, closeErr)
				}
				t.Log(path)
				if width != 720 {
					return
				}
				if err := tt.Click("签名验证选择"); err != nil {
					t.Fatal(err)
				}
				menu, ok := tt.Find("签名验证选项")
				if !ok || menu.W != 256 || menu.H != 104 {
					t.Fatal("signature verification popup dimensions", menu, ok)
				}
				origin := image.Pt(int(math.Round(float64(menu.X))), int(math.Round(float64(menu.Y))))
				crop := image.NewRGBA(image.Rect(0, 0, int(menu.W), int(menu.H)))
				draw.Draw(crop, crop.Bounds(), tt.Image(), origin, draw.Src)
				path = filepath.Join(dir, "signature-menu-"+name+".png")
				f, err = os.Create(path)
				if err != nil {
					t.Fatal(err)
				}
				if err, closeErr := png.Encode(f, crop), f.Close(); err != nil || closeErr != nil {
					t.Fatal(err, closeErr)
				}
				t.Log(path)
			})
		}
	}
}
