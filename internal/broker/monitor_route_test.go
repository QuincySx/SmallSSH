package broker

import (
	"context"
	"sshclient/internal/sshconn"
	"sshclient/internal/testserver"
	"testing"
)

func TestMonitorCollectorsSeparateRoutesAndRouteRevisions(t *testing.T) {
	fixture := testserver.New(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	connection, err := sshconn.Dial(ctx, fixture.Profile())
	if err != nil {
		t.Fatal(err)
	}
	defer connection.Close()
	server := newSharedMonitorTestServer(t)
	p := fixture.Profile()
	firstKey := poolLease{key: pooledKey(p), generation: 1}
	first, _, err := server.subscribeMonitor(ctx, connection, p.ID, p.Fingerprint, firstKey)
	if err != nil {
		t.Fatal(err)
	}
	again, _, err := server.subscribeMonitor(ctx, connection, p.ID, p.Fingerprint, firstKey)
	if err != nil || again != first {
		t.Fatal("same route did not share", err)
	}
	different := firstKey
	different.key.options = "different-route"
	other, _, err := server.subscribeMonitor(ctx, connection, p.ID, p.Fingerprint, different)
	if err != nil || other == first {
		t.Fatal("different routes shared a collector", err)
	}
	revised := firstKey
	revised.generation++
	updated, _, err := server.subscribeMonitor(ctx, connection, p.ID, p.Fingerprint, revised)
	if err != nil || updated == first {
		t.Fatal("edited route reused old monitor transport", err)
	}
}
