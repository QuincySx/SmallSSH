package broker_test

import (
	"bytes"
	"context"
	"encoding/json"
	"testing"
	"time"

	"sshclient/internal/approval"
	"sshclient/internal/broker"
	"sshclient/internal/client"
	"sshclient/internal/connectcfg"
	"sshclient/internal/protocol"
	"sshclient/internal/testserver"
	"sshclient/internal/vault"
)

func TestSavedRouteRPCRequiresOwnerRedactsAndSignsCredentials(t *testing.T) {
	cli, server, _ := newWorkspaceHarness(t)
	ctx := context.Background()
	r := vault.SavedRoute{ID: "shared", Name: "Shared proxy", Proxy: connectcfg.Proxy{Mode: "socks5", Address: "127.0.0.1:1080", User: "fixture", Password: "synthetic route password"}}
	q := signedWorkspaceWrite(t, cli, &protocol.WorkspaceChange{Kind: "route-save", Route: &r})
	r.Proxy.Password = "changed after signing"
	if _, err := cli.Call(ctx, q); err == nil {
		t.Fatal("accepted unsigned password mutation")
	}
	r.Proxy.Password = "synthetic route password"
	if err := cli.SaveRoute(ctx, r, false, false, server.Vault); err != nil {
		t.Fatal(err)
	}
	public, err := cli.ListRoutes(ctx, server.Vault)
	if err != nil || len(public) != 1 || !public[0].HasPassword || public[0].Proxy.Password != "" {
		t.Fatal("route metadata", public, err)
	}
	encoded, _ := json.Marshal(public)
	if bytes.Contains(encoded, []byte(r.Proxy.Password)) {
		t.Fatal("route password returned")
	}
	anonymous := client.Client{Dir: cli.Dir}
	if _, err = anonymous.Call(ctx, broker.Message{Action: "workspaceRead", Workspace: &protocol.WorkspaceQuery{Kind: "route-list"}}); err == nil {
		t.Fatal("anonymous route list")
	}
	if _, err = anonymous.Call(ctx, broker.Message{Action: "workspaceWrite", WorkspaceChange: &protocol.WorkspaceChange{Kind: "route-delete", Route: &r}}); err == nil {
		t.Fatal("anonymous route delete")
	}
	hosts, err := anonymous.Call(ctx, broker.Message{Action: "hosts"})
	if err != nil || len(hosts.Routes) != 0 {
		t.Fatal("public host list disclosed routes", err)
	}
	r.Proxy.Password = ""
	q = signedWorkspaceWrite(t, cli, &protocol.WorkspaceChange{Kind: "route-save", Route: &r, KeepRoutePassword: true})
	q.WorkspaceChange.KeepRoutePassword = false
	if _, err = cli.Call(ctx, q); err == nil {
		t.Fatal("accepted unsigned password preservation mutation")
	}
}

func TestSavedRouteEditsAffectNextConnectionAndKeepExistingStream(t *testing.T) {
	target := testserver.New(t)
	relay := testserver.NewWithForwarding(t)
	p, hop := target.Profile(), relay.Profile()
	hop.ID = "relay"
	cli, server := networkBroker(t, p, hop)
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	r := vault.SavedRoute{ID: "shared", Name: "Shared SSH relay", Relay: hop.ID}
	if err := cli.SaveRoute(ctx, r, false, false, server.Vault); err != nil {
		t.Fatal(err)
	}
	p.Network.RouteID = r.ID
	if err := cli.SaveConnection(ctx, vault.ProfileChange{Profile: p}, server.Vault); err != nil {
		t.Fatal(err)
	}
	probe, err := cli.CallWithOwner(ctx, broker.Message{Action: "probeHost", ID: p.ID, Target: p.Host, Port: p.Port}, server.Vault)
	if err != nil || probe.HostIdentity == nil || probe.HostIdentity.Fingerprint != p.Fingerprint {
		t.Fatal("saved route probe", err)
	}
	stream, err := cli.StartWithOwner(ctx, approval.Operation{Host: p.ID, Kind: "shell"}, server.Vault)
	if err != nil {
		t.Fatal(err)
	}
	defer stream.Close()
	// An unreachable proxy proves the subsequent connection uses the edited
	// route and does not fall back to the target's working direct address.
	r.Relay = ""
	r.Proxy = connectcfg.Proxy{Mode: "http", Address: "127.0.0.1:1"}
	if err = cli.SaveRoute(ctx, r, false, false, server.Vault); err != nil {
		t.Fatal(err)
	}
	if _, err = cli.StartWithOwner(ctx, approval.Operation{Host: p.ID, Kind: "shell"}, server.Vault); err == nil {
		t.Fatal("new connection ignored edited route")
	}
	if _, err = stream.Write([]byte("route-still-live\n")); err != nil {
		t.Fatal("existing stream closed", err)
	}
	var got []byte
	for len(got) < len("route-still-live\n") {
		message, err := stream.Receive()
		if err != nil {
			t.Fatal("existing stream closed after route edit", err)
		}
		got = append(got, message.Data...)
	}
	if string(got) != "route-still-live\n" {
		t.Fatal("existing stream did not survive route edit", string(got))
	}

	if relay.Auths.Load() == 0 {
		t.Fatal("saved relay was never used")
	}
}
