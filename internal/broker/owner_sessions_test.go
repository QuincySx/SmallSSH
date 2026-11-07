package broker_test

import (
	"context"
	"net"
	"os"
	"strings"
	"testing"
	"time"

	"sshclient/internal/broker"
	"sshclient/internal/client"
	"sshclient/internal/localipc"
	"sshclient/internal/ownercap"
	"sshclient/internal/protocol"
	"sshclient/internal/snippet"
	"sshclient/internal/testserver"
	"sshclient/internal/vault"
)

type managedSessionsFixture struct {
	dir         string
	server      *broker.Server
	profile     vault.Profile
	signer      *ownercap.Signer
	lease       *broker.OwnerLease
	identity    *localipc.ProcessIdentity
	ssh         *testserver.Server
	cancelOwner context.CancelFunc
	cli         client.Client
}

func managedSessionsTempDir(t *testing.T, prefix string) string {
	t.Helper()
	dir, err := os.MkdirTemp("/tmp", prefix)
	if err != nil {
		t.Fatal(err)
	}
	if err = os.Chmod(dir, 0o700); err != nil {
		_ = os.RemoveAll(dir)
		t.Fatal(err)
	}
	if err = localipc.Prepare(dir); err != nil {
		_ = os.RemoveAll(dir)
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(dir) })
	return dir
}

func newManagedSessionsFixture(t *testing.T) *managedSessionsFixture {
	return newManagedSessionsFixtureWithServer(t, testserver.New(t))
}

func newManagedSessionsFixtureWithServer(t *testing.T, fixture *testserver.Server) *managedSessionsFixture {
	t.Helper()
	dir := managedSessionsTempDir(t, "b04b2-")
	owner, err := vault.New(pass)
	if err != nil {
		t.Fatal(err)
	}
	profile := fixture.Profile()
	if err = owner.Put(profile); err != nil {
		owner.Close()
		t.Fatal(err)
	}
	if err = vault.Save(broker.VaultPath(dir), owner, true); err != nil {
		owner.Close()
		t.Fatal(err)
	}
	server, err := broker.OpenManagedSession(dir, owner, strings.Repeat("a", 32))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(server.Close)

	controlDir := managedSessionsTempDir(t, "b04b2-control-")
	accepted := make(chan struct {
		conn net.Conn
		err  error
	}, 1)
	var release func()
	var listener net.Listener
	var caller, serverConn net.Conn
	var identity *localipc.ProcessIdentity
	var ctrlCancel context.CancelFunc
	var signer *ownercap.Signer
	t.Cleanup(func() {
		if ctrlCancel != nil {
			ctrlCancel()
		}
		if serverConn != nil {
			_ = serverConn.Close()
		}
		if caller != nil {
			_ = caller.Close()
		}
		if identity != nil {
			identity.Close()
		}
		if listener != nil {
			_ = listener.Close()
		}
		if release != nil {
			release()
		}
		if signer != nil {
			signer.Close()
		}
	})

	release, err = localipc.Acquire(controlDir)
	if err != nil {
		t.Fatal(err)
	}
	listener, err = localipc.Listen(controlDir)
	if err != nil {
		t.Fatal(err)
	}
	dialCtx, dialCancel := context.WithTimeout(context.Background(), 2*time.Second)
	caller, err = localipc.Dial(dialCtx, controlDir)
	dialCancel()
	if err != nil {
		t.Fatal(err)
	}
	go func() {
		conn, err := listener.Accept()
		accepted <- struct {
			conn net.Conn
			err  error
		}{conn: conn, err: err}
	}()
	select {
	case result := <-accepted:
		if result.err != nil {
			t.Fatal(result.err)
		}
		serverConn = result.conn
	case <-time.After(2 * time.Second):
		t.Fatal("timed out accepting the local control connection")
	}
	identity, err = localipc.Identify(serverConn)
	if err != nil {
		t.Fatal(err)
	}
	if identity == nil || identity.Verify() != nil {
		t.Fatal("local control identity is unavailable")
	}

	ctrlCtx, cancel := context.WithCancel(context.Background())
	ctrlCancel = cancel
	signer, err = ownercap.NewSigner()
	if err != nil {
		t.Fatal(err)
	}
	lease, err := server.RegisterOwner(ctrlCtx, identity, signer.PublicKey(), 0)
	if err != nil {
		t.Fatal(err)
	}
	if err = signer.Bind(lease.Info()); err != nil {
		t.Fatal(err)
	}
	return &managedSessionsFixture{
		dir:         dir,
		server:      server,
		profile:     profile,
		signer:      signer,
		lease:       lease,
		identity:    identity,
		ssh:         fixture,
		cancelOwner: ctrlCancel,
		cli: client.Client{
			Dir:       dir,
			BrokerKey: server.PublicKeyPin(),
			Owner:     signer,
		},
	}
}

func signedManagedWorkspaceRead(t *testing.T, ctx context.Context, raw client.Client, signer *ownercap.Signer, query broker.Message) broker.Message {
	t.Helper()
	challenge, err := raw.Call(ctx, broker.Message{Action: "profileChallenge"})
	if err != nil || challenge.ID == "" {
		t.Fatalf("profile challenge failed: %v", err)
	}
	query.Challenge = challenge.ID
	proof, err := broker.OwnerQueryProof(query.Challenge, query, signer.Proof)
	if err != nil {
		t.Fatal(err)
	}
	query.Proof = proof
	return query
}

func TestManagedOwnerWorkspaceAndProfile(t *testing.T) {
	fixture := newManagedSessionsFixture(t)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	initial, err := fixture.cli.ListSnippets(ctx, fixture.signer)
	if err != nil || len(initial) != 0 {
		t.Fatalf("initial snippets = %#v, %v", initial, err)
	}
	command := snippet.Command{ID: "managed-command", Name: "Managed command", Body: "echo $1", Args: []snippet.Arg{{Name: "value"}}}
	if err = fixture.cli.SaveSnippet(ctx, command, false, fixture.signer); err != nil {
		t.Fatal(err)
	}
	commands, err := fixture.cli.ListSnippets(ctx, fixture.signer)
	if err != nil || len(commands) != 1 || commands[0].ID != command.ID || commands[0].Body != command.Body || len(commands[0].History) != 0 {
		t.Fatalf("saved snippets = %#v, %v", commands, err)
	}

	note := vault.ServerNote{Manual: "managed owner note"}
	if err = fixture.cli.SaveServerNote(ctx, fixture.profile.ID, note, fixture.signer); err != nil {
		t.Fatal(err)
	}
	storedNote, err := fixture.cli.ServerNote(ctx, fixture.profile.ID, fixture.signer)
	if err != nil || storedNote.Manual != note.Manual {
		t.Fatalf("stored note = %#v, %v", storedNote, err)
	}

	before, err := fixture.server.Vault.Profile(fixture.profile.ID)
	if err != nil {
		t.Fatal(err)
	}
	edited := before
	edited.Name = "Managed profile name"
	edited.Password = ""
	if err = fixture.cli.SaveConnection(ctx, vault.ProfileChange{Profile: edited, KeepCredential: true}, fixture.signer); err != nil {
		t.Fatal(err)
	}
	updated, err := fixture.server.Vault.Profile(fixture.profile.ID)
	if err != nil || updated.Name != edited.Name || updated.Password != before.Password {
		t.Fatalf("profile metadata update did not preserve credentials: name=%q, err=%v", updated.Name, err)
	}

	root := client.Client{Dir: fixture.dir, BrokerKey: fixture.server.PublicKeyPin(), Owner: fixture.server.Vault}
	rootCommands, err := root.ListSnippets(ctx, fixture.server.Vault)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, saved := range rootCommands {
		if saved.ID == command.ID && saved.Body == command.Body {
			found = true
			break
		}
	}
	if !found {
		t.Fatal("legacy root proof did not read the managed snippet")
	}
}

func TestManagedOwnerRejectsMissingAndReplayProof(t *testing.T) {
	fixture := newManagedSessionsFixture(t)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	raw := client.Client{Dir: fixture.dir, BrokerKey: fixture.server.PublicKeyPin()}
	query := broker.Message{Action: "workspaceRead", Workspace: &protocol.WorkspaceQuery{Kind: "snippet-list", Limit: 1}}
	if _, err := raw.Call(ctx, query); err == nil {
		t.Fatal("anonymous workspace read was accepted")
	}

	copiedSigner, err := ownercap.NewSigner()
	if err != nil {
		t.Fatal(err)
	}
	defer copiedSigner.Close()
	if err = copiedSigner.Bind(fixture.lease.Info()); err != nil {
		t.Fatal(err)
	}
	copied := signedManagedWorkspaceRead(t, ctx, raw, copiedSigner, query)
	if _, err = raw.Call(ctx, copied); err == nil {
		t.Fatal("copied public session metadata with a different key was accepted")
	}

	signed := signedManagedWorkspaceRead(t, ctx, raw, fixture.signer, query)
	tampered := signed
	changed := *signed.Workspace
	changed.Kind = "server-note"
	tampered.Workspace = &changed
	if _, err = raw.Call(ctx, tampered); err == nil {
		t.Fatal("a query changed after signing was accepted")
	}

	valid := signedManagedWorkspaceRead(t, ctx, raw, fixture.signer, query)
	first, err := raw.Call(ctx, valid)
	if err != nil || first.WorkspaceResult == nil {
		t.Fatalf("valid managed workspace read failed: %#v, %v", first.WorkspaceResult, err)
	}
	if _, err = raw.Call(ctx, valid); err == nil {
		t.Fatal("the exact signed query was replayed")
	}
}
