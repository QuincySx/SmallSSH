package broker_test

import (
	"context"
	"testing"
	"time"

	"sshclient/internal/approval"
	"sshclient/internal/testserver"
)

// Browsing a directory is a run of quick operations; without reuse each one pays
// a full SSH handshake first.
func TestQuickOperationsReuseOneVerifiedConnection(t *testing.T) {
	c, fixture := setup(t)
	ctx := context.Background()
	before := fixture.Auths.Load()
	for i := 0; i < 3; i++ {
		op := approval.Operation{Host: "fixture", Kind: "list", Path: testserver.RemotePath("/"), Client: "browser"}
		r, ticket := request(t, c, op)
		approve(t, c, r)
		stream, err := c.Start(ctx, r.ID, ticket, 0, 0)
		if err != nil {
			t.Fatal(err)
		}
		if _, _, err = stream.Copy(discard{}, discard{}); err != nil {
			t.Fatal(err)
		}
		stream.Close()
	}
	if got := fixture.Auths.Load() - before; got != 1 {
		t.Fatalf("%d SSH authentications for three listings, want 1", got)
	}
}

// A terminal keeps its own connection, so it neither takes one from the pool nor
// puts one back.
func TestTerminalDoesNotShareTheReusedConnection(t *testing.T) {
	c, fixture := setup(t)
	ctx := context.Background()
	before := fixture.Auths.Load()
	for i := 0; i < 2; i++ {
		op := approval.Operation{Host: "fixture", Kind: "shell", Client: "terminal"}
		r, ticket := request(t, c, op)
		approve(t, c, r)
		stream, err := c.Start(ctx, r.ID, ticket, 0, 0)
		if err != nil {
			t.Fatal(err)
		}
		stream.Close()
		// Give the broker a moment to tear the session down before the next one.
		time.Sleep(50 * time.Millisecond)
	}
	if got := fixture.Auths.Load() - before; got != 2 {
		t.Fatalf("%d SSH authentications for two terminals, want 2", got)
	}
}

type discard struct{}

func (discard) Write(p []byte) (int, error) { return len(p), nil }
