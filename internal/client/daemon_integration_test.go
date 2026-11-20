//go:build service_integration && with_utls && with_quic

package client

import (
	"context"
	"io"
	"os"
	"path/filepath"
	"testing"
	"time"

	"sshclient/internal/approval"
	"sshclient/internal/connectcfg"
	"sshclient/internal/protocol"
	"sshclient/internal/testproxy"
	"sshclient/internal/testserver"
	"sshclient/internal/vault"
)

// Opt-in process test uses the production daemon built by tools compile; every
// server, credential, profile, and data directory is generated for this test.
func TestDaemonProcessSharesOneCoreAcrossFrontendsAndProxyProtocols(t *testing.T) {
	executable := os.Getenv("SSH_CLIENT_TEST_DAEMON")
	if executable == "" {
		t.Skip("build the daemon with tools compile and set SSH_CLIENT_TEST_DAEMON")
	}
	executable, err := filepath.Abs(executable)
	if err != nil {
		t.Fatal(err)
	}
	dir := b03bClientTestDir(t)
	source, err := vault.New([]byte("synthetic daemon integration password"))
	if err != nil {
		t.Fatal(err)
	}
	defer source.Close()
	fixture := testserver.New(t)
	kinds := []string{"shadowsocks", "snell", "snell6", "vless", "reality", "anytls", "hysteria2"}
	var temporaryRoutes []vault.SavedRoute
	for _, kind := range kinds {
		profile := fixture.Profile()
		profile.ID = kind
		profile.Name = kind
		profile.Network.Proxy = testproxy.New(t, kind)
		if kind == "snell6" || kind == "hysteria2" {
			temporaryRoutes = append(temporaryRoutes, vault.SavedRoute{ID: "saved-" + kind, Name: kind, Proxy: profile.Network.Proxy})
		}
		if err := source.Put(profile); err != nil {
			t.Fatal(err)
		}
	}
	routeOnly := fixture.Profile()
	routeOnly.ID = "temporary-route-target"
	routeOnly.Network.Proxy = connectcfg.Proxy{Mode: "http", Address: "127.0.0.1:1"}
	if err := source.Put(routeOnly); err != nil {
		t.Fatal(err)
	}
	if err := vault.Save(vault.Path(dir), source, true); err != nil {
		t.Fatal(err)
	}
	local := &LocalService{client: ServiceClient{Dir: dir, Executable: executable}}
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()
	if err := local.Ensure(ctx); err != nil {
		t.Fatal(err)
	}
	before, err := local.client.Status(ctx)
	if err != nil || before.State != protocol.ServiceStateLocked {
		t.Fatal("daemon did not start locked", err)
	}
	// A cancelled/failed test may still need to authenticate solely to stop its child.
	t.Cleanup(func() {
		cleanup, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		owner, err := vault.Read(vault.Path(dir), []byte("synthetic daemon integration password"))
		if err != nil {
			return
		}
		defer owner.Close()
		pending, err := local.client.PrepareOwner(cleanup, owner, 0)
		if err != nil {
			return
		}
		defer pending.Close()
		session, err := pending.Commit(cleanup)
		if err != nil {
			return
		}
		defer session.Close()
		_, _ = session.Stop(cleanup)
	})
	prepared, err := local.client.PrepareOwner(ctx, source, 0)
	if err != nil {
		t.Fatal(err)
	}
	defer prepared.Close()
	first, err := prepared.Commit(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer first.Close()
	secondPrepared, err := local.client.PrepareOwner(ctx, source, 0)
	if err != nil {
		t.Fatal(err)
	}
	defer secondPrepared.Close()
	second, err := secondPrepared.Commit(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer second.Close()
	source.Close()
	if _, err := source.Proof([]byte("root must be gone")); err == nil {
		t.Fatal("frontend retained root material")
	}
	pin, err := first.BrokerPublicKeyPin()
	if err != nil {
		t.Fatal(err)
	}
	data := Client{Dir: dir, BrokerKey: pin}
	for _, route := range temporaryRoutes {
		if err := data.SaveRoute(ctx, route, false, false, first); err != nil {
			t.Fatal(err)
		}
	}
	for _, selection := range []connectcfg.RouteSelection{{Mode: "direct"}, {Mode: "saved", ID: "saved-snell6"}, {Mode: "saved", ID: "saved-hysteria2"}} {
		stream, err := data.StartWithOwner(WithRoute(ctx, routeOnly.ID, selection), approval.Operation{Host: routeOnly.ID, Kind: "list", Path: "/"}, first)
		if err != nil {
			t.Fatal("managed owner temporary route", selection, err)
		}
		_, _, err = stream.Copy(io.Discard, io.Discard)
		stream.Close()
		if err != nil || stream.Operation.Route != selection {
			t.Fatal("daemon lost temporary route", err)
		}
	}
	if stream, err := data.ConnectWithOwner(ctx, routeOnly.ID, first); err == nil {
		stream.Close()
		t.Fatal("temporary route replaced the daemon's saved default")
	}
	for _, kind := range kinds {
		t.Run(kind, func(t *testing.T) {
			stream, err := data.StartWithOwner(ctx, approval.Operation{Host: kind, Kind: "exec", Command: "synthetic-daemon-command"}, first)
			if err != nil {
				t.Fatal(err)
			}
			code, _, err := stream.Copy(io.Discard, io.Discard)
			stream.Close()
			if err != nil || code != 0 {
				t.Fatal("SSH command through daemon", err, code)
			}
			stream, err = data.StartWithOwner(ctx, approval.Operation{Host: kind, Kind: "list", Path: "/"}, second)
			if err != nil {
				t.Fatal(err)
			}
			code, _, err = stream.Copy(io.Discard, io.Discard)
			stream.Close()
			if err != nil || code != 0 {
				t.Fatal("SFTP through daemon", err, code)
			}
		})
	}
	if err := local.Ensure(ctx); err != nil {
		t.Fatal(err)
	}
	after, err := local.client.Status(ctx)
	if err != nil || !after.StartedAt.Equal(before.StartedAt) || after.Generation != before.Generation {
		t.Fatal("frontends did not reuse one service/core", err)
	}
	first.Close()
	if _, err := data.ListSnippets(ctx, second); err != nil {
		t.Fatal("closing one owner disrupted another", err)
	}
	if _, err := data.ListSnippets(ctx, first); err == nil {
		t.Fatal("closed owner could still query")
	}
	if _, err := second.Lock(ctx); err != nil {
		t.Fatal(err)
	}
	status, err := local.client.Status(ctx)
	if err != nil || status.State != protocol.ServiceStateLocked {
		t.Fatal("lock did not close the shared core", err)
	}
	reopened, err := vault.Read(vault.Path(dir), []byte("synthetic daemon integration password"))
	if err != nil {
		t.Fatal(err)
	}
	anchor := reopened.AuditHead()
	reopened.Close()
	if anchor == "" {
		t.Fatal("daemon lock did not persist the audit anchor")
	}
}
