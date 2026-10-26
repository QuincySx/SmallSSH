package approval

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func expiredGrant(t *testing.T, permission string) (*Manager, Request, string, ed25519.PrivateKey, *time.Time) {
	t.Helper()
	m, r, ticket, key := grantFixture(t, permission)
	now := time.Now()
	m.now = func() time.Time { return now }
	approve(t, m, r)
	now = now.Add(16 * time.Minute)
	m.Sweep()
	status, err := m.SessionStatus(r.ID)
	if err != nil || status.Status != "expired" {
		t.Fatal("natural expiry not distinguished", err, status.Status)
	}
	return m, r, ticket, key, &now
}

func renewalChallenge(t *testing.T, m *Manager, id string) *RenewalChallenge {
	t.Helper()
	_, c, err := m.RenewalChallenge(id)
	if err != nil || c == nil {
		t.Fatal("renewal challenge unavailable", err)
	}
	return c
}

func TestRenewalRequiresFreshOwnerProofAndKeepsOriginalScope(t *testing.T) {
	m, original, _, key, _ := expiredGrant(t, "full")
	op := grantedOperation(original, "list")
	if _, err := m.DelegationChallenge(original.ID, op, time.Minute); !errors.Is(err, ErrSessionExpired) {
		t.Fatal("not a renewable expiry", err)
	}
	c := renewalChallenge(t, m, original.ID)
	if len(m.List()) != 0 {
		t.Fatal("requesting a challenge queued or approved access")
	}
	r, created, err := m.Renew(c.ID, ed25519.Sign(key, RenewalProofMessage(*c)))
	if err != nil || !created || r.Status != "pending" || !r.Renewal || r.Nonce == original.Nonce || r.Operation != original.Operation {
		t.Fatal("renewal changed scope or restored authority", err, r)
	}
	oldProof, _ := DecisionProof(original, true, prove)
	if err := m.Decide(r.ID, true, oldProof); err == nil {
		t.Fatal("replayed previous owner approval")
	}
	if _, err := m.DelegationChallenge(r.ID, op, time.Minute); !errors.Is(err, ErrSessionRenewing) {
		t.Fatal("executed while awaiting renewed consent", err)
	}
	approve(t, m, r)
	_, ticket := delegated(t, m, r, key, "exec")
	if ticket == "" {
		t.Fatal("freshly approved renewal unusable")
	}
}

func TestConcurrentRenewalProofsJoinOnePendingGeneration(t *testing.T) {
	m, r, _, key, _ := expiredGrant(t, "full")
	const workers = 12
	challenges := make([]*RenewalChallenge, workers)
	for i := range challenges {
		challenges[i] = renewalChallenge(t, m, r.ID)
	}
	var created atomic.Int32
	var wg sync.WaitGroup
	results := make(chan Request, workers)
	for _, c := range challenges {
		wg.Add(1)
		go func(c *RenewalChallenge) {
			defer wg.Done()
			next, newRequest, err := m.Renew(c.ID, ed25519.Sign(key, RenewalProofMessage(*c)))
			if err != nil {
				t.Error(err)
				return
			}
			if newRequest {
				created.Add(1)
			}
			results <- next
		}(c)
	}
	wg.Wait()
	close(results)
	var nonce string
	for next := range results {
		if nonce == "" {
			nonce = next.Nonce
		}
		if next.ID != r.ID || next.Nonce != nonce || next.Status != "pending" {
			t.Fatal("parallel operations made multiple generations")
		}
	}
	if created.Load() != 1 || len(m.List()) != 1 {
		t.Fatal("multiple owner requests created", created.Load(), len(m.List()))
	}
	if _, _, err := m.Renew(challenges[0].ID, ed25519.Sign(key, RenewalProofMessage(*challenges[0]))); err == nil {
		t.Fatal("renewal challenge replayed")
	}
}

func TestRenewalRejectsWrongKeyTamperingReplayAndExpiredChallenges(t *testing.T) {
	for _, kind := range []string{"wrong-key", "scope", "target", "nonce", "expired"} {
		t.Run(kind, func(t *testing.T) {
			m, r, _, key, now := expiredGrant(t, "read")
			original := renewalChallenge(t, m, r.ID)
			changed := *original
			switch kind {
			case "wrong-key":
				_, key, _ = ed25519.GenerateKey(rand.Reader)
			case "scope":
				changed.Operation.Grant.Permission = "full"
			case "target":
				changed.Operation.Host = "another-server"
			case "nonce":
				changed.PreviousNonce = "different-generation"
			case "expired":
				*now = now.Add(30 * time.Second)
			}
			if _, _, err := m.Renew(original.ID, ed25519.Sign(key, RenewalProofMessage(changed))); err == nil {
				t.Fatal("accepted invalid renewal", kind)
			}
			if _, _, err := m.Renew(original.ID, ed25519.Sign(key, RenewalProofMessage(*original))); err == nil {
				t.Fatal("failed challenge reusable")
			}
			if len(m.List()) != 0 {
				t.Fatal("invalid request opened a new approval")
			}
		})
	}
}

func TestRevocationDenialConfigurationAndLockNeverAutomaticallyRenew(t *testing.T) {
	for _, action := range []string{"withdraw-expired", "withdraw-pending", "denied", "host", "lock", "never-approved"} {
		t.Run(action, func(t *testing.T) {
			m, r, ticket, key, now := expiredGrant(t, "read")
			c := renewalChallenge(t, m, r.ID)
			switch action {
			case "withdraw-expired":
				if err := m.Withdraw(r.ID, ticket); err != nil {
					t.Fatal(err)
				}
			case "host":
				m.InvalidateHost(r.Operation.Host)
			case "lock":
				m.Lock()
			case "never-approved":
				m, r, _, key = grantFixture(t, "read")
				m.now = func() time.Time { return now.Add(10 * time.Minute) }
				m.Sweep()
			default:
				pending, _, err := m.Renew(c.ID, ed25519.Sign(key, RenewalProofMessage(*c)))
				if err != nil {
					t.Fatal(err)
				}
				if action == "denied" {
					proof, _ := DecisionProof(pending, false, prove)
					if err = m.Decide(r.ID, false, proof); err != nil {
						t.Fatal(err)
					}
				} else if err = m.Withdraw(r.ID, ticket); err != nil {
					t.Fatal(err)
				}
				*now = now.Add(10 * time.Minute)
				m.Sweep()
			}
			if _, _, err := m.Renew(c.ID, ed25519.Sign(key, RenewalProofMessage(*c))); err == nil {
				t.Fatal("cancelled session renewed", action)
			}
			if _, _, err := m.RenewalChallenge(r.ID); err == nil {
				t.Fatal("cancelled or unknown session requested again", action)
			}
		})
	}
}

func TestReadScopeViolationDoesNotTriggerRenewal(t *testing.T) {
	m, r, _, key, _ := expiredGrant(t, "read")
	op := grantedOperation(r, "exec")
	op.Path = ""
	op.Command = "denied-command"
	if _, err := m.DelegationChallenge(r.ID, op, time.Minute); !errors.Is(err, ErrDenied) {
		t.Fatal("out-of-scope command asked to renew", err)
	}
	c := renewalChallenge(t, m, r.ID)
	if _, _, err := m.Renew(c.ID, ed25519.Sign(key, RenewalProofMessage(*c))); err != nil {
		t.Fatal(err)
	}
	if _, err := m.DelegationChallenge(r.ID, op, time.Minute); !errors.Is(err, ErrDenied) {
		t.Fatal("out-of-scope command joined a renewal", err)
	}
}

func TestActiveRevocationCannotBecomeRenewableWhenItsDeadlinePasses(t *testing.T) {
	for _, action := range []string{"revoke", "withdraw"} {
		m, r, ticket, _ := grantFixture(t, "read")
		now := time.Now()
		m.now = func() time.Time { return now }
		approve(t, m, r)
		current, _ := m.Get(r.ID)
		if action == "revoke" {
			proof, _ := RevokeProof(current, prove)
			if err := m.Revoke(r.ID, proof); err != nil {
				t.Fatal(err)
			}
		} else if err := m.Withdraw(r.ID, ticket); err != nil {
			t.Fatal(err)
		}
		now = now.Add(3 * time.Hour)
		m.Sweep()
		if _, _, err := m.RenewalChallenge(r.ID); err == nil {
			t.Fatal("revoked session resurrected after deadline", action)
		}
	}
}

func TestOwnerCanRevokeDormantSessionAndOldChallengesCannotReviveIt(t *testing.T) {
	m, r, _, key, _ := expiredGrant(t, "read")
	c := renewalChallenge(t, m, r.ID)
	expired, _ := m.SessionStatus(r.ID)
	proof, _ := RevokeProof(expired, prove)
	if err := m.Revoke(r.ID, proof); err != nil {
		t.Fatal("owner could not revoke expired session", err)
	}
	if _, _, err := m.Renew(c.ID, ed25519.Sign(key, RenewalProofMessage(*c))); err == nil {
		t.Fatal("challenge survived owner revocation")
	}
	if _, err := m.DelegationChallenge(r.ID, grantedOperation(r, "list"), time.Minute); !errors.Is(err, ErrDenied) {
		t.Fatal("revoked record treated as expiry", err)
	}
}

func TestRenewalDoesNotRevivePreviousChildrenOrTheirProofs(t *testing.T) {
	m, r, _, key := grantFixture(t, "full")
	now := time.Now()
	m.now = func() time.Time { return now }
	approve(t, m, r)
	oldChild, oldTicket := delegated(t, m, r, key, "exec")
	oldContext, _, cancel, err := m.Start(context.Background(), oldChild.ID, oldTicket)
	if err != nil {
		t.Fatal(err)
	}
	defer cancel()
	prepared, err := m.DelegationChallenge(r.ID, grantedOperation(r, "list"), time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	now = now.Add(16 * time.Minute)
	m.Sweep()
	if oldContext.Err() == nil {
		t.Fatal("expired parent left child running")
	}
	c := renewalChallenge(t, m, r.ID)
	renewed, _, err := m.Renew(c.ID, ed25519.Sign(key, RenewalProofMessage(*c)))
	if err != nil {
		t.Fatal(err)
	}
	approve(t, m, renewed)
	if _, _, _, err := m.Start(context.Background(), oldChild.ID, oldTicket); err == nil {
		t.Fatal("old child ticket revived after renewal")
	}
	if _, _, err := m.MintDelegated(prepared.ID, ed25519.Sign(key, DelegationProofMessage(prepared))); err == nil {
		t.Fatal("old delegation challenge survived generation change")
	}
}

func TestIgnoredRenewalCanBeRequestedAgainOnlyOnNewUse(t *testing.T) {
	m, r, _, key, now := expiredGrant(t, "read")
	c := renewalChallenge(t, m, r.ID)
	pending, _, err := m.Renew(c.ID, ed25519.Sign(key, RenewalProofMessage(*c)))
	if err != nil {
		t.Fatal(err)
	}
	*now = now.Add(6 * time.Minute)
	m.Sweep()
	if len(m.List()) != 0 {
		t.Fatal("timer automatically queued another approval")
	}
	newChallenge := renewalChallenge(t, m, r.ID)
	next, created, err := m.Renew(newChallenge.ID, ed25519.Sign(key, RenewalProofMessage(*newChallenge)))
	if err != nil || !created || next.Nonce == pending.Nonce || next.Operation != pending.Operation {
		t.Fatal("new actual use could not ask again", err)
	}
}

func TestExpiredRenewalMetadataIsBounded(t *testing.T) {
	m := New(prove)
	now := m.now()
	for i := 0; i < 270; i++ {
		id := fmt.Sprint(i)
		m.dormant[id] = &entry{request: Request{ID: id}, dormantAt: now.Add(time.Duration(i) * time.Second)}
	}
	m.Sweep()
	if len(m.dormant) != 256 || m.dormant["0"] != nil {
		t.Fatal("expired metadata unbounded or newest evicted")
	}
	if _, _, _, err := m.Start(context.Background(), "269", "none"); err == nil {
		t.Fatal("dormant metadata became an execution ticket")
	}
}
