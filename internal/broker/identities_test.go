package broker

import (
	"context"
	"sync"
	"testing"
	"time"

	"sshclient/internal/client"
	"sshclient/internal/identitymodel"
	"sshclient/internal/protocol"
)

type offlineIdentityController struct {
	mu                 sync.Mutex
	changes            []identitymodel.Change
	started, cancelled chan struct{}
	closed             bool
}

func (c *offlineIdentityController) State(context.Context) (identitymodel.State, error) {
	return identitymodel.State{Supported: true, Keys: []identitymodel.Key{{ID: "offline", Name: "Synthetic public identity"}}}, nil
}
func (c *offlineIdentityController) Change(ctx context.Context, v identitymodel.Change) error {
	c.mu.Lock()
	c.changes = append(c.changes, v)
	c.mu.Unlock()
	if c.started != nil {
		close(c.started)
		<-ctx.Done()
		close(c.cancelled)
		return ctx.Err()
	}
	return nil
}
func (c *offlineIdentityController) Close() { c.mu.Lock(); c.closed = true; c.mu.Unlock() }
func TestSSHIdentityOwnerProofBindsExactIntent(t *testing.T) {
	dir, server := newCoreLifetimeTestServer(t)
	c := &offlineIdentityController{}
	server.identityAgent = c
	cli := client.Client{Dir: dir, Owner: server.Vault}
	ctx := context.Background()
	if _, err := cli.Identities(ctx, server.Vault); err != nil {
		t.Fatal(err)
	}
	value := identitymodel.Change{Action: "persist", ID: "offline", Seconds: 60}
	if err := cli.ChangeIdentity(ctx, value, server.Vault); err != nil {
		t.Fatal(err)
	}
	untrusted := client.Client{Dir: dir}
	for _, q := range []protocol.Message{{Action: "workspaceRead", Workspace: &protocol.WorkspaceQuery{Kind: "ssh-identities"}}, {Action: "workspaceWrite", WorkspaceChange: &protocol.WorkspaceChange{Kind: "ssh-identity-change", Identity: &value}}} {
		if _, err := untrusted.Call(ctx, q); err == nil {
			t.Fatal("untrusted client managed signing identities")
		}
	}
	challenge, err := untrusted.Call(ctx, protocol.Message{Action: "profileChallenge"})
	if err != nil {
		t.Fatal(err)
	}
	q := protocol.Message{Action: "workspaceWrite", Challenge: challenge.ID, WorkspaceChange: &protocol.WorkspaceChange{Kind: "ssh-identity-change", Identity: &value}}
	q.Proof, err = protocol.OwnerQueryProof(q.Challenge, q, server.Vault.Proof)
	if err != nil {
		t.Fatal(err)
	}
	value.Seconds = 86400
	if _, err = untrusted.Call(ctx, q); err == nil {
		t.Fatal("mutated lifetime kept owner proof")
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if len(c.changes) != 1 || c.changes[0].Seconds != 60 {
		t.Fatal("unapproved management reached signer", c.changes)
	}
}
func TestSSHIdentityOwnerDisconnectCancelsPendingAuthentication(t *testing.T) {
	dir, server := newCoreLifetimeTestServer(t)
	c := &offlineIdentityController{started: make(chan struct{}), cancelled: make(chan struct{})}
	server.identityAgent = c
	cli := client.Client{Dir: dir, Owner: server.Vault}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() {
		done <- cli.ChangeIdentity(ctx, identitymodel.Change{Action: "persist", ID: "offline", Seconds: 60}, server.Vault)
	}()
	select {
	case <-c.started:
	case <-time.After(3 * time.Second):
		t.Fatal("owner change did not reach offline controller")
	}
	cancel()
	select {
	case <-c.cancelled:
	case <-time.After(3 * time.Second):
		t.Fatal("owner disconnect left native prompt running")
	}
	select {
	case err := <-done:
		if err == nil {
			t.Fatal("canceled owner call succeeded")
		}
	case <-time.After(3 * time.Second):
		t.Fatal("owner request did not return")
	}
}
