//go:build preview_qa

package desktop

import (
	"image/png"
	"os"
	"path/filepath"
	"testing"

	"github.com/egoist/mygo/ui"
	"sshclient/internal/desktop/components"
	"sshclient/internal/vault"
)

func TestVNCSnapshotsWithoutWindows(t *testing.T) {
	if os.Getenv("SSH_CLIENT_PREVIEW_QA") != "1" {
		t.Skip("opt-in offline VNC dialog snapshots")
	}
	dir := os.Getenv("SSH_CLIENT_UI_SNAPSHOT_DIR")
	if !filepath.IsAbs(dir) {
		t.Fatal("absolute snapshot directory required")
	}
	if err := os.MkdirAll(dir, 0700); err != nil {
		t.Fatal(err)
	}
	for _, mode := range []string{"direct", "ssh"} {
		for _, style := range []struct {
			name, locale  string
			width, height int
			dark          bool
		}{
			{"light", "zh-Hans", 680, 720, false}, {"dark", "zh-Hans", 680, 720, true}, {"english", "en", 560, 760, false},
		} {
			t.Run(mode+"-"+style.name, func(t *testing.T) {
				a := &app{online: true, connections: []vault.Summary{{ID: "fixture", Name: "示例服务器", Host: "192.0.2.10", User: "demo", Port: 22}}}
				a.openVNC("fixture")
				a.vnc.mode = mode
				tt := ui.NewTester(func(c *ui.Context) { components.ApplyTheme(c, false); a.vncDialog(c) }, style.width, style.height)
				tt.SetLocale(style.locale)
				tt.SetDark(style.dark)
				file, err := os.Create(filepath.Join(dir, "vnc-"+mode+"-"+style.name+".png"))
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
}
