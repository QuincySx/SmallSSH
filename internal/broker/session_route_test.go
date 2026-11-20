package broker_test

import (
	"context"
	"io"
	"sshclient/internal/approval"
	"sshclient/internal/client"
	"sshclient/internal/connectcfg"
	"sshclient/internal/protocol"
	"sshclient/internal/testserver"
	"sshclient/internal/vault"
	"testing"
	"time"
)

func TestOwnerSessionRouteDoesNotChangeDefaultAndNeverFallsBack(t *testing.T) {
	target, jump := testserver.New(t), testserver.NewWithForwarding(t)
	p, hop := target.Profile(), jump.Profile()
	hop.ID = "jump"
	p.Network.Proxy = connectcfg.Proxy{Mode: "http", Address: "127.0.0.1:1"}
	cli, server := networkBroker(t, p, hop)
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	route := vault.SavedRoute{ID: "once", Name: "One-off jump", Relay: hop.ID}
	if err := cli.SaveRoute(ctx, route, false, false, server.Vault); err != nil {
		t.Fatal(err)
	}
	op := approval.Operation{Host: p.ID, Kind: "list", Path: "/"}
	run := func(ctx context.Context, succeeds bool) {
		t.Helper()
		stream, err := cli.StartWithOwner(ctx, op, server.Vault)
		if succeeds {
			if err != nil {
				t.Fatal(err)
			}
			defer stream.Close()
			if _, _, err := stream.Copy(io.Discard, io.Discard); err != nil {
				t.Fatal(err)
			}
		} else if err == nil {
			stream.Close()
			t.Fatal("unavailable route fell back to direct or another pooled connection")
		}
	}
	direct := client.WithRoute(ctx, p.ID, connectcfg.RouteSelection{Mode: "direct"})
	saved := client.WithRoute(ctx, p.ID, connectcfg.RouteSelection{Mode: "saved", ID: route.ID})
	run(ctx, false)
	run(direct, true)
	run(saved, true)
	if jump.Auths.Load() == 0 {
		t.Fatal("saved route skipped the jump host")
	}
	stored, err := server.Vault.Profile(p.ID)
	if err != nil || stored.Network != p.Network {
		t.Fatal("temporary route overwrote default", err)
	}
	run(client.WithRoute(direct, p.ID, connectcfg.RouteSelection{}), false)
	run(client.WithRoute(ctx, "different-host", connectcfg.RouteSelection{Mode: "direct"}), false)
	run(client.WithRoute(ctx, p.ID, connectcfg.RouteSelection{Mode: "saved", ID: "missing"}), false)
	route.Relay, route.Proxy = "", connectcfg.Proxy{Mode: "http", Address: "127.0.0.1:1"}
	if err := cli.SaveRoute(ctx, route, false, false, server.Vault); err != nil {
		t.Fatal(err)
	}
	run(saved, false)
	run(direct, true)
	route.Relay, route.Proxy = p.ID, connectcfg.Proxy{}
	if err := cli.SaveRoute(ctx, route, false, false, server.Vault); err != nil {
		t.Fatal(err)
	}
	run(saved, false)
}

func TestOwnerRouteIntentCannotBeSubstitutedOrRequestedAnonymously(t *testing.T) {
	fixture := testserver.New(t)
	cli, server := networkBroker(t, fixture.Profile())
	ctx := context.Background()
	op := approval.Operation{Host: fixture.Profile().ID, Kind: "shell", Client: "test", Route: connectcfg.RouteSelection{Mode: "direct"}}
	challenge, err := cli.Call(ctx, protocol.Message{Action: "profileChallenge"})
	if err != nil {
		t.Fatal(err)
	}
	proof, err := protocol.OwnerRequestProof(challenge.ID, op, server.Vault.Proof)
	if err != nil {
		t.Fatal(err)
	}
	op.Route = connectcfg.RouteSelection{Mode: "saved", ID: "substituted"}
	if _, err := cli.Call(ctx, protocol.Message{Action: "ownerRequest", ID: challenge.ID, Operation: op, Proof: proof}); err == nil {
		t.Fatal("accepted unsigned route substitution")
	}
	anonymous := client.Client{Dir: cli.Dir}
	if _, _, err := anonymous.Request(ctx, op, time.Minute); err == nil {
		t.Fatal("ordinary request selected route")
	}
}
