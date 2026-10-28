//go:build preview_qa

package desktop

import (
	"context"
	"crypto/ed25519"
	"encoding/base64"
	"fmt"
	"image/png"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"
	"sshclient/internal/approval"
	"sshclient/internal/vault"
)

func TestDelegationApprovalSnapshotsWithoutWindows(t *testing.T) {
	if os.Getenv("SSH_CLIENT_PREVIEW_QA") != "1" {
		t.Skip("opt-in offline preview")
	}
	dir := os.Getenv("SSH_CLIENT_UI_SNAPSHOT_DIR")
	if !filepath.IsAbs(dir) {
		t.Fatal("absolute snapshot directory required")
	}
	if err := os.MkdirAll(dir, 0700); err != nil {
		t.Fatal(err)
	}
	pub := ed25519.NewKeyFromSeed(make([]byte, ed25519.SeedSize)).Public().(ed25519.PublicKey)
	for _, permission := range []string{"read", "full"} {
		for _, dark := range []bool{false, true} {
			name := permission
			if dark {
				name += "-dark"
			}
			t.Run(name, func(t *testing.T) {
				a := &app{ctx: context.Background(), online: true, page: 3, sidebarWidth: 196, selected: "demo", groups: map[string]*bool{}, connections: []vault.Summary{{ID: "demo", Name: "演示服务器", Group: "个人服务", Host: "192.0.2.10", Port: 22, User: "deploy"}}}
				a.requests = []approval.Request{{ID: "demo-parent-authorization", Status: "pending", Expires: time.Date(2026, 10, 21, 12, 5, 0, 0, time.UTC), Operation: approval.Operation{Host: "demo", Endpoint: "deploy@192.0.2.10:22", HostFingerprint: "SHA256:DEMO_HOST_FINGERPRINT", Client: "演示 AI 工具", Kind: "grant", Grant: approval.GrantScope{PublicKey: base64.RawURLEncoding.EncodeToString(pub), Permission: permission, LifetimeSeconds: 7200, IdleSeconds: 900}}}}
				tt := ui.NewTester(a.workspaceView, 1280, 900)
				tt.SetDark(dark)
				for _, label := range []string{"允许", "不允许", "保险库主密码", a.requests[0].Operation.Grant.PairingCode()} {
					if !tt.HasText(label) {
						t.Fatal("authorization controls missing", label)
					}
				}
				f, err := os.Create(filepath.Join(dir, fmt.Sprintf("delegation-%s.png", name)))
				if err != nil {
					t.Fatal(err)
				}
				err = png.Encode(f, tt.Image())
				closed := f.Close()
				if err != nil || closed != nil {
					t.Fatal(err, closed)
				}
			})
		}
	}
}
