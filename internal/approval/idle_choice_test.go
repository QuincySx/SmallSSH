package approval

import (
	"testing"
	"time"
)

func TestOwnerIdleChoiceControlsGrantExpiry(t *testing.T) {
	for _, tc := range []struct {
		name               string
		idle               int
		aliveAt, expiresAt time.Duration
	}{
		{"disabled", 7200, 21 * time.Minute, 2 * time.Hour},
		{"five minutes", 300, 4 * time.Minute, 5 * time.Minute},
	} {
		t.Run(tc.name, func(t *testing.T) {
			now := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
			start := now
			m := New(prove)
			m.now = func() time.Time { return now }
			r, _, err := m.Create(Operation{Host: "server", Kind: "grant", Client: "synthetic", Grant: GrantScope{PublicKey: "AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA", Permission: "full", LifetimeSeconds: 7200, IdleSeconds: 1200}}, time.Minute)
			if err != nil {
				t.Fatal(err)
			}
			reviewed := r
			reviewed.Operation.Grant.IdleSeconds = tc.idle
			proof, err := DecisionProof(reviewed, true, prove)
			if err != nil {
				t.Fatal(err)
			}
			if err := m.DecideWithLifetime(r.ID, true, 7200, tc.idle, proof); err != nil {
				t.Fatal(err)
			}
			now = start.Add(tc.aliveAt)
			got, err := m.Get(r.ID)
			if err != nil || got.Status != "approved" || got.Operation.Grant.IdleSeconds != tc.idle {
				t.Fatal("chosen terms not active", got, err)
			}
			now = start.Add(tc.expiresAt)
			got, err = m.Get(r.ID)
			if err == nil && got.Status == "approved" {
				t.Fatal("grant survived its deadline")
			}
		})
	}
}
