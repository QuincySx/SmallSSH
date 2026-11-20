//go:build terminal_integration

package desktop

import (
	"os"
	"path/filepath"
	"sshclient/internal/broker"
	"sshclient/internal/connectcfg"
	"sshclient/internal/testserver"
	"testing"
)

func TestTemporaryRouteTerminalReconnectDerivedTabsAndFiles(t *testing.T) {
	h := newConnectionFlowHarness(t)
	p := h.fixture.Profile()
	p.Network.Proxy = connectcfg.Proxy{Mode: "http", Address: "127.0.0.1:1"}
	if err := h.server.Vault.SaveProfile(broker.VaultPath(h.dir), p); err != nil {
		t.Fatal(err)
	}
	a := h.a
	a.page, a.selected, a.connections = 1, p.ID, h.server.Vault.Summaries()
	route := connectcfg.RouteSelection{Mode: "direct"}
	a.openShellForHostAfter(p.ID, true, nil, route)
	h.wait(func() bool { return readyTerminalCount(a) == 1 })
	source := a.sessions[0]
	if source.target.Route != route {
		t.Fatal("connected session lost route")
	}
	a.newTerminalTab(source)
	h.wait(func() bool { return readyTerminalCount(a) == 2 })
	if a.sessions[1].target.Route != route {
		t.Fatal("derived tab lost route")
	}
	source.term.Close()
	h.wait(func() bool { return source.closed })
	a.reconnectTerminal(source)
	h.wait(func() bool { return !a.hasSession(source) && readyTerminalCount(a) == 2 })
	source = a.sessions[0]
	if source.target.Route != route {
		t.Fatal("reconnect reset route")
	}
	a.activateTerminal(source, true)
	a.openShell() // Ordinary connect must use the broken saved default.
	h.wait(func() bool { return len(a.sessions) == 3 && a.sessions[2].connectionState == "error" })
	if a.sessions[2].target.Route.Temporary() {
		t.Fatal("ordinary connection inherited temporary route")
	}
	a.activateTerminal(source, true)
	a.newCanvasTerminal(source, source.group, "right")
	h.wait(func() bool { return readyTerminalCount(a) == 3 })
	derived := a.sessions[len(a.sessions)-1]
	if derived.target.Route != route {
		t.Fatal("canvas split lost route")
	}
	a.ensurePanes()
	a.syncFollowingFilePanes()
	pane := a.primaryFilePane
	a.loadDirectory(pane, "/", true)
	h.wait(func() bool {
		_, loaded := pane.browser().cache["/"]
		return loaded || pane.browser().loadError["/"] != ""
	})
	if pane.route != route || pane.browser().loadError["/"] != "" {
		t.Fatal("files did not follow route", pane.browser().loadError)
	}
	name := filepath.Join(t.TempDir(), "route-source.txt")
	if err := os.WriteFile(name, []byte("source route"), 0600); err != nil {
		t.Fatal(err)
	}
	a.readEditor(p.ID, testserver.RemotePath(name), pane.route)
	// Read completion is tied to the editor, while another same-host tab is active.
	a.activateTerminal(a.sessions[2], true)
	h.wait(func() bool { return a.editorRead == nil || !a.editorRead.loading })
	if a.editorRead != nil || a.editRoute != route || editorBufferText(a.edit.Buffer) != "source route" {
		t.Fatal("editor read changed route", a.editorRead)
	}
}
