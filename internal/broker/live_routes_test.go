package broker_test

import (
	"context"
	"io"
	"sshclient/internal/approval"
	"sshclient/internal/client"
	"sshclient/internal/connectcfg"
	"sshclient/internal/protocol"
	"sshclient/internal/testserver"
	"testing"
	"time"
)

func TestLiveRoutesRequireRunningOwnerSourceAndNeverFallBack(t *testing.T) {
	for _, mode := range []string{"session", "tunnel"} {
		t.Run(mode, func(t *testing.T) {
			target, hop := testserver.New(t), testserver.NewWithForwarding(t)
			p, relay := target.Profile(), hop.Profile()
			relay.ID = "live-hop"
			p.Network.Proxy = connectcfg.Proxy{Mode: "http", Address: "127.0.0.1:1"}
			relay.Network.Proxy = p.Network.Proxy
			cli, server := networkBroker(t, p, relay)
			ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
			defer cancel()
			sourceOp := approval.Operation{Host: relay.ID, Kind: "shell"}
			if mode == "tunnel" {
				sourceOp.Kind, sourceOp.Tunnel = "tunnel", connectcfg.Tunnel{Kind: "socks5", Listen: "127.0.0.1:0"}
			}
			source, err := cli.StartWithOwner(client.WithRoute(ctx, relay.ID, connectcfg.RouteSelection{Mode: "direct"}), sourceOp, server.Vault)
			if err != nil {
				t.Fatal(err)
			}
			defer source.Close()
			selection := connectcfg.RouteSelection{Mode: mode, ID: source.RequestID}
			run := func(route connectcfg.RouteSelection, succeeds bool) {
				t.Helper()
				stream, err := cli.StartWithOwner(client.WithRoute(ctx, p.ID, route), approval.Operation{Host: p.ID, Kind: "list", Path: "/"}, server.Vault)
				if !succeeds {
					if err == nil {
						stream.Close()
						t.Fatal("unavailable or wrong-kind live source fell back")
					}
					return
				}
				if err != nil {
					t.Fatal(err)
				}
				defer stream.Close()
				if _, _, err := stream.Copy(io.Discard, io.Discard); err != nil {
					t.Fatal(err)
				}
			}
			run(connectcfg.RouteSelection{Mode: "direct"}, true)
			beforeHop, beforeTarget := hop.Auths.Load(), target.Auths.Load()
			run(selection, true)
			if target.Auths.Load() <= beforeTarget || mode == "session" && hop.Auths.Load() <= beforeHop {
				t.Fatal("live route reused direct target transport")
			}
			query := protocol.Message{Action: "probeHost", ID: p.ID, Target: p.Host, Port: p.Port, ConnectionRoute: &selection}
			response, err := cli.CallWithOwner(ctx, query, server.Vault)
			if err != nil || response.HostIdentity == nil || response.HostIdentity.Fingerprint != p.Fingerprint {
				t.Fatal("live route probe failed", err)
			}
			wrong := selection
			if mode == "session" {
				wrong.Mode = "tunnel"
			} else {
				wrong.Mode = "session"
			}
			run(wrong, false)
			run(connectcfg.RouteSelection{Mode: mode, ID: "missing"}, false)
			anonymous := client.Client{Dir: cli.Dir}
			if _, _, err := anonymous.Request(ctx, approval.Operation{Host: p.ID, Kind: "list", Path: "/", Route: selection}, time.Minute); err == nil {
				t.Fatal("ordinary caller selected live owner route")
			}
			// Closing a source removes future routing authority even when the target is
			// reachable and a connection from the previous run is sitting in the pool.
			source.Close()
			deadline := time.Now().Add(2 * time.Second)
			for {
				r, err := server.Approvals.Get(source.RequestID)
				if err != nil || r.Status != "running" {
					break
				}
				if time.Now().After(deadline) {
					t.Fatal("closed source stayed running")
				}
				time.Sleep(5 * time.Millisecond)
			}
			before := target.Auths.Load()
			run(selection, false)
			if _, err := cli.CallWithOwner(ctx, query, server.Vault); err == nil {
				t.Fatal("closed source still allowed probing")
			}
			if target.Auths.Load() != before {
				t.Fatal("closed route authenticated directly")
			}
			saved, err := server.Vault.Profile(p.ID)
			if err != nil || saved.Network != p.Network {
				t.Fatal("runtime routing changed default", err)
			}
		})
	}
}
