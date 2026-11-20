package desktop

import (
	"github.com/egoist/mygo/ui"
	"sshclient/internal/vault"
	"strings"
	"testing"
)

func TestTunnelActivityShowsOnlyRunningServicesAndStopsWithoutOpeningSettings(t *testing.T) {
	for _, width := range []int{560, 1440} {
		a := fileFollowDemo()
		a.connections = append(a.connections, vault.Summary{ID: "proxy", Name: "办公跳板"})
		stopped := [2]bool{}
		for i, kind := range []string{"socks5", "local"} {
			run := &tunnelRun{host: "proxy", kind: kind, running: true, ready: true, address: []string{"127.0.0.1:1080", "127.0.0.1:8080"}[i]}
			run.cancel = func() { stopped[i], run.running = true, false }
			a.tunnels.active = append(a.tunnels.active, run)
		}
		a.tunnels.active = append(a.tunnels.active, &tunnelRun{host: "proxy", kind: "remote", address: "stopped", ready: true}, &tunnelRun{host: "proxy", kind: "socks5", running: true, address: "starting"})
		tt := ui.NewTester(a.view, width, 900)
		icon, ok := tt.Find("端口转发")
		if !ok || icon.X < float32(width)/2 || icon.X+icon.W > float32(width) {
			t.Fatal("running icon missing or outside right header", icon)
		}
		settings, _ := tt.Find("设置")
		if icon.Y != settings.Y || icon.H != settings.H || icon.W != settings.W {
			t.Fatal("proxy icon differs from adjacent actions", icon, settings)
		}
		if err := tt.RightClick("端口转发"); err != nil {
			t.Fatal(err)
		}
		for _, label := range tt.Menu() {
			if strings.Contains(label, "stopped") || strings.Contains(label, "starting") {
				t.Fatal("inactive source in menu", label)
			}
		}
		if err := tt.ChooseMenuItem("停止 办公跳板 · SOCKS5 · 127.0.0.1:1080"); err != nil {
			t.Fatal(err)
		}
		if !stopped[0] || stopped[1] || a.tunnels.open {
			t.Fatal("quick stop affected another service or opened a dialog")
		}
		if err := tt.RightClick("端口转发"); err != nil {
			t.Fatal(err)
		}
		if err := tt.ChooseMenuItem("停止 办公跳板 · 本地转发 · 127.0.0.1:8080"); err != nil {
			t.Fatal(err)
		}
		if !stopped[1] {
			t.Fatal("second service not stopped")
		}
		if _, ok := tt.Find("端口转发"); !ok {
			t.Fatal("permanent service icon disappeared")
		}
	}
}

func TestTunnelActivityStopsAllActiveServices(t *testing.T) {
	a := fileFollowDemo()
	canceled := 0
	for range 2 {
		a.tunnels.active = append(a.tunnels.active, &tunnelRun{host: "a", kind: "socks5", running: true, ready: true, cancel: func() { canceled++ }})
	}
	tt := ui.NewTester(a.view, 1080, 800)
	if err := tt.RightClick("端口转发"); err != nil {
		t.Fatal(err)
	}
	if err := tt.ChooseMenuItem("停止全部服务"); err != nil {
		t.Fatal(err)
	}
	if canceled != 2 {
		t.Fatal("stop-all skipped a service", canceled)
	}
}

func TestServiceListAndCreationStaySeparateAndDefaultToCurrentServer(t *testing.T) {
	a := fileFollowDemo()
	a.selected = ""
	current := a.currentTerminal().host
	a.tunnels.active = []*tunnelRun{{host: current, purpose: "website", kind: "local", address: "127.0.0.1:50821", ready: true, running: true}}
	tt := ui.NewTester(a.view, 900, 800)
	if err := tt.Click("端口转发"); err != nil {
		t.Fatal(err)
	}
	if !a.tunnels.showingServices || tt.HasText("使用服务器") || !tt.HasText("http://127.0.0.1:50821") {
		t.Fatal("view services mixed with creation form", tt.Texts())
	}
	if err := tt.Click("新建服务…"); err != nil {
		t.Fatal(err)
	}
	if a.tunnels.showingServices || a.tunnels.host != current || !tt.HasText("让服务器访问本机服务") || tt.HasText("http://127.0.0.1:50821") {
		t.Fatal("new service did not use current host or hide old rows")
	}
}
