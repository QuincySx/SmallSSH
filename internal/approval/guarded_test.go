package approval

import (
	"context"
	"crypto/ed25519"
	"errors"
	"sync"
	"testing"
	"time"
)

func mintGuarded(m *Manager, p Request, key ed25519.PrivateKey, op Operation) (Request, string, error) {
	c, err := m.DelegationChallenge(p.ID, op, 5*time.Minute)
	if err != nil {
		return Request{}, "", err
	}
	return m.MintDelegated(c.ID, ed25519.Sign(key, DelegationProofMessage(c)))
}

func TestGuardedSessionReviewsOnlyTheExactOperation(t *testing.T) {
	m, p, _, key := grantFixture(t, "guarded")
	approve(t, m, p)
	op := grantedOperation(p, "exec")
	op.Command = "uname -a"
	low, _, err := mintGuarded(m, p, key, op)
	if err != nil || low.Status != "approved" {
		t.Fatal(low, err)
	}
	op.Command = "systemctl restart nginx"
	r, ticket, err := mintGuarded(m, p, key, op)
	if err != nil || r.Status != "pending" || r.ParentID != p.ID {
		t.Fatal(r, err)
	}
	if _, _, _, err = m.Start(context.Background(), r.ID, ticket); err == nil {
		t.Fatal("medium operation ran without a fresh owner decision")
	}
	if err = m.Decide(r.ID, true, []byte("client self approval")); err == nil {
		t.Fatal("client approved itself")
	}
	other := op
	other.Client = "different self-reported label"
	if _, _, err = mintGuarded(m, p, key, other); !errors.Is(err, ErrReviewPending) {
		t.Fatal("duplicate review was not suppressed", err)
	}
	proof, _ := DecisionProof(r, true, prove)
	if err = m.Decide(r.ID, true, proof); err != nil {
		t.Fatal(err)
	}
	_, actual, cancel, err := m.Start(context.Background(), r.ID, ticket)
	if err != nil || actual != op {
		t.Fatal(actual, err)
	}
	cancel()
	m.Finish(r.ID)
	if _, _, _, err = m.Start(context.Background(), r.ID, ticket); err == nil {
		t.Fatal("single-use operation replayed")
	}
	later, _, err := mintGuarded(m, p, key, op)
	if err != nil || later.Status != "pending" {
		t.Fatal("approval widened the parent", later, err)
	}
	if err = m.Decide(later.ID, true, proof); err == nil {
		t.Fatal("old owner proof approved another request")
	}
	denial, _ := DecisionProof(later, false, prove)
	if err = m.Decide(later.ID, false, denial); err != nil {
		t.Fatal(err)
	}
	op.Command = "uname -a"
	if low, _, err = mintGuarded(m, p, key, op); err != nil || low.Status != "approved" {
		t.Fatal("denial destroyed unrelated session authority", low, err)
	}
	before := len(m.List())
	for _, command := range []string{"rm -rf /", "mkfs /dev/sda", "systemctl stop sshd"} {
		op.Command = command
		if _, _, err = mintGuarded(m, p, key, op); err == nil {
			t.Fatal("high risk created authority", command)
		}
	}
	if len(m.List()) != before {
		t.Fatal("high risk created pending reviews")
	}
}

func TestGuardedReviewDeduplicationIsAtomic(t *testing.T) {
	m, p, _, key := grantFixture(t, "guarded")
	approve(t, m, p)
	op := grantedOperation(p, "exec")
	op.Command = "systemctl restart nginx"
	challenges := make([]DelegationChallenge, 8)
	for i := range challenges {
		var err error
		challenges[i], err = m.DelegationChallenge(p.ID, op, time.Minute)
		if err != nil {
			t.Fatal(err)
		}
	}
	var wg sync.WaitGroup
	errorsOut := make(chan error, len(challenges))
	for _, c := range challenges {
		wg.Go(func() {
			_, _, err := m.MintDelegated(c.ID, ed25519.Sign(key, DelegationProofMessage(c)))
			errorsOut <- err
		})
	}
	wg.Wait()
	close(errorsOut)
	successes := 0
	for err := range errorsOut {
		if err == nil {
			successes++
		} else if !errors.Is(err, ErrReviewPending) {
			t.Fatal(err)
		}
	}
	if successes != 1 {
		t.Fatal("duplicate pending operations", successes)
	}
	pending := 0
	for _, r := range m.List() {
		if r.Status == "pending" {
			pending++
			if r.ParentID != p.ID {
				t.Fatal("unbound intermediate request escaped", r)
			}
		}
	}
	if pending != 1 {
		t.Fatal(pending)
	}
}

func TestGuardedReviewsCannotSurviveParentExpiryOrRevocation(t *testing.T) {
	for _, revoke := range []bool{false, true} {
		m, p, _, key := grantFixture(t, "guarded")
		now := time.Now()
		m.now = func() time.Time { return now }
		p.Operation.Grant.LifetimeSeconds, p.Operation.Grant.IdleSeconds = 60, 60
		m.pending[p.ID].request.Operation.Grant = p.Operation.Grant
		approve(t, m, p)
		op := grantedOperation(p, "exec")
		op.Command = "systemctl restart nginx"
		r, ticket, err := mintGuarded(m, p, key, op)
		if err != nil {
			t.Fatal(err)
		}
		proof, _ := DecisionProof(r, true, prove)
		current, _ := m.Get(p.ID)
		if r.Expires.After(current.Expires) {
			t.Fatal("pending operation outlived parent", r.Expires, current.Expires)
		}
		if revoke {
			revokeProof, _ := RevokeProof(current, prove)
			if err = m.Revoke(p.ID, revokeProof); err != nil {
				t.Fatal(err)
			}
		} else {
			now = now.Add(61 * time.Second)
		}
		if err = m.Decide(r.ID, true, proof); err == nil {
			t.Fatal("stale parent still allowed approval")
		}
		if _, _, _, err = m.Start(context.Background(), r.ID, ticket); err == nil {
			t.Fatal("stale review executed")
		}
	}
}

func TestGuardedScopeStillLimitsDirectoriesAndDeadlines(t *testing.T) {
	m, p, _, key := grantFixture(t, "guarded")
	scope := p.Operation.Grant
	scope.ReadRoot, scope.WriteRoot = "/srv/read", "/srv/write"
	m.pending[p.ID].request.Operation.Grant = scope
	p.Operation.Grant = scope
	approve(t, m, p)
	op := grantedOperation(p, "list")
	op.Path = "/srv/read/a"
	if r, _, err := mintGuarded(m, p, key, op); err != nil || r.Status != "approved" {
		t.Fatal(r, err)
	}
	op.Kind, op.Path = "remove", "/srv/write/a"
	r, _, err := mintGuarded(m, p, key, op)
	if err != nil || r.Status != "pending" {
		t.Fatal(r, err)
	}
	parent, _ := m.Get(p.ID)
	if r.Expires.After(parent.Expires) {
		t.Fatal("review outlived its session")
	}
	for _, path := range []string{"/srv/write", "/srv/other/a"} {
		op.Path = path
		if _, _, err := mintGuarded(m, p, key, op); err == nil {
			t.Fatal("scope widened", path)
		}
	}
	op = grantedOperation(p, "exec")
	op.Command = "uname -a"
	if _, _, err := mintGuarded(m, p, key, op); err == nil {
		t.Fatal("directory grant acquired a command")
	}
}

func TestGuardedPendingRequestsDoNotRenewIdleButOwnerConsentDoes(t *testing.T) {
	for _, consent := range []bool{false, true} {
		m, p, _, key := grantFixture(t, "guarded")
		now := time.Now()
		m.now = func() time.Time { return now }
		p.Operation.Grant.IdleSeconds = 60
		m.pending[p.ID].request.Operation.Grant = p.Operation.Grant
		approve(t, m, p)
		now = now.Add(50 * time.Second)
		op := grantedOperation(p, "exec")
		op.Command = "systemctl restart nginx"
		r, ticket, err := mintGuarded(m, p, key, op)
		if err != nil {
			t.Fatal(err)
		}
		proof, _ := DecisionProof(r, true, prove)
		if consent {
			now = now.Add(5 * time.Second)
			if err = m.Decide(r.ID, true, proof); err != nil {
				t.Fatal(err)
			}
			now = now.Add(10 * time.Second)
			current, err := m.Get(p.ID)
			if err != nil || current.Status != "approved" {
				t.Fatal("fresh owner consent did not count as activity", current, err)
			}
		} else {
			now = now.Add(15 * time.Second)
			if err = m.Decide(r.ID, true, proof); err == nil {
				t.Fatal("unapproved request extended the idle deadline")
			}
			if _, _, _, err = m.Start(context.Background(), r.ID, ticket); err == nil {
				t.Fatal("idle-expired operation executed")
			}
		}
	}
}
