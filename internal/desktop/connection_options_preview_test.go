//go:build preview_qa

package desktop

import (
	"context"
	"image/png"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"
	"sshclient/internal/approval"
	"sshclient/internal/connectcfg"
	"sshclient/internal/desktop/components"
	"sshclient/internal/hardware"
	"sshclient/internal/vault"
)

func TestConnectionCapabilitiesSnapshotsWithoutWindows(t *testing.T) {
	if os.Getenv("SSH_CLIENT_PREVIEW_QA") != "1" {
		t.Skip("opt-in offline capability snapshots")
	}
	directory := os.Getenv("SSH_CLIENT_UI_SNAPSHOT_DIR")
	if !filepath.IsAbs(directory) {
		t.Fatal("absolute snapshot path required")
	}
	if e := os.MkdirAll(directory, 0700); e != nil {
		t.Fatal(e)
	}
	for _, variant := range []string{"terminal-network", "terminal-network-bottom", "terminal-network-dark", "hardware", "tunnels", "tunnel-review"} {
		t.Run(variant, func(t *testing.T) {
			a := &app{ctx: context.Background(), online: true, selected: "fixture", connections: []vault.Summary{{ID: "fixture", Name: "本机演示连接", Host: "192.0.2.10", User: "example", Port: 22}, {ID: "relay", Name: "自有中转"}}}
			a.newConnection()
			a.connection.replaceProxy = true
			a.form.Network.Proxy = connectcfg.Proxy{Mode: "socks5", Address: "127.0.0.1:1080", User: "example"}
			a.form.Terminal = connectcfg.Terminal{Encoding: "gb18030", Backspace: "bs"}
			var view func(*ui.Context)
			switch variant {
			case "terminal-network", "terminal-network-bottom", "terminal-network-dark":
				view = func(c *ui.Context) {
					open := true
					components.Dialog(c, &open, components.DialogOptions{Title: "连接选项", Width: 640, Height: 850}, func() { a.connectionNetworkForm(c) }, nil)
				}
			case "hardware":
				a.form.Auth = "pkcs11"
				a.form.Hardware = connectcfg.Hardware{Module: "/example/pkcs11/provider.dylib", ModuleSHA256: "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef", TokenSerial: "EXAMPLE0001", KeyID: "01", Fingerprint: "SHA256:EXAMPLE_PUBLIC_FINGERPRINT"}
				a.connection.hardwareKeys = []hardware.Key{{Token: "示例 PKCS#11 令牌", Model: "PIV / PKCS#11", Serial: "EXAMPLE0001", ID: "01", Algorithm: "ecdsa-sha2-nistp256", Fingerprint: "SHA256:EXAMPLE_PUBLIC_FINGERPRINT"}}
				view = func(c *ui.Context) {
					open := true
					components.Dialog(c, &open, components.DialogOptions{Title: "硬件密钥", Width: 640, Height: 800}, func() { a.hardwareForm(c) }, nil)
				}
			case "tunnels":
				a.openTunnels("fixture")
				a.tunnels.active = []*tunnelRun{{host: "fixture", kind: "local", address: "127.0.0.1:15432", status: "演示 · 未启动网络", running: false}}
				view = a.tunnelsView
			case "tunnel-review":
				view = func(c *ui.Context) {
					ui.Column(c).FillWidth().Padding(24).Children(func() {
						requestReview(c, approval.Request{ID: "synthetic-tunnel", Expires: time.Now().Add(15 * time.Minute), Operation: approval.Operation{Kind: "tunnel", Host: "fixture", Client: "演示调用方", Endpoint: "example@127.0.0.1:22", Tunnel: connectcfg.Tunnel{Kind: "remote", Listen: "127.0.0.1:5432", Target: "127.0.0.1:15432"}}})
					})
				}
			}
			tester := ui.NewTester(view, 900, 950)
			tester.SetDark(variant == "terminal-network-dark")
			if variant == "terminal-network-bottom" {
				tester.Scroll(450, 700, 0, 600)
				tester.Frame()
			}
			path := filepath.Join(directory, variant+".png")
			file, e := os.OpenFile(path, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0600)
			if e != nil {
				t.Fatal(e)
			}
			e = png.Encode(file, tester.Image())
			closeError := file.Close()
			if e != nil || closeError != nil {
				t.Fatal(e, closeError)
			}
			t.Log(path)
		})
	}
}
