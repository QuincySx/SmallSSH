package transfer

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"sshclient/internal/broker"
	"sshclient/internal/client"
	"sshclient/internal/connectcfg"
	"sshclient/internal/testserver"
	"sshclient/internal/vault"
	"testing"
	"time"
)

func TestTransferPersistsSourceRouteAndOverridesLaterTabSelection(t *testing.T) {
	fixture := testserver.New(t)
	dir, err := os.MkdirTemp("", "route-job-")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(dir)
	pass := []byte("synthetic route transfer password")
	owner, err := vault.New(pass)
	if err != nil {
		t.Fatal(err)
	}
	p := fixture.Profile()
	p.Network.Proxy = connectcfg.Proxy{Mode: "http", Address: "127.0.0.1:1"}
	if err := owner.Put(p); err != nil {
		t.Fatal(err)
	}
	if err := vault.Save(broker.VaultPath(dir), owner, true); err != nil {
		t.Fatal(err)
	}
	server, err := broker.OpenSession(dir, owner)
	if err != nil {
		t.Fatal(err)
	}
	defer server.Close()
	source := filepath.Join(t.TempDir(), "source")
	body := []byte("source route remains fixed\n")
	if err := os.WriteFile(source, body, 0600); err != nil {
		t.Fatal(err)
	}
	remote := testserver.RemotePath(filepath.Join(t.TempDir(), "uploaded"))
	manager := New(dir)
	route := connectcfg.RouteSelection{Mode: "direct"}
	job, err := manager.Add(p.ID, "upload", source, remote, route)
	if err != nil {
		t.Fatal(err)
	}
	manager = New(dir)
	if got := manager.List(); len(got) != 1 || got[0].Route != route {
		t.Fatal("route not persisted", got)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	ctx = client.WithRoute(ctx, p.ID, connectcfg.RouteSelection{Mode: "saved", ID: "different-tab"})
	if err := manager.RunWithOwner(ctx, job.ID, "route test", server.Vault); err != nil {
		t.Fatal(err)
	}
	actual, err := os.ReadFile(testserver.LocalPath(remote))
	if err != nil || !bytes.Equal(body, actual) {
		t.Fatal("upload changed", err)
	}
	stored, err := server.Vault.Profile(p.ID)
	if err != nil || stored.Network != p.Network {
		t.Fatal("job changed saved route", err)
	}
	// Non-owner resumes cannot acquire an owner-selected transport from disk.
	second, err := manager.Add(p.ID, "download", filepath.Join(t.TempDir(), "download"), remote, route)
	if err != nil {
		t.Fatal(err)
	}
	if err := manager.Run(ctx, second.ID, "ordinary"); err == nil {
		t.Fatal("ordinary client resumed owner route")
	}
}
