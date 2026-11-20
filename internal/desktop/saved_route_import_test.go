package desktop

import (
	"encoding/json"
	"runtime"
	"strings"
	"testing"

	"github.com/egoist/mygo/ui"
	"sshclient/internal/proxyconfig"
)

func TestSavedRouteImportSnellV6ObjectAndArray(t *testing.T) {
	const node = `{"type":"snell","tag":"Snell v6 fixture","server":"proxy.invalid","server_port":76,"version":6,"reuse":true,"psk":"synthetic-snell6-ui-secret","mode":"default"}`
	for _, input := range []string{node, "[" + node + "]"} {
		h := newConnectionFlowHarness(t)
		a := h.a
		a.connection.section = connectionNetwork
		h.ui = ui.NewTester(func(c *ui.Context) { a.connectionView(c); a.promptView(c) }, 820, 950)
		h.click("新建并选用线路")
		h.click("线路导入内容")
		h.ui.Type(input)
		h.click("解析节点")
		s := a.prompt.route
		if s.error != "" || s.importText != "" || s.value.Proxy.Mode != "snell" || h.ui.HasText("synthetic-snell6-ui-secret") {
			t.Fatal("v6 import failed or exposed input", s.error)
		}
		h.click("保存线路")
		h.wait(func() bool { return a.prompt.route == nil || !a.prompt.route.saving })
		if a.prompt.route != nil || len(a.routes) != 1 {
			t.Fatal("v6 route was not saved")
		}
		route, err := h.server.Vault.ResolveRoute(a.form)
		if err != nil {
			t.Fatal(err)
		}
		p := route[0].Network.Proxy
		raw, err := proxyconfig.Outbound(p.Mode, p.Address, p.Config, p.Password, true)
		if err != nil {
			t.Fatal("saved v6 outbound invalid", err)
		}
		var config proxyconfig.Config
		if err := json.Unmarshal(raw, &config); err != nil || config.Version != 6 || config.Mode != "default" || !config.Reuse || config.PSK != "synthetic-snell6-ui-secret" {
			t.Fatal("v6 settings changed on save", err)
		}
		h.click("编辑所选线路")
		if h.ui.HasText("synthetic-snell6-ui-secret") {
			t.Fatal("editing revealed stored credential")
		}
	}
}

func TestSavedRouteImportPreviewSaveRedactEditAndCancel(t *testing.T) {
	h := newConnectionFlowHarness(t)
	a := h.a
	a.connection.section = connectionNetwork
	h.ui = ui.NewTester(func(c *ui.Context) { a.connectionView(c); a.promptView(c) }, 820, 950)
	input := func(text string) {
		t.Helper()
		h.click("线路导入内容")
		modifier := ui.Ctrl
		if runtime.GOOS == "darwin" {
			modifier = ui.Super
		}
		h.ui.Key(modifier, ui.KeyA)
		h.ui.Type(text)
	}
	h.click("新建并选用线路")
	input(`anytls://synthetic-import-ui-secret@proxy.invalid:443#Imported`)
	h.click("解析节点")
	if a.prompt.route.value.Proxy.Mode != "anytls" || a.prompt.route.importText != "" || h.ui.HasText("synthetic-import-ui-secret") {
		t.Fatal("import did not produce a redacted preview")
	}
	h.click("保存线路")
	h.wait(func() bool { return a.prompt.route == nil || !a.prompt.route.saving })
	if a.prompt.route != nil || len(a.routes) != 1 || a.form.Network.RouteID != a.routes[0].ID {
		t.Fatal("imported route not saved and selected")
	}
	route, err := h.server.Vault.ResolveRoute(a.form)
	if err != nil || !strings.Contains(route[0].Network.Proxy.Password, "synthetic-import-ui-secret") {
		t.Fatal("imported credential not stored", err)
	}
	h.click("编辑所选线路")
	if a.prompt.route.mode != "import" || a.prompt.route.value.Proxy.Password != "" || h.ui.HasText("线路导入内容") {
		t.Fatal("editing backfilled a secret")
	}
	h.click("保存线路")
	h.wait(func() bool { return a.prompt.route == nil || !a.prompt.route.saving })
	if a.prompt.route != nil {
		t.Fatal("redacted edit failed", a.prompt.route.error)
	}
	h.click("编辑所选线路")
	h.click("重新导入节点")
	input(`hy2://synthetic-replacement-ui-secret@updated.invalid:444#Replacement`)
	h.click("解析节点")
	h.click("保存线路")
	h.wait(func() bool { return a.prompt.route == nil || !a.prompt.route.saving })
	if a.prompt.route != nil {
		t.Fatal("reimport failed", a.prompt.route.error)
	}
	updated, err := h.server.Vault.ResolveRoute(a.form)
	if err != nil || updated[0].Network.Proxy.Mode != "hysteria2" || updated[0].Network.Proxy.Address != "updated.invalid:444" || !strings.Contains(updated[0].Network.Proxy.Password, "synthetic-replacement-ui-secret") || route[0].Network.Proxy.Mode != "anytls" {
		t.Fatal("reimport lost configuration or mutated old snapshot", err)
	}
	h.click("编辑所选线路")
	h.click("重新导入节点")
	input(`anytls://synthetic-invalid-secret@proxy.invalid:443?insecure=1`)
	h.click("解析节点")
	if a.prompt.route.error == "" || strings.Contains(a.prompt.route.error, "synthetic-invalid-secret") {
		t.Fatal("invalid configuration accepted or error exposed credential")
	}
	h.click("取消")
	still, err := h.server.Vault.ResolveRoute(a.form)
	if err != nil || still[0].Network.Proxy != updated[0].Network.Proxy {
		t.Fatal("cancelled import changed saved route", err)
	}
}

func TestSavedRouteImportChoosesNodeWithoutRunningFullConfig(t *testing.T) {
	h := newConnectionFlowHarness(t)
	a := h.a
	a.connection.section = connectionNetwork
	h.ui = ui.NewTester(func(c *ui.Context) { a.connectionView(c); a.promptView(c) }, 820, 950)
	h.click("新建并选用线路")
	h.click("线路导入内容")
	h.ui.Type(`{"inbounds":[{"type":"tun"}],"experimental":{"debug":{"listen":"0.0.0.0:6060"}},"outbounds":[{"type":"anytls","tag":"First","server":"first.invalid","server_port":443,"password":"synthetic-first","tls":{"enabled":true}},{"type":"snell","tag":"Second","server":"second.invalid","server_port":443,"version":5,"psk":"synthetic-second"}]}`)
	h.click("解析节点")
	if len(a.prompt.route.nodes) != 2 || a.prompt.route.importText != "" {
		t.Fatal("multiple nodes did not offer selection")
	}
	h.click("选择导入节点选择")
	h.click("Second · Snell · second.invalid:443")
	h.click("使用所选节点")
	h.click("保存线路")
	h.wait(func() bool { return a.prompt.route == nil || !a.prompt.route.saving })
	if a.prompt.route != nil || len(a.routes) != 1 || a.routes[0].Proxy.Mode != "snell" || a.routes[0].Proxy.Address != "second.invalid:443" || strings.Contains(a.routes[0].Proxy.Config, "experimental") {
		t.Fatal("node selection or config extraction failed")
	}
}
