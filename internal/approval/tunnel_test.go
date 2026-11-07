package approval

import (
	"testing"
	"time"

	"sshclient/internal/connectcfg"
)

func TestTunnelScopeCannotBeHiddenInOtherOperationsOrReadGrants(t *testing.T) {
	tunnel := connectcfg.Tunnel{Kind: "local", Listen: "127.0.0.1:0", Target: "127.0.0.1:80"}
	m, r, _, _ := grantFixture(t, "read")
	op := Operation{Host: r.Operation.Host, Endpoint: r.Operation.Endpoint, HostFingerprint: r.Operation.HostFingerprint, Client: "fixture", Kind: "tunnel", Tunnel: tunnel}
	if _, _, e := m.Create(op, time.Minute); e != nil {
		t.Fatal(e)
	}
	parent := r
	parent.Status = "approved"
	if grantAllows(parent, op) {
		t.Fatal("read-only grant acquired TCP forwarding")
	}
	parent.Operation.Grant.Permission = "full"
	parent.Operation.Grant.ReadRoot = "/srv"
	parent.Operation.Grant.WriteRoot = "/srv"
	if grantAllows(parent, op) {
		t.Fatal("directory-scoped grant acquired TCP forwarding")
	}
	parent.Operation.Grant.ReadRoot = ""
	parent.Operation.Grant.WriteRoot = ""
	if !grantAllows(parent, op) {
		t.Fatal("broad full grant could not request a tunnel")
	}
	op.Kind = "monitor"
	if _, _, e := m.Create(op, time.Minute); e == nil {
		t.Fatal("tunnel metadata hidden in monitor approval")
	}
	for _, listen := range []string{"0.0.0.0:1080", "[::]:1080", "localhost:1080"} {
		op.Kind = "tunnel"
		op.Tunnel.Listen = listen
		if _, _, e := m.Create(op, time.Minute); e == nil {
			t.Fatal("nonliteral or public listener accepted", listen)
		}
	}
}
