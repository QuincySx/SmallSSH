//go:build preview_qa

package desktop

import (
	"context"
	"image/png"
	"os"
	"path/filepath"
	"testing"

	"github.com/egoist/mygo/ui"
	"sshclient/internal/connectcfg"
	"sshclient/internal/proxyconfig"
	"sshclient/internal/vault"
)

func TestSavedRouteSnapshotsWithoutWindows(t *testing.T) {
	if os.Getenv("SSH_CLIENT_PREVIEW_QA") != "1" {
		t.Skip("opt-in offline snapshots")
	}
	dir := os.Getenv("SSH_CLIENT_UI_SNAPSHOT_DIR")
	if !filepath.IsAbs(dir) {
		t.Fatal("absolute snapshot path required")
	}
	if err := os.MkdirAll(dir, 0700); err != nil {
		t.Fatal(err)
	}
	for _, mode := range []string{"library", "new", "edit", "connection", "imported", "select", "snell6"} {
		for _, style := range []struct {
			name, locale string
			width        int
			dark         bool
		}{{"light", "zh-Hans", 1080, false}, {"dark", "zh-Hans", 720, true}, {"english", "en", 560, false}} {
			t.Run(mode+"-"+style.name, func(t *testing.T) {
				t.Setenv("MYGO_LOCALE", style.locale)
				a := &app{ctx: context.Background(), online: true, toolsHidden: true, settingsSection: settingsRoutes, settingsOpen: true, groups: map[string]*bool{}}
				a.routes = []vault.RouteSummary{{ID: "shared", Name: "日常工作代理", Proxy: connectcfg.Proxy{Mode: "socks5", Address: "127.0.0.1:1080", User: "example"}, HasPassword: true, References: 3}, {ID: "relay-route", Name: "自有中转", Relay: "relay"}}
				a.connections = []vault.Summary{{ID: "relay", Name: "跳板机"}}
				a.newConnection()
				a.connection.section = connectionNetwork
				a.form.Network.RouteID = "shared"
				if mode == "new" {
					a.editSavedRoute(nil, true)
				}
				if mode == "edit" {
					a.editSavedRoute(&a.routes[0], false)
				}
				if mode == "snell6" {
					nodes, err := proxyconfig.Import(`{"type":"snell","tag":"Snell v6 线路","server":"proxy.example","server_port":76,"version":6,"mode":"default","reuse":true,"psk":"synthetic-snell6-preview"}`)
					if err != nil {
						t.Fatal(err)
					}
					n := nodes[0]
					a.routes[0].Proxy = connectcfg.Proxy{Mode: n.Kind, Address: n.Address, Config: n.Config}
					a.editSavedRoute(&a.routes[0], false)
				}
				if mode == "imported" || mode == "select" {
					nodes, err := proxyconfig.Import(`[{"type":"anytls","tag":"日常工作代理","server":"proxy.example","server_port":443,"password":"synthetic-snapshot-only","tls":{"enabled":true}},{"type":"snell","tag":"备用线路","server":"relay.example","server_port":443,"version":5,"psk":"synthetic-snapshot-snell"}]`)
					if err != nil {
						t.Fatal(err)
					}
					if mode == "imported" {
						n := nodes[0]
						a.routes[0].Proxy = connectcfg.Proxy{Mode: n.Kind, Address: n.Address, Config: n.Config}
						a.editSavedRoute(&a.routes[0], false)
					} else {
						a.editSavedRoute(nil, false)
						a.prompt.route.nodes, a.prompt.route.selectedNode = nodes, "0"
					}
				}
				tt := ui.NewTester(func(c *ui.Context) {
					if mode == "library" {
						a.settingsWindowView(c)
					} else {
						a.view(c)
					}
				}, style.width, 820)
				tt.SetLocale(style.locale)
				tt.SetDark(style.dark)
				file, err := os.OpenFile(filepath.Join(dir, "routes-"+mode+"-"+style.name+".png"), os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0600)
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
