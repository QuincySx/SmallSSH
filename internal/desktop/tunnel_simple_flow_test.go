package desktop

import (
	"errors"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/egoist/mygo/ui"
)

func TestSimpleWebsiteOpensAllocatedAddressOnlyAfterTrustAndSupportsRetry(t *testing.T) {
	h := firstTunnelFixture(t)
	web := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { io.WriteString(w, "remote website reached") }))
	defer web.Close()
	h.click("启动后打开网页")
	_, port, _ := net.SplitHostPort(strings.TrimPrefix(web.URL, "http://"))
	h.input("服务端口", port)
	h.ui.FailOpenURL(errors.New("test browser unavailable"))
	h.click("打开网页")
	h.wait(func() bool { return h.a.prompt.open })
	if len(h.ui.OpenedURLs()) != 0 || h.fixture.Auths.Load() != 0 {
		t.Fatal("website opened before trust")
	}
	h.click("信任指纹并连接")
	h.wait(func() bool { return h.a.tunnels.active[0].ready || h.a.tunnels.active[0].failed })
	run := h.a.tunnels.active[0]
	if run.failed || run.address == "127.0.0.1:0" || len(h.ui.OpenedURLs()) != 1 || run.urlError == "" {
		t.Fatal("website entry or recoverable browser error missing", run)
	}
	address := h.ui.OpenedURLs()[0]
	if address != "http://"+run.address {
		t.Fatal("browser bypassed loopback forward", address)
	}
	response, err := http.Get(address)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	body, err := io.ReadAll(response.Body)
	if err != nil || string(body) != "remote website reached" {
		t.Fatal("website did not use SSH forward", err, string(body))
	}
	auths := h.fixture.Auths.Load()
	h.ui.FailOpenURL(nil)
	h.click("打开网页")
	if len(h.ui.OpenedURLs()) != 2 || len(h.a.tunnels.active) != 1 || run.urlError != "" || h.fixture.Auths.Load() != auths {
		t.Fatal("browser retry started another SSH service")
	}
}

func TestSimpleWebsiteClosingPanelPreventsDeferredBrowserLaunch(t *testing.T) {
	h := firstTunnelFixture(t)
	h.click("启动后打开网页")
	h.click("打开网页")
	h.wait(func() bool { return h.a.prompt.open })
	h.click("信任指纹并连接")
	h.a.closeTunnelPanel()
	h.wait(func() bool { return h.a.tunnels.active[0].ready || h.a.tunnels.active[0].failed })
	if h.a.tunnels.active[0].failed {
		t.Fatal(h.a.tunnels.active[0].status)
	}
	h.a.tunnels.open = true
	h.ui.Frame()
	if len(h.ui.OpenedURLs()) != 0 {
		t.Fatal("reopening service panel unexpectedly launched browser")
	}
}

func TestSimpleSharingAllocatesRemotePortAndTransfersBytes(t *testing.T) {
	h := firstTunnelFixture(t)
	h.click("让服务器访问本机服务")
	target := tunnelEchoTarget(t)
	_, port, _ := net.SplitHostPort(target)
	h.input("本机服务端口", port)
	h.click("启动远程转发")
	h.wait(func() bool { return h.a.prompt.open })
	h.click("信任指纹并连接")
	h.wait(func() bool { return h.a.tunnels.active[0].ready || h.a.tunnels.active[0].failed })
	run := h.a.tunnels.active[0]
	if run.failed || run.spec.Kind != "remote" || run.address == "127.0.0.1:0" || run.spec.Listen != "127.0.0.1:0" {
		t.Fatal("sharing required manual entry allocation", run.status)
	}
	assertTunnelEcho(t, run.address)
	if !h.ui.HasText("服务器上的访问地址") || len(h.ui.OpenedURLs()) != 0 {
		t.Fatal("sharing endpoint context missing or opened browser")
	}
}

func TestServiceManagerAlwaysAvailableWithNoConnections(t *testing.T) {
	a := fileFollowDemo()
	a.connections = nil
	a.sessions = nil
	a.selected = ""
	tt := ui.NewTester(a.view, 560, 800)
	if err := tt.Click("端口转发"); err != nil {
		t.Fatal(err)
	}
	if err := tt.Click("新建服务…"); err != nil {
		t.Fatal(err)
	}
	if !a.tunnels.open || !tt.HasText("先添加一台服务器") {
		t.Fatal("empty manager cannot create a service")
	}
}
