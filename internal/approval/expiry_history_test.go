package approval

import (
	"context"
	"testing"
	"time"
)

func TestExpiredAuditQueueRetainsEarlyPruningAndDrainsOnce(t *testing.T) {
	now := time.Now()
	m := NewWithClock(prove, func() time.Time { return now })
	op := Operation{Host: "test", Client: "AI", Kind: "exec", Command: "safe"}
	pending, _, err := m.Create(op, time.Second)
	if err != nil {
		t.Fatal(err)
	}
	running, ticket, err := m.Create(op, time.Second)
	if err != nil {
		t.Fatal(err)
	}
	approve(t, m, running)
	ctx, _, cancel, err := m.Start(context.Background(), running.ID, ticket)
	if err != nil {
		t.Fatal(err)
	}
	defer cancel()
	finished, _, err := m.Create(op, time.Second)
	if err != nil {
		t.Fatal(err)
	}
	proof, _ := DecisionProof(finished, false, prove)
	if err = m.Decide(finished.ID, false, proof); err != nil {
		t.Fatal(err)
	}
	now = now.Add(2 * time.Second)
	_, _ = m.Get(pending.ID) // pruning before the scheduled broker sweep
	if ctx.Err() == nil {
		t.Fatal("expiry did not cancel running operation")
	}
	seen := map[string]bool{}
	for _, r := range m.DrainExpired() {
		if r.Status != "expired" {
			t.Fatal("wrong historical state", r.Status)
		}
		seen[r.ID] = true
	}
	if len(seen) != 2 || !seen[pending.ID] || !seen[running.ID] {
		t.Fatal("expiry lost or duplicated", seen)
	}
	m.Sweep()
	if len(m.DrainExpired()) != 0 {
		t.Fatal("expiry emitted twice")
	}
}
