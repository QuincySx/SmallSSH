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
	"sshclient/internal/connectcfg"
	"sshclient/internal/desktop/components"
	"sshclient/internal/vault"
)

func TestConnectionSectionSnapshotsWithoutWindows(t *testing.T) {
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
	for _, section := range connectionSections {
		for _, style := range []struct {
			name, locale    string
			width           int
			dark, keepalive bool
		}{{"wide", "zh-Hans", 1100, false, false}, {"narrow", "zh-Hans", 560, false, false}, {"dark", "zh-Hans", 1100, true, false}, {"english", "en", 560, false, false}, {"keepalive-wide", "zh-Hans", 1100, false, true}, {"keepalive-narrow", "zh-Hans", 560, true, true}, {"keepalive-english", "en", 560, false, true}} {
			if style.keepalive && section.id != connectionTerminal {
				continue
			}
			t.Run(section.id+"-"+style.name, func(t *testing.T) {
				t.Setenv("MYGO_LOCALE", style.locale)
				a := &app{ctx: context.Background(), online: true, toolsHidden: true, sidebarWidth: 190, groups: map[string]*bool{}}
				a.newConnection()
				a.form.Name, a.form.Host, a.form.User = "示例服务器", "192.0.2.10", "example"
				a.form.Network.Proxy = connectcfg.Proxy{Mode: "socks5", Address: "127.0.0.1:1080", User: "example"}
				a.connections = []vault.Summary{{ID: "relay", Name: "自有 SSH 中转"}}
				if style.keepalive {
					a.form.Network.KeepaliveSeconds = 30
				}
				a.connection.section = section.id
				widths := make(map[string]float32)
				var sidebarLabel, contentLabel string
				tt := ui.NewTester(func(c *ui.Context) {
					widths = make(map[string]float32)
					sidebarLabel = tr(c, "连接分类")
					contentLabel = tr(c, "连接分类内容")
					for _, item := range connectionSections {
						label := tr(c, item.title)
						widths[tr(c, "连接分类：")+label], _ = c.MeasureText(0, ui.Span{Text: label, Size: 13})
					}
					a.view(c)
				}, style.width, 760)
				tt.SetLocale(style.locale)
				tt.SetDark(style.dark)
				left, ok := tt.Find(sidebarLabel)
				right, rightOK := tt.Find(contentLabel)
				if !ok || !rightOK || left.W < 128 || right.X < left.X+left.W || right.X+right.W > float32(style.width) {
					t.Fatal("split navigation missing", left, right)
				}
				for label, width := range widths {
					button, found := tt.Find(label)
					available := button.W - components.NavigationIconSlotSize - components.NavigationTextGap - 8
					if !found || available+0.5 < width || button.X < left.X || button.X+button.W > left.X+left.W {
						t.Fatalf("navigation text %q clipped: %v, available %v, natural width %v", label, button, available, width)
					}
				}
				name := filepath.Join(dir, fmt.Sprintf("connection-%s-%s.png", section.id, style.name))
				f, err := os.OpenFile(name, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0600)
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
