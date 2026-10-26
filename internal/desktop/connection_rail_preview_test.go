//go:build preview_qa

package desktop

import (
	"context"
	"fmt"
	"image/png"
	"os"
	"path/filepath"
	"testing"

	"github.com/egoist/mygo/ui"
	"sshclient/internal/desktop/components"
	"sshclient/internal/vault"
)

// Opt-in, offline rendering uses documentation addresses and never starts the
// native app, opens a vault, or attempts an SSH connection.
func TestConnectionRailSnapshotsWithoutWindows(t *testing.T) {
	if os.Getenv("SSH_CLIENT_PREVIEW_QA") != "1" {
		t.Skip("opt-in offline rail snapshots")
	}
	dir := os.Getenv("SSH_CLIENT_UI_SNAPSHOT_DIR")
	if !filepath.IsAbs(dir) {
		t.Fatal("absolute snapshot directory required")
	}
	if err := os.MkdirAll(dir, 0700); err != nil {
		t.Fatal(err)
	}
	for _, variant := range []string{"light", "dark", "scrolled"} {
		t.Run(variant, func(t *testing.T) {
			a := &app{ctx: context.Background(), online: true, page: 1, selected: "prod-web"}
			a.connections = []vault.Summary{
				{ID: "prod-web", Name: "生产 Web", Group: "生产环境", User: "ops", Host: "192.0.2.10"},
				{ID: "prod-db", Name: "生产数据库", Group: "生产环境", User: "ops", Host: "192.0.2.11"},
				{ID: "dev-web", Name: "开发 Web", Group: "开发测试", User: "dev", Host: "192.0.2.20"},
				{ID: "dev-api", Name: "开发 API", Group: "开发测试", User: "dev", Host: "192.0.2.21"},
				{ID: "ungrouped", Name: "临时服务器", User: "ops", Host: "192.0.2.30"},
			}
			if variant == "scrolled" {
				for i := 0; i < 100; i++ {
					a.connections = append(a.connections, vault.Summary{ID: fmt.Sprintf("example-%03d", i), Name: fmt.Sprintf("示例服务器 %03d", i), Group: "示例组", User: "ops", Host: "192.0.2.40"})
				}
			}
			compact := false
			tt := ui.NewTester(func(c *ui.Context) { a.connectionSidebar(c, compact) }, 248, 560)
			tt.SetDark(variant == "dark")
			if variant == "scrolled" {
				a.connectionRailList.ScrollToEnd()
				tt.Frame()
			}
			for _, mode := range []string{"expanded", "compact", "restored"} {
				compact = mode == "compact"
				width := 248
				if compact {
					width = int(components.RailWidth)
				}
				tt.SetSize(width, 560)
				tt.Frame()
				path := filepath.Join(dir, "connection-sidebar-"+variant+"-"+mode+".png")
				f, err := os.OpenFile(path, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0600)
				if err != nil {
					t.Fatal(err)
				}
				err = png.Encode(f, tt.Image())
				closeErr := f.Close()
				if err != nil || closeErr != nil {
					t.Fatal(err, closeErr)
				}
				t.Log(path)
			}
		})
	}
}
