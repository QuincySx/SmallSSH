//go:build preview_qa

package desktop

import (
	"fmt"
	"image/png"
	"os"
	"path/filepath"
	"sshclient/internal/localize"
	"testing"

	"github.com/egoist/mygo/ui"
	"sshclient/internal/desktop/components"
)

func TestTunnelDiagramOfflineSnapshots(t *testing.T) {
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

	for _, locale := range []string{"zh-Hans", "en"} {
		for _, purpose := range []string{"home", "website", "share", "socks", "upstream", "services"} {
			for _, dark := range []bool{false, true} {
				for _, width := range []int{560, 1080} {
					name := fmt.Sprintf("%s-%s-%t-%d", purpose, locale, dark, width)
					t.Run(name, func(t *testing.T) {
						a := tunnelDiagramFixture("local")
						if purpose == "services" {
							a.tunnels.showingServices = true
							a.tunnels.active = []*tunnelRun{{host: "diagram", kind: "local", purpose: "website", address: "127.0.0.1:50821", ready: true, running: true}, {host: "diagram", kind: "remote", purpose: "share", address: "127.0.0.1:50822", ready: true, running: true}}
						} else {
							kind := map[string]string{"share": "remote", "socks": "socks5", "upstream": "socks5"}[purpose]
							if kind == "" {
								kind = "local"
							}
							a.tunnels.selectKind(kind)
							a.tunnels.browse = purpose == "website"
							if purpose == "upstream" {
								a.tunnels.routeChoice, a.tunnels.localProxy = "local-socks", "127.0.0.1:1080"
								a.tunnels.listen = "127.0.0.1:1081"
							}
						}
						tt := ui.NewTester(func(c *ui.Context) {
							components.ApplyTheme(c, false)
							ui.Column(c).Fill().Background(c.Theme().Background).Children(func() { a.tunnelsView(c) })
						}, width, 800)
						tt.SetLocale(locale)
						tt.SetDark(dark)
						labels := []string{"关闭面板"}
						if purpose == "services" {
							labels = append(labels, "新建服务…")
						} else {
							labels = append(labels, "使用服务器选择")
						}
						if purpose != "home" && purpose != "services" {
							labels = append(labels, a.tunnels.actionLabel())
						}
						for _, label := range labels {
							r, ok := tt.Find(localize.Text(locale, label))
							if !ok || r.X < 0 || r.X+r.W > float32(width) || r.Y < 0 || r.Y+r.H > 800 {
								t.Fatal("simple action clipped", label, r)
							}
						}
						f, err := os.Create(filepath.Join(dir, "service-"+name+".png"))
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
		}
	}
}

func TestTunnelActivityOfflineSnapshots(t *testing.T) {
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
	for _, locale := range []string{"zh-Hans", "en"} {
		for _, dark := range []bool{false, true} {
			for _, width := range []int{560, 1440} {
				a := fileFollowDemo()
				a.tunnels.active = []*tunnelRun{{host: "a", kind: "socks5", address: "127.0.0.1:1080", running: true, ready: true}, {host: "b", kind: "local", address: "127.0.0.1:8080", running: true, ready: true}}
				tt := ui.NewTester(a.view, width, 800)
				tt.SetLocale(locale)
				tt.SetDark(dark)
				icon, ok := tt.Find(localize.Text(locale, "端口转发"))
				if !ok || icon.Y+icon.H > 48 || icon.X+icon.W > float32(width) {
					t.Fatal("running action escaped header", icon)
				}
				name := fmt.Sprintf("running-proxies-%s-%t-%d.png", locale, dark, width)
				f, err := os.Create(filepath.Join(dir, name))
				if err != nil {
					t.Fatal(err)
				}
				err = png.Encode(f, tt.Image())
				closeErr := f.Close()
				if err != nil || closeErr != nil {
					t.Fatal(err, closeErr)
				}
			}
		}
	}
}
