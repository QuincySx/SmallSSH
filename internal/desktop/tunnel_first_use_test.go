package desktop

import (
	"encoding/binary"
	"io"
	"net"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"
	"sshclient/internal/broker"
	"sshclient/internal/connectcfg"
	"sshclient/internal/testserver"
	"sshclient/internal/vault"
)

func firstTunnelFixture(t *testing.T) *connectionFlowHarness {
	h := newConnectionFlowHarness(t)
	h.fixture = testserver.NewWithForwarding(t)
	p := h.fixture.Profile()
	p.Fingerprint = ""
	if err := h.server.Vault.SaveProfile(broker.VaultPath(h.dir), p); err != nil {
		t.Fatal(err)
	}
	h.a.connections = h.server.Vault.Summaries()
	h.a.page = 1
	h.a.openTunnels(p.ID)
	h.a.tunnels.listen = "127.0.0.1:0"
	h.ui = ui.NewTester(h.a.view, 1000, 800)
	t.Cleanup(h.a.closeTunnels)
	return h
}

func assertTunnelEcho(t *testing.T, address string) {
	t.Helper()
	c, err := net.DialTimeout("tcp", address, time.Second)
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	c.SetDeadline(time.Now().Add(3 * time.Second))
	if _, err = io.WriteString(c, "first-use tunnel"); err != nil {
		t.Fatal(err)
	}
	b := make([]byte, len("first-use tunnel"))
	if _, err = io.ReadFull(c, b); err != nil || string(b) != "first-use tunnel" {
		t.Fatal("tunnel did not transfer bytes", err)
	}
}
func tunnelEchoTarget(t *testing.T) string {
	t.Helper()
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { l.Close() })
	go func() {
		for {
			c, err := l.Accept()
			if err != nil {
				return
			}
			go func() { defer c.Close(); c.SetDeadline(time.Now().Add(3 * time.Second)); io.Copy(c, c) }()
		}
	}()
	return l.Addr().String()
}

func TestFirstTunnelAsksForIdentityWithoutOpeningATerminal(t *testing.T) {
	h := firstTunnelFixture(t)
	h.a.tunnels.target = tunnelEchoTarget(t)
	h.a.startTunnel()
	h.wait(func() bool { return h.a.prompt.open || h.a.tunnels.active[0].failed })
	if !h.a.prompt.open || h.fixture.Auths.Load() != 0 || len(h.a.sessions) != 0 {
		t.Fatal("first tunnel skipped trust or authenticated first", h.a.tunnels.active[0].status)
	}
	h.click("信任指纹并连接")
	h.wait(func() bool { return h.a.tunnels.active[0].ready || h.a.tunnels.active[0].failed })
	run := h.a.tunnels.active[0]
	if !run.ready || run.failed || len(h.a.sessions) != 0 {
		t.Fatal("first accepted tunnel required a terminal or failed", run.status)
	}
	assertTunnelEcho(t, run.address)
	h.a.tunnels.open = false
	h.ui.Frame()
	if err := h.ui.RightClick("端口转发"); err != nil {
		t.Fatal(err)
	}
	if err := h.ui.ChooseMenuItem("停止 " + h.a.tunnelServerName(run.host) + " · " + run.label() + " · " + run.address); err != nil {
		t.Fatal(err)
	}
	h.wait(func() bool { return !run.running })
	if c, err := net.DialTimeout("tcp", run.address, 100*time.Millisecond); err == nil {
		c.Close()
		t.Fatal("stopped tunnel retained listener")
	}
}
func TestFirstTunnelCancelAndEscapeNeverAuthenticate(t *testing.T) {
	for _, escape := range []bool{false, true} {
		t.Run(map[bool]string{false: "cancel", true: "escape"}[escape], func(t *testing.T) {
			h := firstTunnelFixture(t)
			h.a.startTunnel()
			h.wait(func() bool { return h.a.prompt.open })
			if escape {
				h.ui.Key(0, ui.KeyEscape)
			} else {
				h.click("取消")
			}
			h.wait(func() bool { return !h.a.tunnels.active[0].running })
			if h.fixture.Auths.Load() != 0 || h.stored(h.fixture.Profile().ID).Fingerprint != "" || h.a.tunnels.active[0].failed {
				t.Fatal("cancel trusted or authenticated")
			}
		})
	}
}
func TestFirstTunnelTrustsUnseenRelayThenDestinationOnSelectedRoute(t *testing.T) {
	h := firstTunnelFixture(t)
	relay := testserver.NewWithForwarding(t)
	hop := relay.Profile()
	hop.ID = "new-relay"
	hop.Fingerprint = ""
	if err := h.server.Vault.SaveProfile(broker.VaultPath(h.dir), hop); err != nil {
		t.Fatal(err)
	}
	route := vault.SavedRoute{ID: "hop-route", Name: "Relay", Relay: hop.ID}
	if err := h.server.Vault.SaveRoute(broker.VaultPath(h.dir), route, false, false); err != nil {
		t.Fatal(err)
	}
	// Saved default is broken, so both the probe and the first operation must use
	// the temporary route. There must never be a direct-dial retry.
	p, _ := h.server.Vault.Profile(h.fixture.Profile().ID)
	p.Network.Proxy = connectcfg.Proxy{Mode: "http", Address: "127.0.0.1:1"}
	if err := h.server.Vault.SaveProfile(broker.VaultPath(h.dir), p); err != nil {
		t.Fatal(err)
	}
	h.a.connections = h.server.Vault.Summaries()
	h.a.routes = h.server.Vault.Routes()
	h.a.tunnels.routeChoice = "saved:" + route.ID
	h.a.tunnels.target = tunnelEchoTarget(t)
	h.a.startTunnel()
	h.wait(func() bool { return h.a.prompt.open || h.a.tunnels.active[0].failed })
	if !strings.Contains(h.a.prompt.target, targetID(hop.Host, hop.Port)) || relay.Auths.Load() != 0 || h.fixture.Auths.Load() != 0 {
		t.Fatal("outer relay was not confirmed first", h.a.tunnels.active[0].status)
	}
	h.click("信任指纹并连接")
	h.wait(func() bool { return h.a.prompt.open || h.a.tunnels.active[0].failed })
	if !strings.Contains(h.a.prompt.target, targetID(p.Host, p.Port)) || h.fixture.Auths.Load() != 0 {
		t.Fatal("destination authenticated before its confirmation", h.a.tunnels.active[0].status)
	}
	h.click("信任指纹并连接")
	h.wait(func() bool { return h.a.tunnels.active[0].ready || h.a.tunnels.active[0].failed })
	run := h.a.tunnels.active[0]
	if !run.ready {
		t.Fatal("new relay / target needed direct login first", run.status)
	}
	assertTunnelEcho(t, run.address)
	if len(h.a.sessions) != 0 || h.stored(p.ID).Network != p.Network {
		t.Fatal("temporary tunnel changed profile or opened terminal")
	}
}

func TestFirstTunnelUsesExistingLocalSOCKSForProbeAndOperation(t *testing.T) {
	h := firstTunnelFixture(t)
	expected := h.fixture.Listener.Addr().String()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	var connections atomic.Int32
	go func() {
		for {
			in, err := listener.Accept()
			if err != nil {
				return
			}
			connections.Add(1)
			go func() {
				defer in.Close()
				in.SetDeadline(time.Now().Add(10 * time.Second))
				var greeting [2]byte
				if _, err := io.ReadFull(in, greeting[:]); err != nil || greeting[0] != 5 {
					return
				}
				methods := make([]byte, int(greeting[1]))
				if _, err := io.ReadFull(in, methods); err != nil {
					return
				}
				if _, err := in.Write([]byte{5, 0}); err != nil {
					return
				}
				var request [10]byte
				if _, err := io.ReadFull(in, request[:]); err != nil || request[3] != 1 {
					return
				}
				target := net.JoinHostPort(net.IP(request[4:8]).String(), strconv.Itoa(int(binary.BigEndian.Uint16(request[8:]))))
				if target != expected {
					return
				}
				out, err := net.DialTimeout("tcp", expected, time.Second)
				if err != nil {
					return
				}
				defer out.Close()
				if _, err = in.Write([]byte{5, 0, 0, 1, 0, 0, 0, 0, 0, 0}); err != nil {
					return
				}
				go func() { io.Copy(out, in); out.Close() }()
				io.Copy(in, out)
			}()
		}
	}()
	h.a.tunnels.routeChoice = "local-socks"
	h.a.tunnels.localProxy = listener.Addr().String()
	h.a.tunnels.target = tunnelEchoTarget(t)
	h.a.startTunnel()
	h.wait(func() bool { return h.a.prompt.open || h.a.tunnels.active[0].failed })
	if !h.a.prompt.open || h.fixture.Auths.Load() != 0 {
		t.Fatal("upstream probe skipped trust", h.a.tunnels.active[0].status)
	}
	h.click("信任指纹并连接")
	h.wait(func() bool { return h.a.tunnels.active[0].ready || h.a.tunnels.active[0].failed })
	run := h.a.tunnels.active[0]
	if !run.ready {
		t.Fatal("local SOCKS tunnel failed", run.status)
	}
	assertTunnelEcho(t, run.address)
	if connections.Load() < 2 || len(h.server.Vault.Routes()) != 1 {
		t.Fatal("probe or operation bypassed selected local SOCKS route", connections.Load())
	}
	if p := h.stored(h.fixture.Profile().ID); p.Network.RouteID != "" || p.Network.Proxy.Mode != "" {
		t.Fatal("one-time route changed the saved SSH profile")
	}
}
