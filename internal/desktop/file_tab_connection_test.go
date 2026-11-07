//go:build terminal_integration

package desktop

import (
	"sshclient/internal/vault"
	"testing"
)

func TestFileTabExplicitConnectionRevealsTerminalWithoutLateFocusTheft(t *testing.T) {
	a := fileFollowDemo()
	a.filesTabOpen, a.filesTabSelected = true, true
	a.selected = a.sessions[0].host
	a.openShellMode(false)
	if a.filesTabSelected || !a.filesTabOpen || a.currentTerminal() != a.sessions[0] {
		t.Fatal("explicit existing connection did not leave file view")
	}
	h := newConnectionFlowHarness(t)
	p := h.fixture.Profile()
	if err := h.a.client.SaveConnection(h.ctx, vault.ProfileChange{Profile: p}, h.server.Vault); err != nil {
		t.Fatal(err)
	}
	h.a.page = 1
	h.a.filesTabOpen, h.a.filesTabSelected = true, true
	h.a.openShellForHostAfter(p.ID, true, nil)
	pending := h.a.currentTerminal()
	if h.a.filesTabSelected || !h.a.filesTabOpen || pending == nil || pending.connectionState != "connecting" {
		t.Fatal("new connection did not immediately show pending terminal")
	}
	h.a.filesTabSelected = true
	h.wait(func() bool { return pending.term != nil || pending.connectionState == "error" })
	if pending.term == nil || !h.a.filesTabSelected {
		t.Fatal("late connection failed or stole the file tab", pending.connectionError)
	}
}
