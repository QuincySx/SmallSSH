package desktop

import (
	"context"
	"github.com/egoist/mygo/ui"
	"sshclient/internal/approval"
	"sshclient/internal/broker"
	"sshclient/internal/client"
	"sshclient/internal/connectcfg"
	"sshclient/internal/testserver"
	"sshclient/internal/vault"
	"strings"
	"testing"
)

func TestTemporaryRouteMenuQueuesNewSessionWithoutChangingDefault(t *testing.T) {
	h := newConnectionFlowHarness(t)
	p := h.fixture.Profile()
	p.Network.Proxy = connectcfg.Proxy{Mode: "http", Address: "127.0.0.1:1"}
	if err := h.server.Vault.SaveProfile(broker.VaultPath(h.dir), p); err != nil {
		t.Fatal(err)
	}
	a := h.a
	a.page, a.connections = 1, h.server.Vault.Summaries()
	a.routes = []vault.RouteSummary{{ID: "route-a", Name: "Route A"}, {ID: "jump", Name: "Jump", Relay: "hop"}}
	a.connecting = map[string]context.CancelFunc{p.ID: func() {}}
	// Exercise the native menu contract without opening a window or PTY.
	tt := ui.NewTester(func(c *ui.Context) {
		ui.Button(c, "server").ContextMenu(func(m *ui.Menu) { a.connectionContextMenu(m, a.connections[0], "") })
	}, 400, 200)
	for _, choice := range []struct {
		label string
		route connectcfg.RouteSelection
	}{
		{"Route A", connectcfg.RouteSelection{Mode: "saved", ID: "route-a"}},
		{"本次直连", connectcfg.RouteSelection{Mode: "direct"}},
		{"Jump · SSH 跳板机", connectcfg.RouteSelection{Mode: "saved", ID: "jump"}},
	} {
		if err := tt.RightClick("server"); err != nil {
			t.Fatal(err)
		}
		if err := tt.ChooseMenuItem("终端连接", choice.label); err != nil {
			t.Fatal(err)
		}
		s := a.sessions[len(a.sessions)-1]
		if s.target.Route != choice.route || s.connectionState != "queued" {
			t.Fatal(s.target, s.connectionState)
		}
		if !strings.Contains(a.workspaceTabDisplayTitle(nil, workspaceTab{session: s}), "本次：") {
			t.Fatal("temporary route is invisible")
		}
	}
	stored, err := h.server.Vault.Profile(p.ID)
	if err != nil || stored.Network != p.Network || len(a.pendingShellOpens[p.ID]) != 3 {
		t.Fatal("menu changed default or reused another session", err)
	}
}

func TestSessionRouteFollowsFilesAndOwnerOperationsWithoutCacheMixing(t *testing.T) {
	a := fileFollowDemo()
	first, pane := a.sessions[0], a.primaryFilePane
	defaultBrowser := pane.browser()
	first.target.Route = connectcfg.RouteSelection{Mode: "saved", ID: "temporary"}
	a.syncFollowingFilePanes()
	if pane.route != first.target.Route || pane.browser() == defaultBrowser {
		t.Fatal("same-host route reused default browser")
	}
	temporaryBrowser := pane.browser()
	copy := pane.snapshot()
	if copy.browser() != temporaryBrowser || copy.route != first.target.Route {
		t.Fatal("dialog snapshot lost route")
	}
	ctx := a.operationContext()
	a.activateTerminal(a.sessions[1], false)
	if route, ok := client.RouteFromContext(ctx, first.host); !ok || route != first.target.Route {
		t.Fatal("focus change redirected captured operation", route)
	}
	a.activateTerminal(first, false)
	a.chooseFileEndpoint(pane, first.host) // Explicit server choice uses its saved default.
	if pane.route.Temporary() || pane.browser() != defaultBrowser || copy.browser() != temporaryBrowser {
		t.Fatal("fixed pane changed route or snapshot")
	}
	a.chooseFileEndpoint(pane, followTerminalEndpoint)
	if pane.browser() != temporaryBrowser {
		t.Fatal("following did not restore its route cache")
	}
}

func TestLiveConnectionMenuUsesSourceRouteAndConfirmsFirstIdentity(t *testing.T) {
	for _, mode := range []string{"session", "tunnel", "relay"} {
		t.Run(mode, func(t *testing.T) {
			h := newConnectionFlowHarness(t)
			hopFixture := testserver.NewWithForwarding(t)
			hop := hopFixture.Profile()
			hop.ID, hop.Name = "intermediary", "中继"
			// Source was explicitly connected directly; its saved default cannot work.
			hop.Network.Proxy = connectcfg.Proxy{Mode: "http", Address: "127.0.0.1:1"}
			target := h.fixture.Profile()
			target.Fingerprint = ""
			target.Network.Proxy = hop.Network.Proxy
			if mode == "relay" {
				hop.Network.Proxy = connectcfg.Proxy{}
				hop.Fingerprint = ""
			}
			for _, p := range []vault.Profile{hop, target} {
				if err := h.server.Vault.SaveProfile(broker.VaultPath(h.dir), p); err != nil {
					t.Fatal(err)
				}
			}
			a := h.a
			a.connections, a.page = h.server.Vault.Summaries(), 1
			h.ui = ui.NewTester(a.view, 1100, 900)
			direct := connectcfg.RouteSelection{Mode: "direct"}
			if mode == "session" {
				a.openShellForHostAfter(hop.ID, true, nil, direct)
				h.wait(func() bool { return readyTerminalCount(a) == 1 })
			} else if mode == "tunnel" {
				a.openTunnels(hop.ID)
				a.tunnels.kind, a.tunnels.listen, a.tunnels.routeChoice = "socks5", "127.0.0.1:0", "direct"
				a.startTunnel()
				h.wait(func() bool { return a.tunnels.active[0].ready || a.tunnels.active[0].failed })
				if a.tunnels.active[0].failed {
					t.Fatal(a.tunnels.active[0].status)
				}
				a.tunnels.open = false
				t.Cleanup(a.closeTunnels)
			}
			path := []string{"终端连接"}
			selected := connectcfg.RouteSelection{Mode: "relay", ID: hop.ID}
			if mode == "relay" {
				path = append(path, "新建 SSH 中转", hop.Name)
				if len(a.liveConnectionRoutes(target.ID)) != 0 {
					t.Fatal("saved SSH hop should start without an existing terminal")
				}
			} else {
				choices := a.liveConnectionRoutes(target.ID)
				if len(choices) != 1 || choices[0].route.Mode != mode {
					t.Fatal(choices)
				}
				path = append(path, choices[0].label)
				selected = choices[0].route
			}
			menu := ui.NewTester(func(c *ui.Context) {
				ui.Button(c, "server").ContextMenu(func(m *ui.Menu) { a.connectionContextMenu(m, vault.Summary{ID: target.ID}, "") })
			}, 400, 200)
			if err := menu.RightClick("server"); err != nil {
				t.Fatal(err)
			}
			if err := menu.ChooseMenuItem(path...); err != nil {
				t.Fatal(err)
			}
			h.wait(func() bool { return a.prompt.open })
			if mode == "relay" {
				if !strings.Contains(a.prompt.target, targetID(hop.Host, hop.Port)) || hopFixture.Auths.Load() != 0 {
					t.Fatal("new relay must confirm the outer SSH identity first", a.prompt.target)
				}
				h.click("信任指纹并连接")
				h.wait(func() bool { return a.prompt.open })
			}
			if h.fixture.Auths.Load() != 0 {
				t.Fatal("destination authenticated before first trust")
			}
			h.click("信任指纹并连接")
			h.wait(func() bool { return len(a.connecting) == 0 })
			destination := a.sessions[len(a.sessions)-1]
			if destination.host != target.ID || destination.connectionState != "" || destination.requestID == "" || destination.target.Route != selected {
				t.Fatal("one-click route failed", destination.connectionState, destination.connectionError, a.status)
			}
			if h.fixture.Auths.Load() == 0 || hopFixture.Auths.Load() == 0 || h.stored(target.ID).Network != target.Network || h.stored(hop.ID).Network != hop.Network {
				t.Fatal("route bypassed source or changed saved defaults")
			}
			if mode == "relay" && (len(a.sessions) != 1 || len(h.server.Vault.Routes()) != 0) {
				t.Fatal("one-time relay created an extra terminal or a persistent route")
			}
		})
	}
}

func TestLiveConnectionMenuOmitsStoppedSourcesAndKeepsDistinctRoutes(t *testing.T) {
	a := &app{connections: []vault.Summary{{ID: "hop", Name: "Hop"}, {ID: "target", Name: "Target"}}}
	source := func(id, host string, route connectcfg.RouteSelection) *session {
		return &session{requestID: id, host: host, target: approval.Operation{Kind: "shell", Host: host, Route: route}}
	}
	a.sessions = []*session{source("one", "hop", connectcfg.RouteSelection{}), source("duplicate", "hop", connectcfg.RouteSelection{}), source("direct", "hop", connectcfg.RouteSelection{Mode: "direct"}), source("self", "target", connectcfg.RouteSelection{}), source("ended", "hop", connectcfg.RouteSelection{})}
	a.sessions[4].closed = true
	a.tunnels.active = []*tunnelRun{{kind: "socks5", requestID: "running", host: "hop", running: true, ready: true, address: "127.0.0.1:1080"}, {kind: "socks5", requestID: "stopped", host: "hop", ready: true}, {kind: "local", requestID: "wrong", host: "hop", ready: true, running: true}}
	choices := a.liveConnectionRoutes("target")
	if len(choices) != 3 || choices[0].route.ID != "one" || choices[1].route.ID != "direct" || choices[2].route.ID != "running" {
		t.Fatal(choices)
	}
	a.sessions[0].closed, a.sessions[1].closed, a.sessions[2].closed = true, true, true
	a.tunnels.active[0].running = false
	if choices := a.liveConnectionRoutes("target"); len(choices) != 0 {
		t.Fatal("stopped sources remained", choices)
	}
}
