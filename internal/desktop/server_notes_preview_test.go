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
	"sshclient/internal/monitor"
	"sshclient/internal/servernotes"
	"sshclient/internal/vault"
)

func TestServerNotesSnapshotsWithoutWindows(t *testing.T) {
	if os.Getenv("SSH_CLIENT_PREVIEW_QA") != "1" {
		t.Skip("opt-in offline rendering")
	}
	dir := os.Getenv("SSH_CLIENT_UI_SNAPSHOT_DIR")
	if !filepath.IsAbs(dir) {
		t.Fatal("absolute output directory required")
	}
	if err := os.MkdirAll(dir, 0700); err != nil {
		t.Fatal(err)
	}
	for _, variant := range []string{"default", "empty", "expanded", "narrow", "small", "dark", "discard"} {
		t.Run(variant, func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			a := &app{online: true, ctx: ctx}
			target := vault.Summary{ID: "fixture", Name: "个人服务器", Host: "192.0.2.10", Port: 22, User: "deploy", Fingerprint: "SHA256:fixture"}
			report := servernotes.Report{Inventory: monitor.Inventory{OS: "Debian GNU/Linux 12", Manager: "systemd", Services: []monitor.Service{
				{Name: "apparmor.service", Active: "active", Sub: "exited"},
				{Name: "substore.service", Active: "active", Sub: "running"},
				{Name: "openresty.service", Active: "active", Sub: "running"},
				{Name: "frps.service", Active: "active", Sub: "running"},
			}, Docker: &monitor.DockerInfo{Status: "missing"}}, Ports: []string{"TCP 0.0.0.0:443"}, Domains: []servernotes.Domain{
				{Name: "sub.example.test", Status: "匹配已知地址", Source: "/etc/nginx/conf.d/sub.conf"},
				{Name: "www.example.test", Status: "匹配已知地址", Source: "/etc/nginx/conf.d/web.conf"},
				{Name: "example.test", Status: "未匹配已知地址（可能经 CDN / 代理）", IPs: []string{"198.51.100.7"}, Source: "/etc/nginx/conf.d/web.conf"},
			}}
			note := vault.ServerNote{Manual: "个人网站、订阅和内网穿透。\n变更网站前核对域名。", Discovered: report.Text(), Details: report.Details(), CheckedAt: time.Date(2026, 10, 22, 4, 0, 0, 0, time.UTC), Target: vault.NoteTarget(target)}
			a.serverNotes = &serverNotesState{ctx: ctx, cancel: cancel, target: target, original: note, draft: note, expanded: variant == "expanded", discard: variant == "discard"}
			if variant == "empty" {
				a.serverNotes.original = vault.ServerNote{}
				a.serverNotes.draft = vault.ServerNote{}
			}
			width, height := 760, 650
			if variant == "narrow" {
				width, height = 560, 600
			}
			if variant == "small" {
				width, height = 480, 480
			}
			tt := ui.NewTester(a.serverNotesDialog, width, height)
			tt.SetDark(variant == "dark")
			for _, label := range []string{"服务器备注输入", "保存备注"} {
				if variant != "discard" {
					b, ok := tt.Find(label)
					if !ok || b.X < 0 || b.Y < 0 || b.X+b.W > float32(width) || b.Y+b.H > float32(height) {
						t.Fatal("note control clipped", label, b)
					}
				}
			}
			f, err := os.Create(filepath.Join(dir, "server-notes-"+variant+".png"))
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
