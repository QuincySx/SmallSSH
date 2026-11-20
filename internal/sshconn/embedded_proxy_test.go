//go:build with_utls && with_quic

package sshconn

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"sshclient/internal/proxyconfig"
	"sshclient/internal/testproxy"
	"sshclient/internal/testserver"
)

func TestEmbeddedProxyProtocolsCarryPinnedSSHAndSFTP(t *testing.T) {
	for _, kind := range []string{"shadowsocks", "snell", "snell6", "vless", "reality", "anytls", "hysteria2"} {
		t.Run(kind, func(t *testing.T) {
			proxy := testproxy.New(t, kind)
			server := testserver.New(t)
			profile := server.Profile()
			profile.Network.Proxy = proxy
			ctx, cancel := context.WithTimeout(context.Background(), 12*time.Second)
			defer cancel()
			identity, err := ProbeRoute(ctx, profile.Host, profile.Port, profile.Network, nil)
			if err != nil || identity.Fingerprint != profile.Fingerprint {
				t.Fatal("host identity probe through proxy failed", err)
			}
			client, err := Dial(ctx, profile)
			if err != nil {
				t.Fatal("authenticated proxy SSH failed", err)
			}
			defer client.Close()
			if result, err := client.Exec(ctx, "synthetic-proxy-command"); err != nil || result.ExitCode != 0 {
				t.Fatal("SSH exec through proxy failed", err)
			}
			dir := t.TempDir()
			if err := os.WriteFile(filepath.Join(dir, "synthetic.txt"), []byte("fixture"), 0600); err != nil {
				t.Fatal(err)
			}
			files, err := client.List(ctx, testserver.RemotePath(dir))
			if err != nil || len(files) != 1 || files[0].Name != "synthetic.txt" {
				t.Fatal("SFTP through proxy failed", err)
			}
			changed := profile
			changed.Fingerprint = "SHA256:untrusted"
			if bad, err := Dial(ctx, changed); err == nil {
				bad.Close()
				t.Fatal("proxy bypassed SSH host identity verification")
			}
			if kind == "anytls" || kind == "hysteria2" || kind == "vless" {
				wrongPin := profile
				var node proxyconfig.Config
				if err := json.Unmarshal([]byte(proxy.Config), &node); err != nil {
					t.Fatal(err)
				}
				node.TLS.Pins = []string{base64.StdEncoding.EncodeToString(make([]byte, 32))}
				encoded, _ := json.Marshal(node)
				wrongPin.Network.Proxy.Config = string(encoded)
				if _, err := ProbeRoute(ctx, profile.Host, profile.Port, wrongPin.Network, nil); err == nil {
					t.Fatal("TLS certificate public-key pin was not enforced")
				}
			}
		})
	}
}
