package broker_test

import (
	"context"
	"errors"
	"io"
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"sshclient/internal/approval"
	"sshclient/internal/broker"
	"sshclient/internal/client"
	"sshclient/internal/connectcfg"
	"sshclient/internal/testserver"
	"sshclient/internal/vault"
)

func networkBroker(t *testing.T, profiles ...vault.Profile) (client.Client, *broker.Server) {
	t.Helper()
	dir, e := os.MkdirTemp("", "network-broker-")
	if e != nil {
		t.Fatal(e)
	}
	t.Cleanup(func() { os.RemoveAll(dir) })
	owner, e := vault.New(pass)
	if e != nil {
		t.Fatal(e)
	}
	for _, p := range profiles {
		if e = owner.Put(p); e != nil {
			owner.Close()
			t.Fatal(e)
		}
	}
	if e = vault.Save(broker.VaultPath(dir), owner, true); e != nil {
		owner.Close()
		t.Fatal(e)
	}
	server, e := broker.OpenSession(dir, owner)
	if e != nil {
		owner.Close()
		t.Fatal(e)
	}
	t.Cleanup(server.Close)
	return client.Client{Dir: dir, Owner: server.Vault}, server
}
func networkEcho(t *testing.T) string {
	t.Helper()
	listener, e := net.Listen("tcp", "127.0.0.1:0")
	if e != nil {
		t.Fatal(e)
	}
	t.Cleanup(func() { listener.Close() })
	go func() {
		for {
			c, e := listener.Accept()
			if e != nil {
				return
			}
			go func() { defer c.Close(); c.SetDeadline(time.Now().Add(3 * time.Second)); io.Copy(c, c) }()
		}
	}()
	return listener.Addr().String()
}
func TestBrokerTunnelRevocationClosesListenerAndEstablishedConnections(t *testing.T) {
	fixture := testserver.NewWithForwarding(t)
	c, server := networkBroker(t, fixture.Profile())
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	spec := connectcfg.Tunnel{Kind: "local", Listen: "127.0.0.1:0", Target: networkEcho(t)}
	stream, e := c.StartWithOwner(ctx, approval.Operation{Host: "fixture", Kind: "tunnel", Tunnel: spec}, server.Vault)
	if e != nil {
		t.Fatal(e)
	}
	defer stream.Close()
	connection, e := net.DialTimeout("tcp", stream.ListenAddress, time.Second)
	if e != nil {
		t.Fatal(e)
	}
	defer connection.Close()
	connection.SetDeadline(time.Now().Add(3 * time.Second))
	io.WriteString(connection, "approved tunnel")
	data := make([]byte, len("approved tunnel"))
	if _, e = io.ReadFull(connection, data); e != nil || string(data) != "approved tunnel" {
		t.Fatal(string(data), e)
	}
	if e = c.DecideWithOwner(ctx, stream.RequestID, false, true, server.Vault); e != nil {
		t.Fatal(e)
	}
	if n, e := connection.Read(data); e == nil || n != 0 {
		t.Fatal("revoked tunnel retained a live connection", n, e)
	}
	if next, e := net.DialTimeout("tcp", stream.ListenAddress, 100*time.Millisecond); e == nil {
		next.Close()
		t.Fatal("revoked tunnel retained its listener")
	}
}
func TestSSHRelayChangeRevokesDependentAuthority(t *testing.T) {
	relay, target := testserver.NewWithForwarding(t), testserver.NewWithForwarding(t)
	first, last := relay.Profile(), target.Profile()
	first.ID = "relay"
	last.ID = "destination"
	last.Network.Relay = first.ID
	c, server := networkBroker(t, first, last)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	spec := connectcfg.Tunnel{Kind: "local", Listen: "127.0.0.1:0", Target: networkEcho(t)}
	stream, e := c.StartWithOwner(ctx, approval.Operation{Host: last.ID, Kind: "tunnel", Tunnel: spec}, server.Vault)
	if e != nil {
		t.Fatal(e)
	}
	defer stream.Close()
	first.Name = "changed relay configuration"
	if e = c.SaveConnection(ctx, vault.ProfileChange{Profile: first}, server.Vault); e != nil {
		t.Fatal(e)
	}
	r, e := server.Approvals.Get(stream.RequestID)
	if e != nil && !errors.Is(e, approval.ErrDenied) || e == nil && r.Status != "revoked" && r.Status != "finished" {
		t.Fatal("relay update left dependent authority valid", r.Status, e)
	}
	done := make(chan error, 1)
	go func() { _, e := stream.Receive(); done <- e }()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("relay update did not interrupt its dependent tunnel")
	}
}
func TestProtectedAutomationRejectsTunnelBeforeSSHAuthentication(t *testing.T) {
	fixture := testserver.NewWithForwarding(t)
	p := fixture.Profile()
	p.Policy.Unrestricted = false
	c, _ := networkBroker(t, p)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_, _, e := c.Request(ctx, approval.Operation{Host: p.ID, Client: "synthetic external caller", Kind: "tunnel", Tunnel: connectcfg.Tunnel{Kind: "socks5", Listen: "127.0.0.1:0"}}, time.Minute)
	if e == nil {
		t.Fatal("protected automation allowed an arbitrary network tunnel")
	}
	if fixture.Auths.Load() != 0 {
		t.Fatal("rejected tunnel reached SSH authentication")
	}
}

func TestRouteMeasurementRPCRequiresOwnerAndAuditsRelayAuthentication(t *testing.T) {
	relay, target := testserver.NewWithForwarding(t), testserver.New(t)
	first, last := relay.Profile(), target.Profile()
	first.ID = "relay"
	last.ID = "target"
	last.Network.Relay = first.ID
	c, server := networkBroker(t, first, last)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	query := broker.Message{Action: "measureRoutes", ID: last.ID, Target: last.Host, Port: last.Port, Network: &last.Network, ExpectedFingerprint: last.Fingerprint}
	untrusted := c
	untrusted.Owner = nil
	if _, e := untrusted.Call(ctx, query); e == nil {
		t.Fatal("unverified caller could measure and authenticate through a saved relay")
	}
	if relay.Auths.Load() != 0 {
		t.Fatal("unverified measurement reached relay authentication")
	}
	result, e := c.CallWithOwner(ctx, query, server.Vault)
	if e != nil || len(result.RouteMeasurements) != 2 {
		t.Fatal(result, e)
	}
	if target.Auths.Load() != 0 {
		t.Fatal("measurement authenticated to the destination")
	}
	log, e := os.ReadFile(filepath.Join(c.Dir, "audit.jsonl"))
	if e != nil {
		t.Fatal(e)
	}
	if !strings.Contains(string(log), "route-probe") || !strings.Contains(string(log), `"event":"ended"`) {
		t.Fatal("authenticated relay probe omitted its audit completion")
	}
	if strings.Contains(string(log), first.Password) || strings.Contains(string(log), last.Password) {
		t.Fatal("route audit leaked a credential")
	}
	challenge, e := untrusted.Call(ctx, broker.Message{Action: "profileChallenge"})
	if e != nil {
		t.Fatal(e)
	}
	query.Challenge = challenge.ID
	query.Proof, e = broker.OwnerQueryProof(challenge.ID, query, server.Vault.Proof)
	if e != nil {
		t.Fatal(e)
	}
	query.ExpectedFingerprint = "SHA256:tampered"
	if _, e = untrusted.Call(ctx, query); e == nil {
		t.Fatal("changed measurement fingerprint bypassed the signed owner query")
	}
}
