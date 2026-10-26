package approval

import (
	"context"
	"testing"
	"time"
)

func TestHostInvalidationPreservesOtherServerSessions(t *testing.T) {
	m := New(prove)
	contexts := map[string]context.Context{}
	ids := map[string]string{}
	for _, host := range []string{"one", "two"} {
		r, ticket, err := m.Create(Operation{Host: host, Kind: "shell", Client: "test"}, time.Minute)
		if err != nil {
			t.Fatal(err)
		}
		approve(t, m, r)
		ctx, _, _, err := m.Start(context.Background(), r.ID, ticket)
		if err != nil {
			t.Fatal(err)
		}
		contexts[host] = ctx
		ids[host] = r.ID
	}
	defer m.Lock()
	m.InvalidateHost("one")
	if contexts["one"].Err() == nil {
		t.Fatal("changed server session survived")
	}
	if contexts["two"].Err() != nil {
		t.Fatal("unrelated server was disconnected")
	}
	if _, err := m.Get(ids["one"]); err == nil {
		t.Fatal("old request survived target update")
	}
}
