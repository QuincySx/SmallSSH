package desktop

import (
	"runtime"
	"testing"

	"github.com/egoist/mygo/ui"
	"sshclient/internal/broker"
	"sshclient/internal/connectcfg"
	"sshclient/internal/vault"
)

func TestSavedRouteQuickCreateSelectEditPreservesPassword(t *testing.T) {
	h := newConnectionFlowHarness(t)
	a := h.a
	a.connection.section = connectionNetwork
	h.ui = ui.NewTester(func(c *ui.Context) { a.connectionView(c); a.promptView(c) }, 820, 950)
	h.click("新建并选用线路")
	if a.prompt.route == nil {
		t.Fatal("quick create did not open")
	}
	input := func(label, value string) {
		t.Helper()
		h.click(label)
		mod := ui.Ctrl
		if runtime.GOOS == "darwin" {
			mod = ui.Super
		}
		h.ui.Key(mod, ui.KeyA)
		h.ui.Type(value)
	}
	input("线路名称", "常用代理")
	h.click("线路类型选择")
	h.click("SOCKS5")
	input("线路代理地址", "127.0.0.1:1080")
	input("线路代理用户名", "fixture")
	input("线路代理密码", "synthetic shared route password")
	if h.ui.HasText("synthetic shared route password") {
		t.Fatal("password rendered in clear")
	}
	h.click("保存线路")
	h.wait(func() bool { return a.prompt.route == nil || !a.prompt.route.saving })
	if a.prompt.route != nil {
		t.Fatal("save failed", a.prompt.route.error)
	}
	if len(a.routes) != 1 || a.form.Network.RouteID != a.routes[0].ID || a.form.Network.Proxy != (connectcfg.Proxy{}) || a.form.Network.Relay != "" {
		t.Fatal("new route not selected", a.form.Network)
	}
	if h.ui.HasText("代理主机:端口") {
		t.Fatal("selected route still requires per-connection proxy fields")
	}
	h.click("编辑所选线路")
	if a.prompt.route == nil || !a.prompt.route.hasPassword || a.prompt.route.value.Proxy.Password != "" {
		t.Fatal("editing route exposed credential")
	}
	input("线路代理地址", "127.0.0.1:1081")
	h.click("保存线路")
	h.wait(func() bool { return a.prompt.route == nil || !a.prompt.route.saving })
	if a.prompt.route != nil {
		t.Fatal(a.prompt.route.error)
	}
	route, err := h.server.Vault.ResolveRoute(a.form)
	if err != nil || route[0].Network.Proxy.Address != "127.0.0.1:1081" || route[0].Network.Proxy.Password != "synthetic shared route password" {
		t.Fatal("route update lost password", err)
	}
	h.click("编辑所选线路")
	h.click("已存代理密码选择")
	h.click("更换或清空密码")
	input("线路代理密码", "synthetic replacement route password")
	h.click("保存线路")
	h.wait(func() bool { return a.prompt.route == nil || !a.prompt.route.saving })
	if a.prompt.route != nil {
		t.Fatal(a.prompt.route.error)
	}
	route, err = h.server.Vault.ResolveRoute(a.form)
	if err != nil || route[0].Network.Proxy.Password != "synthetic replacement route password" {
		t.Fatal("password replacement failed", err)
	}
	selected := a.form.Network.RouteID
	h.click("新建并选用线路")
	input("线路名称", "取消草稿")
	h.click("取消")
	if a.form.Network.RouteID != selected || len(a.routes) != 1 {
		t.Fatal("cancel altered selected route")
	}
}

func TestSavedRouteQuickCreateSSHRelayAndDelete(t *testing.T) {
	h := newConnectionFlowHarness(t)
	a := h.a
	hop := h.fixture.Profile()
	hop.ID, hop.Name = "relay", "中转测试"
	if err := h.server.Vault.SaveProfile(broker.VaultPath(h.dir), hop); err != nil {
		t.Fatal(err)
	}
	a.connections = h.server.Vault.Summaries()
	a.connection.section = connectionNetwork
	a.form.Network.KeepaliveSeconds = 30
	h.ui = ui.NewTester(func(c *ui.Context) { a.connectionView(c); a.promptView(c) }, 820, 950)
	h.click("新建并选用线路")
	h.click("线路名称")
	h.ui.Type("复用中转")
	h.click("线路类型选择")
	h.click("SSH 跳板机")
	if h.ui.HasText("代理密码") {
		t.Fatal("SSH relay asks for proxy credentials")
	}
	h.click("跳板机选择")
	h.click("中转测试 · relay")
	h.click("保存线路")
	h.wait(func() bool { return a.prompt.route == nil || !a.prompt.route.saving })
	if a.prompt.route != nil {
		t.Fatal(a.prompt.route.error)
	}
	route, err := h.server.Vault.ResolveRoute(a.form)
	if err != nil || len(route) != 2 || route[1].ID != hop.ID || route[0].Network.KeepaliveSeconds != 30 {
		t.Fatal("shared relay not resolved", err)
	}
	// No saved connection references this route yet, so cancellation/deletion
	// remains available without launching a settings window.
	r := a.routes[0]
	a.removeSavedRoute(r)
	h.ui.Frame()
	h.click("取消")
	if len(h.server.Vault.Routes()) != 1 {
		t.Fatal("cancel deleted a route")
	}
	a.removeSavedRoute(r)
	h.ui.Frame()
	h.click("删除")
	h.wait(func() bool { return len(a.routes) == 0 || a.routeError != "" })
	if a.routeError != "" || len(h.server.Vault.Routes()) != 0 {
		t.Fatal("route delete failed", a.routeError)
	}
	if _, err := h.server.Vault.ResolveRoute(vault.Profile{Network: a.form.Network}); err == nil {
		t.Fatal("deleted draft route silently became direct")
	}
}

func TestSavedRouteSettingsCategoryAndMissingSelection(t *testing.T) {
	a := &app{online: true, settingsSection: settingsRoutes}
	tt := settingsTester(a, 1000, 700)
	if !tt.HasText("新建线路") || !tt.HasText("暂无已保存线路") {
		t.Fatal("route settings missing", tt.Texts())
	}
	a.form.Network.RouteID = "missing"
	tt = ui.NewTester(a.connectionNetworkForm, 700, 700)
	if !tt.HasText("线路不可用，请重新选择") {
		t.Fatal("missing route appeared as direct", tt.Texts())
	}
}
