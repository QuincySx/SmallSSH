package approval

import (
	"context"
	"testing"
	"time"
)

func TestLiveMediumReviewChangesFutureOperationsAndRevokesUnusedPermits(t *testing.T) {
	m, p, _, key := grantFixture(t, "guarded")
	approve(t, m, p)
	original, err := m.Get(p.ID)
	if err != nil || !original.ConfirmsMediumReview() {
		t.Fatal("guarded session should default to review", err)
	}
	now := time.Now()
	m.now = func() time.Time { return now }
	confirm := false
	change := GrantPolicyChange{ID: p.ID, Nonce: "old-generation", MediumReview: &confirm}
	if _, _, err := m.SetGrantPolicy(change); err == nil {
		t.Fatal("accepted stale generation")
	}
	change.Nonce = p.Nonce
	before, after, err := m.SetGrantPolicy(change)
	if err != nil || !before.ConfirmsMediumReview() || after.ConfirmsMediumReview() ||
		after.Expires != original.Expires || after.Activity.LastUsed != original.Activity.LastUsed ||
		after.Activity.IdleSeconds != original.Activity.IdleSeconds || after.Operation != original.Operation {
		t.Fatal("live change modified original grant or deadlines", before, after, err)
	}
	op := grantedOperation(p, "mkdir")
	op.Path = "/srv/live-review-fixture"
	unused, ticket, err := mintGuarded(m, p, key, op)
	if err != nil || unused.Status != "approved" || unused.MediumReview || unused.ParentGeneration != p.Generation() {
		t.Fatal("active opt-out or audit context missing", unused, err)
	}
	op.Path += "-running"
	running, runningTicket, err := mintGuarded(m, p, key, op)
	if err != nil {
		t.Fatal(err)
	}
	ctx, _, cancel, err := m.Start(context.Background(), running.ID, runningTicket)
	if err != nil {
		t.Fatal(err)
	}
	defer cancel()
	confirm = true
	if _, _, err = m.SetGrantPolicy(change); err != nil {
		t.Fatal(err)
	}
	if ctx.Err() != nil {
		t.Fatal("turning on review cancelled running work", ctx.Err())
	}
	if _, _, cancel, err := m.Start(context.Background(), unused.ID, ticket); err == nil {
		cancel()
		t.Fatal("unused automatic approval survived re-enabling review")
	}
	op.Path += "-new"
	pending, _, err := mintGuarded(m, p, key, op)
	if err != nil || pending.Status != "pending" || !pending.MediumReview {
		t.Fatal("next operation did not require review", pending, err)
	}
	confirm = false
	if _, _, err = m.SetGrantPolicy(change); err != nil {
		t.Fatal(err)
	}
	if pending, err = m.Get(pending.ID); err != nil || pending.Status != "pending" {
		t.Fatal("turning off review approved an existing pending entry", pending, err)
	}
	revoke, err := RevokeProof(original, prove)
	if err != nil || m.Revoke(p.ID, revoke) != nil {
		t.Fatal("live rules invalidated the original revoke proof", err)
	}
}

func TestGrantPolicyEditsKeepProofsAndDeadlineButExpireIdleChildren(t *testing.T) {
	m, p, _, key := grantFixture(t, "full")
	now := time.Now()
	m.now = func() time.Time { return now }
	approve(t, m, p)
	p, _ = m.Get(p.ID)
	original := p.Expires
	child, ticket := delegated(t, m, p, key, "exec")
	ctx, _, cancel, err := m.Start(context.Background(), child.ID, ticket)
	if err != nil {
		t.Fatal(err)
	}
	defer cancel()
	proof, err := RevokeProof(p, prove)
	if err != nil {
		t.Fatal(err)
	}
	now = now.Add(10 * time.Minute)
	_, after, err := m.SetGrantPolicy(GrantPolicyChange{ID: p.ID, Nonce: p.Nonce, IdleSeconds: 0})
	if err != nil || after.Activity == nil || after.Activity.IdleSeconds != 0 || after.Expires != original {
		t.Fatal("policy edit extended or changed authority", after, err)
	}
	newProof, _ := RevokeProof(after, prove)
	if string(newProof) != string(proof) {
		t.Fatal("activity changed owner revoke proof")
	}
	_, after, err = m.SetGrantPolicy(GrantPolicyChange{ID: p.ID, Nonce: p.Nonce, IdleSeconds: 60})
	if err != nil || after.Status != "expired" {
		t.Fatal("tightening reset last activity", err)
	}
	select {
	case <-ctx.Done():
	default:
		t.Fatal("idle parent did not cancel running child")
	}
	if _, _, err = m.SetGrantPolicy(GrantPolicyChange{ID: p.ID, Nonce: p.Nonce, IdleSeconds: 0}); err == nil {
		t.Fatal("expired authority revived")
	}
}

func TestGrantPolicyRejectsStaleAndInvalidChanges(t *testing.T) {
	m, p, _, _ := grantFixture(t, "read")
	approve(t, m, p)
	for _, c := range []GrantPolicyChange{{ID: p.ID, Nonce: "stale", IdleSeconds: 0}, {ID: p.ID, Nonce: p.Nonce, IdleSeconds: 59}, {ID: p.ID, Nonce: p.Nonce, IdleSeconds: 1801}, {ID: "missing", Nonce: p.Nonce, IdleSeconds: 900}} {
		if _, _, err := m.SetGrantPolicy(c); err == nil {
			t.Fatal("invalid edit accepted", c)
		}
	}
}
