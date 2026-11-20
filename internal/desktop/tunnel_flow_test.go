package desktop

import (
	"context"
	"testing"

	"sshclient/internal/vault"
)

func tunnelDiagramFixture(kind string) *app {
	a := &app{ctx: context.Background(), online: true, connections: []vault.Summary{{ID: "diagram", Name: "示例服务器", Host: "192.0.2.10", Port: 22, User: "demo"}}}
	a.tunnels = tunnelManager{open: true, host: "diagram", kind: kind, listen: "127.0.0.1:8080", target: "127.0.0.1:80"}
	return a
}
func TestTunnelDiagramShowsActualListenerExitAndLoopbackOwner(t *testing.T) {
	for _, kind := range []string{"local", "remote", "socks5"} {
		path := describeTunnel(kind, "127.0.0.1:8080", "127.0.0.1:80", "192.0.2.10:22")
		current := -1
		for i, hop := range path.hops {
			if hop.current {
				if current >= 0 {
					t.Fatal("multiple current-device highlights")
				}
				current = i
			}
		}
		want := 0
		if kind == "remote" {
			want = 1
		}
		if current != want {
			t.Fatal("current device is on the wrong side", kind, current)
		}
		switch kind {
		case "local":
			if path.hops[2].role != "服务器本机服务" || path.scope != "在本机开一个端口，通过 SSH 访问远端服务。" {
				t.Fatal(path)
			}
		case "remote":
			if path.hops[0].title != "SSH 服务器" || path.hops[2].role != "本机上的服务" || path.listenLabel != "服务器入口地址" {
				t.Fatal(path)
			}
		case "socks5":
			if path.hops[2].address != "任意 TCP 目标" || path.hops[0].role != "SOCKS5 入口" {
				t.Fatal(path)
			}
		}
	}
	remote := describeTunnel("remote", "127.0.0.1:9000", "192.0.2.44:8080", "192.0.2.10:22")
	if remote.hops[2].role != "从本机访问" {
		t.Fatal("non-loopback target was described as the current device")
	}
}
