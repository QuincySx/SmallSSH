package desktop

import (
	"context"
	"errors"
	"net"
	"strconv"
	"sync/atomic"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"
	"sshclient/internal/connectcfg"
	"sshclient/internal/vault"
)

func TestSavingSSHNeverDialsResolvesOrActivatesTarget(t *testing.T) {
	h := newConnectionFlowHarness(t)
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	var packets, lookups atomic.Int32
	go func() {
		for {
			c, err := listener.Accept()
			if err != nil {
				return
			}
			packets.Add(1)
			c.Close()
		}
	}()
	old := net.DefaultResolver
	net.DefaultResolver = &net.Resolver{PreferGo: true, Dial: func(context.Context, string, string) (net.Conn, error) {
		lookups.Add(1)
		return nil, errors.New("unexpected DNS")
	}}
	defer func() { net.DefaultResolver = old }()
	for _, host := range []string{"127.0.0.1", "private-target.invalid"} {
		h.a.newConnection()
		p := h.fixture.Profile()
		p.ID = vault.ShortID()
		p.Host = host
		p.Port = listener.Addr().(*net.TCPAddr).Port
		p.Fingerprint = ""
		// Both a target and a proxy can be sensitive. Neither is contacted by Save.
		p.Network.Proxy = connectcfg.Proxy{Mode: "http", Address: listener.Addr().String()}
		h.a.form = p
		h.a.port = strconv.Itoa(p.Port)
		h.ui = ui.NewTester(h.a.view, 1100, 900)
		h.saveOnly()
		if got := h.stored(p.ID); got.Host != host || got.Fingerprint != "" {
			t.Fatal("offline save lost target or invented trust")
		}
		if h.a.selected != "" || len(h.a.connecting) > 0 || h.a.prompt.open {
			t.Fatal("save activated target")
		}
	}
	// Give any accidentally launched goroutine a chance to reach the canaries.
	time.Sleep(50 * time.Millisecond)
	if packets.Load() != 0 || lookups.Load() != 0 || h.fixture.Auths.Load() != 0 {
		t.Fatal("save leaked network access", packets.Load(), lookups.Load())
	}
}

func TestFirstIdentityProbeHonorsTemporaryRouteAndCancelDoesNotAuthenticate(t *testing.T) {
	h := newConnectionFlowHarness(t)
	p := h.fixture.Profile()
	p.Fingerprint = ""
	p.Network.Proxy = connectcfg.Proxy{Mode: "http", Address: "127.0.0.1:1"}
	if err := h.a.client.SaveConnection(h.ctx, vault.ProfileChange{Profile: p}, h.server.Vault); err != nil {
		t.Fatal(err)
	}
	h.a.connections = h.server.Vault.Summaries()
	h.a.page = 1
	h.ui = ui.NewTester(h.a.view, 1100, 900)
	// Default saved proxy is broken: an identity probe must fail, without falling back.
	h.a.openShellForHostAfter(p.ID, true, nil)
	h.wait(func() bool { return len(h.a.sessions) > 0 && h.a.sessions[0].connectionState == "error" })
	if h.a.prompt.open || h.fixture.Auths.Load() != 0 {
		t.Fatal("probe bypassed the saved route")
	}
	// Explicit one-time direct routing applies to the unauthenticated probe too.
	h.a.openShellForHostAfter(p.ID, true, nil, connectcfg.RouteSelection{Mode: "direct"})
	h.wait(func() bool { return h.a.prompt.open })
	h.click("取消")
	if h.fixture.Auths.Load() != 0 || h.stored(p.ID).Fingerprint != "" {
		t.Fatal("cancel authenticated or trusted the target")
	}
	if h.stored(p.ID).Network != p.Network {
		t.Fatal("temporary routing changed the saved default")
	}
}

func TestChangedIdentityRecheckPreservesRouteAndAllSavedOptions(t *testing.T) {
	h := newConnectionFlowHarness(t)
	p := h.fixture.Profile()
	p.Fingerprint = "SHA256:previous-server-identity"
	p.Network.Proxy = connectcfg.Proxy{Mode: "http", Address: "127.0.0.1:1"}
	p.Terminal = connectcfg.Terminal{Encoding: "gb18030", Backspace: "bs"}
	if err := h.a.client.SaveConnection(h.ctx, vault.ProfileChange{Profile: p}, h.server.Vault); err != nil {
		t.Fatal(err)
	}
	h.a.connections = h.server.Vault.Summaries()
	h.a.page = 1
	h.ui = ui.NewTester(h.a.view, 1100, 900)
	h.a.openShellForHostAfter(p.ID, true, nil, connectcfg.RouteSelection{Mode: "direct"})
	h.wait(func() bool { return len(h.a.connecting) == 0 })
	if !h.a.sessions[0].connectionMismatch || h.fixture.Auths.Load() != 0 {
		t.Fatal("mismatch was not blocked")
	}
	h.click("重新核对服务器身份")
	h.wait(func() bool { return h.a.prompt.open })
	if h.fixture.Auths.Load() != 0 {
		t.Fatal("recheck authenticated before confirmation")
	}
	h.click("保存新指纹并连接")
	h.wait(func() bool { return readyTerminalCount(h.a) > 0 })
	saved := h.stored(p.ID)
	if saved.Fingerprint != h.fixture.Profile().Fingerprint || saved.Network != p.Network || saved.Terminal != p.Terminal || saved.Password != p.Password {
		t.Fatal("trust update replaced unrelated configuration")
	}
	if h.a.sessions[0].target.Route.Mode != "direct" {
		t.Fatal("recheck reset the temporary route")
	}
}
