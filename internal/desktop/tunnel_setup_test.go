package desktop

import (
	"github.com/egoist/mygo/ui"
	"runtime"
	"sshclient/internal/vault"
	"testing"
)

func TestTunnelDiagramSelectsThreeModesAndEditsEndpointsOnNodes(t *testing.T) {
	a := tunnelDiagramFixture("local")
	tt := ui.NewTester(a.tunnelsView, 900, 800)
	for _, label := range []string{"访问远端服务", "让服务器访问本机服务", "把服务器当代理", "服务端口输入", "本机入口端口输入", "使用服务器选择", "服务端口主机输入", "启动后打开网页"} {
		if _, ok := tt.Find(label); !ok {
			t.Fatal("scene or inline control missing", label)
		}
	}
	if _, ok := tt.Find("高级设置"); ok {
		t.Fatal("endpoint options are hidden behind advanced settings")
	}
	if err := tt.Click("服务端口主机输入"); err != nil {
		t.Fatal(err)
	}
	mod := ui.Ctrl
	if runtime.GOOS == "darwin" {
		mod = ui.Super
	}
	tt.Key(mod, ui.KeyA)
	tt.Type("10.0.0.8")
	if err := tt.Click("服务端口输入"); err != nil {
		t.Fatal(err)
	}
	tt.Key(mod, ui.KeyA)
	tt.Type("8080")
	if a.tunnels.target != "10.0.0.8:8080" {
		t.Fatal("inline port edit replaced target IP", a.tunnels.target)
	}
	if err := tt.Click("让服务器访问本机服务"); err != nil {
		t.Fatal(err)
	}
	if a.tunnels.kind != "remote" || a.tunnels.listen != "127.0.0.1:0" || a.tunnels.target != "127.0.0.1:3000" {
		t.Fatal("remote mode did not default to an automatic server-side port", a.tunnels)
	}
	if !tt.HasText("在服务器本机使用") {
		t.Fatal("remote entry lacks device context")
	}
	if tt.HasText("启动后打开网页") {
		t.Fatal("remote mode shows the local browser option")
	}
	if err := tt.Click("访问远端服务"); err != nil {
		t.Fatal(err)
	}
	if a.tunnels.target != "10.0.0.8:8080" {
		t.Fatal("switching scenes discarded the edited endpoint")
	}
	if err := tt.Click("启动后打开网页"); err != nil {
		t.Fatal(err)
	}
	if _, ok := tt.Find("网页协议选择"); !ok || !a.tunnels.browse {
		t.Fatal("browser option did not show the protocol picker")
	}
}

func TestTunnelDiagramOffersProxyOnTheLineWithoutFixedSOCKSTarget(t *testing.T) {
	a := tunnelDiagramFixture("local")
	tt := ui.NewTester(a.tunnelsView, 900, 1000)
	if err := tt.Click("把服务器当代理"); err != nil {
		t.Fatal(err)
	}
	if a.tunnels.kind != "socks5" {
		t.Fatal("SOCKS scene did not switch")
	}
	if _, ok := tt.Find("服务端口输入"); ok {
		t.Fatal("SOCKS asks for a fixed destination")
	}
	if tt.HasText("高级设置") || tt.HasText("已有 SOCKS5 代理") {
		t.Fatal("SOCKS displays irrelevant settings")
	}
	if err := tt.Click("选择线路"); err != nil {
		t.Fatal(err)
	}
	if err := tt.ChooseMenuItem("本机已有 SOCKS5"); err != nil {
		t.Fatal(err)
	}
	if a.tunnels.listen != "127.0.0.1:1081" || a.tunnels.localProxy != "127.0.0.1:1080" {
		t.Fatal("upstream conflicts with listener")
	}
	if !tt.HasText("已有 SOCKS5 代理") {
		t.Fatal("the selected upstream address cannot be edited")
	}
	if err := tt.Click("移除中转"); err != nil {
		t.Fatal(err)
	}
	if tt.HasText("已有 SOCKS5 代理") || tt.HasText("移除中转") || a.tunnels.selectedRoute().Mode != "direct" {
		t.Fatal("removing the upstream did not restore a direct line")
	}
	if err := tt.Click("选择线路"); err != nil {
		t.Fatal("the add control did not return", err)
	}
}

func TestTunnelProxySelectionReusesAnExistingLocalRoute(t *testing.T) {
	a := tunnelDiagramFixture("local")
	a.routes = []vault.RouteSummary{{ID: "local-proxy", Name: "Local proxy"}}
	a.routes[0].Proxy.Mode = "socks5"
	a.routes[0].Proxy.Address = "127.0.0.1:1080"
	r, err := a.localTunnelProxy("127.0.0.1:1080")
	if err != nil || r.ID != "local-proxy" {
		t.Fatal("local proxy was duplicated", r, err)
	}
	if _, err := a.localTunnelProxy("192.0.2.10:1080"); err == nil {
		t.Fatal("remote proxy mislabeled as local")
	}
}
