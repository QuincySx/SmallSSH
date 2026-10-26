//go:build preview_qa

package desktop

import (
	"context"
	"image/png"
	"os"
	"path/filepath"
	"testing"

	"github.com/egoist/mygo/ui"
	"sshclient/internal/sshconn"
	"sshclient/internal/vault"
)

// Explicit, offline visual QA. It never starts MyGo.App, native dialogs, SSH or
// a production vault; all labels and paths below are documentation examples.
func TestConnectionFormSnapshotsWithoutWindows(t *testing.T) {
	if os.Getenv("SSH_CLIENT_PREVIEW_QA") != "1" {
		t.Skip("opt-in offline form snapshots")
	}
	dir := os.Getenv("SSH_CLIENT_UI_SNAPSHOT_DIR")
	if !filepath.IsAbs(dir) {
		t.Fatal("absolute snapshot directory required")
	}
	if err := os.MkdirAll(dir, 0700); err != nil {
		t.Fatal(err)
	}
	for _, variant := range []string{"password", "private-key", "edit", "small-window", "identity", "groups", "tabs", "dark"} {
		t.Run(variant, func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			a := &app{ctx: ctx, online: true, groups: map[string]*bool{}, sidebarWidth: 190, toolsHidden: true}
			a.newConnection()
			a.form.Name, a.form.Host, a.form.User = "示例服务器", "192.0.2.10", "root"
			a.connections = []vault.Summary{{ID: "prod", Name: "生产服务器", Group: "生产环境"}, {ID: "dev", Name: "测试服务器", Group: "开发测试"}}
			if variant == "identity" {
				a.connection.identity = &sshconn.HostIdentity{Host: "192.0.2.10", Port: 22, Algorithm: "ssh-ed25519", Fingerprint: "SHA256:w1H85gE9C7qRE4zP0O4Wp5vM7Tg5J2Gr8RcbX6cVxfk"}
				a.connection.review = true
			}
			if variant == "tabs" {
				a.page = 1
				a.sessions = []*session{{name: "生产服务器", host: "prod"}, {name: "开发测试", host: "dev"}}
			}
			if variant == "private-key" {
				a.form.Auth = "key"
				a.connection.authLabel = "已有私钥"
				a.keyFile = filepath.Join("example", "ssh-key.pem")
			}
			if variant == "edit" {
				a.connections = []vault.Summary{{ID: "example", Name: "示例服务器", Host: "192.0.2.10", Port: 22, User: "root", Auth: "password", Fingerprint: "SHA256:EXAMPLE_PUBLIC_FINGERPRINT"}}
				a.selected = "example"
				a.editConnection()
			}
			width, height := 1280, 900
			if variant == "small-window" {
				width, height = 1000, 760
			}
			tt := ui.NewTester(a.view, width, height)
			tt.SetDark(variant == "dark")
			if variant == "groups" {
				if err := tt.Click("分组输入"); err != nil {
					t.Fatal(err)
				}
			}
			path := filepath.Join(dir, "connection-"+variant+".png")
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
		})
	}
}
