package desktop

import (
	"strconv"
	"testing"

	"github.com/egoist/mygo/ui"
	"sshclient/internal/approval"
	"sshclient/internal/connectcfg"
	"sshclient/internal/vault"
)

func TestSavedConnectionImmediatelyReopensWithItsPublicOptions(t *testing.T) {
	h := newConnectionFlowHarness(t)
	p := h.fixture.Profile()
	p.Terminal = connectcfg.Terminal{Encoding: "gb18030", Backspace: "bs"}
	p.Network = connectcfg.Network{Proxy: connectcfg.Proxy{Mode: "socks5", Address: "127.0.0.1:1080", User: "example", Password: "synthetic proxy password"}, KeepaliveSeconds: 30}
	h.a.form, h.a.port = p, strconv.Itoa(p.Port)
	h.a.connection.trustedTarget = targetID(p.Host, p.Port)
	h.a.connection.trustedTarget = targetID(p.Host, p.Port)
	h.a.saveProfile()
	h.wait(func() bool { return h.a.page == 1 })
	h.a.selected = p.ID  // Saving does not activate tools for the new target.
	h.a.editConnection() // No poll/refresh between saving and reopening.
	if h.a.form.Terminal != p.Terminal || h.a.form.Network != p.Network.Public() {
		t.Fatal("immediate edit lost terminal or route options", h.a.form.Terminal, h.a.form.Network.Public())
	}
	if h.a.details.Network.Proxy.Password != "" || h.a.form.Network.Proxy.Password != "" {
		t.Fatal("public cache retained the proxy password")
	}
	change, ok := h.a.connectionDraft()
	if !ok || !change.KeepCredential || !change.KeepProxyCredential {
		t.Fatal("metadata edit did not preserve saved secrets", h.a.connection.errors)
	}
}

func TestConnectionTerminalAndNetworkOptionsSurviveEditing(t *testing.T) {
	saved := vault.Summary{ID: "fixture", Name: "fixture", Host: "127.0.0.1", Port: 22, User: "fixture", Auth: "password", Fingerprint: "SHA256:fixture", Terminal: connectcfg.Terminal{Encoding: "gb18030", Backspace: "bs"}, Network: connectcfg.Network{Proxy: connectcfg.Proxy{Mode: "socks5", Address: "127.0.0.1:1080", User: "fixture"}, KeepaliveSeconds: 30}}
	a := &app{online: true, selected: saved.ID, connections: []vault.Summary{saved}}
	a.editConnection()
	if a.connectionHasChanges() {
		t.Fatal("loading connection options made the editor dirty")
	}
	tester := ui.NewTester(a.connectionView, 900, 900)
	if err := tester.Click("连接分类：终端设置"); err != nil {
		t.Fatal(err)
	}
	for _, label := range []string{"字符编码", "退格序列", "SSH 保活间隔"} {
		if !tester.HasText(label) {
			t.Fatal("missing editable option", label, tester.Texts())
		}
	}
	if err := tester.Click("连接分类：加速与代理"); err != nil {
		t.Fatal(err)
	}
	for _, label := range []string{"网络代理", "自有 SSH 跳板机", "路径实测"} {
		if !tester.HasText(label) {
			t.Fatal("missing network option", label, tester.Texts())
		}
	}
	change, ok := a.connectionDraft()
	if !ok || change.Profile.Terminal != saved.Terminal || change.Profile.Network != saved.Network || !change.KeepCredential || !change.KeepProxyCredential {
		t.Fatal("metadata-only edit lost saved connection settings")
	}
	a.form.Terminal.Backspace = "del"
	if !a.connectionHasChanges() {
		t.Fatal("backspace change was not tracked")
	}
}
func TestTunnelApprovalShowsNetworkCapabilityAndBothEndpoints(t *testing.T) {
	r := approval.Request{ID: "fixture", Operation: approval.Operation{Host: "fixture", Kind: "tunnel", Tunnel: connectcfg.Tunnel{Kind: "remote", Listen: "127.0.0.1:8080", Target: "127.0.0.1:3306"}}}
	tester := ui.NewTester(func(c *ui.Context) { requestOperationReview(c, r) }, 600, 300)
	if !tester.HasText("127.0.0.1:8080") || !tester.HasText("127.0.0.1:3306") || !tester.HasText("remote") {
		t.Fatal("tunnel approval omitted direction or an endpoint", tester.Texts())
	}
	_, _, caution := approvalPermission(r)
	if caution == "" {
		t.Fatal("tunnel approval omitted its capability warning")
	}
}

func TestConnectionOptionSelectionsUpdateSavedValues(t *testing.T) {
	a := &app{online: true}
	a.newConnection()
	tester := ui.NewTester(a.connectionView, 900, 900)
	if err := tester.Click("连接分类：终端设置"); err != nil {
		t.Fatal(err)
	}
	if e := tester.Click("字符编码选择"); e != nil {
		t.Fatal(e)
	}
	if e := tester.Click("GB18030"); e != nil {
		t.Fatal(e)
	}
	if a.form.Terminal.Encoding != "gb18030" {
		t.Fatal("encoding dropdown did not update the saved field")
	}
	if e := tester.Click("SSH 保活间隔选择"); e != nil {
		t.Fatal(e)
	}
	if e := tester.Click("30 秒"); e != nil {
		t.Fatal(e)
	}
	if a.form.Network.KeepaliveSeconds != 30 {
		t.Fatal("keepalive dropdown lost its chosen value between frames")
	}
}

func TestKeepaliveHelpExplainsStatesAndPreservesTheInactiveWaitLimit(t *testing.T) {
	a := &app{online: true}
	a.newConnection()
	a.connection.section = connectionTerminal
	tt := ui.NewTester(a.connectionView, 1000, 900)
	if !tt.HasText("关：不发送探测") || tt.HasText("最长等待") || a.connectionHasChanges() {
		t.Fatal("disabled keepalive was not explained or viewing help changed the draft")
	}
	if err := tt.Click("SSH 保活间隔选择"); err != nil {
		t.Fatal(err)
	}
	if err := tt.Click("30 秒"); err != nil {
		t.Fatal(err)
	}
	if !tt.HasText("开：定时探测连接") || !tt.HasText("最长等待 90 秒") {
		t.Fatal("default wait limit did not explain the actual response deadline")
	}
	if err := tt.Click("无响应等待上限选择"); err != nil {
		t.Fatal(err)
	}
	if err := tt.Click("5 个保活间隔"); err != nil {
		t.Fatal(err)
	}
	if a.form.Network.KeepaliveFailures != 5 || !tt.HasText("最长等待 150 秒") {
		t.Fatal("wait selection did not update its plain-language deadline")
	}
	if err := tt.Click("SSH 保活间隔选择"); err != nil {
		t.Fatal(err)
	}
	if err := tt.Click("关闭"); err != nil {
		t.Fatal(err)
	}
	_ = tt.Click("无响应等待上限选择")
	if tt.HasText("1 个保活间隔") || a.form.Network.KeepaliveFailures != 5 || a.form.Network.KeepaliveSeconds != 0 {
		t.Fatal("inactive wait control opened or discarded its saved preference")
	}
	if err := tt.Click("SSH 保活间隔选择"); err != nil {
		t.Fatal(err)
	}
	if err := tt.Click("30 秒"); err != nil {
		t.Fatal(err)
	}
	if !tt.HasText("最长等待 150 秒") || a.form.Network.KeepaliveFailures != 5 {
		t.Fatal("re-enabling keepalive did not restore its wait preference")
	}
}
