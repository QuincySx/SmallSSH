package approval

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func grantFixture(t *testing.T, permission string) (*Manager, Request, string, ed25519.PrivateKey) {
	t.Helper()
	pub, key, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	m := New(prove)
	r, ticket, err := m.Create(Operation{Host: "server", Endpoint: "user@host:22", HostFingerprint: "SHA256:trusted", Client: "unverified label", Kind: "grant", Grant: GrantScope{PublicKey: base64.RawURLEncoding.EncodeToString(pub), Permission: permission, LifetimeSeconds: 7200, IdleSeconds: 900}}, time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	return m, r, ticket, key
}
func grantedOperation(r Request, kind string) Operation {
	return Operation{Host: r.Operation.Host, Endpoint: r.Operation.Endpoint, HostFingerprint: r.Operation.HostFingerprint, Client: "same label is not authority", Kind: kind, Path: "/example"}
}
func delegated(t *testing.T, m *Manager, parent Request, key ed25519.PrivateKey, kind string) (Request, string) {
	t.Helper()
	op := grantedOperation(parent, kind)
	if kind == "exec" {
		op.Path = ""
		op.Command = "fixture command"
	}
	c, err := m.DelegationChallenge(parent.ID, op, 5*time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	r, ticket, err := m.MintDelegated(c.ID, ed25519.Sign(key, DelegationProofMessage(c)))
	if err != nil {
		t.Fatal(err)
	}
	return r, ticket
}

func TestDelegationRequiresOwnerApprovalAndCorrectClientKey(t *testing.T) {
	m, p, rootTicket, key := grantFixture(t, "full")
	if _, err := m.DelegationChallenge(p.ID, grantedOperation(p, "list"), time.Minute); err == nil {
		t.Fatal("unapproved parent minted a challenge")
	}
	if err := m.Decide(p.ID, true, []byte("self approval")); err == nil {
		t.Fatal("client approved itself")
	}
	approve(t, m, p)
	if _, _, _, err := m.Start(context.Background(), p.ID, rootTicket); err == nil {
		t.Fatal("parent authority ran as an operation")
	}
	c, err := m.DelegationChallenge(p.ID, grantedOperation(p, "list"), time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	_, other, _ := ed25519.GenerateKey(rand.Reader)
	if _, _, err = m.MintDelegated(c.ID, ed25519.Sign(other, DelegationProofMessage(c))); err == nil {
		t.Fatal("another key with the same label used the parent")
	}
	if _, _, err = m.MintDelegated(c.ID, ed25519.Sign(key, DelegationProofMessage(c))); err == nil {
		t.Fatal("failed challenge was reusable")
	}
	r, ticket := delegated(t, m, p, key, "exec")
	if r.ParentID != p.ID || r.OwnerIssued || r.OwnerSession || r.Status != "approved" {
		t.Fatal("child acquired owner authority", r)
	}
	ctx, op, cancel, err := m.Start(context.Background(), r.ID, ticket)
	if err != nil {
		t.Fatal(err)
	}
	defer cancel()
	if op.Command != "fixture command" || ctx.Err() != nil {
		t.Fatal("child operation changed")
	}
	if _, _, _, err = m.Start(context.Background(), r.ID, ticket); err == nil {
		t.Fatal("child ticket replayed")
	}
}

func TestDelegationReadScopeTargetAndTranscriptCannotExpand(t *testing.T) {
	m, p, _, key := grantFixture(t, "read")
	approve(t, m, p)
	for _, kind := range []string{"shell", "exec", "remove", "rename", "mkdir", "chmod", "file-attributes", "put", "replace", "grant"} {
		if _, err := m.DelegationChallenge(p.ID, grantedOperation(p, kind), time.Minute); err == nil {
			t.Fatal("read scope expanded", kind)
		}
	}
	for _, change := range []func(*Operation){func(o *Operation) { o.Host = "another" }, func(o *Operation) { o.Endpoint = "root@elsewhere:22" }, func(o *Operation) { o.HostFingerprint = "SHA256:changed" }, func(o *Operation) { o.Grant = p.Operation.Grant }} {
		op := grantedOperation(p, "list")
		change(&op)
		if _, err := m.DelegationChallenge(p.ID, op, time.Minute); err == nil {
			t.Fatal("target or delegation scope expanded", op)
		}
	}
	for _, change := range []func(*DelegationChallenge){func(c *DelegationChallenge) { c.Operation.Path = "/other" }, func(c *DelegationChallenge) { c.TTLSeconds++ }, func(c *DelegationChallenge) { c.ParentID = "other" }, func(c *DelegationChallenge) { c.Expires = c.Expires.Add(time.Hour) }} {
		c, err := m.DelegationChallenge(p.ID, grantedOperation(p, "list"), time.Minute)
		if err != nil {
			t.Fatal(err)
		}
		changed := c
		change(&changed)
		if _, _, err = m.MintDelegated(c.ID, ed25519.Sign(key, DelegationProofMessage(changed))); err == nil {
			t.Fatal("signed transcript substitution accepted")
		}
	}
	for _, kind := range []string{"list", "file-info", "get", "get-resume", "monitor"} {
		op := grantedOperation(p, kind)
		if kind == "get-resume" {
			op.TransferID = "fixture"
		}
		c, err := m.DelegationChallenge(p.ID, op, time.Minute)
		if err != nil {
			t.Fatal(err)
		}
		if _, _, err = m.MintDelegated(c.ID, ed25519.Sign(key, DelegationProofMessage(c))); err != nil {
			t.Fatal(err)
		}
	}
}

func TestDelegationRevokeWithdrawLockAndHostChangeCancelRunningChildren(t *testing.T) {
	for _, action := range []string{"revoke", "withdraw", "lock", "host"} {
		t.Run(action, func(t *testing.T) {
			m, p, ticket, key := grantFixture(t, "full")
			approve(t, m, p)
			r, childTicket := delegated(t, m, p, key, "exec")
			ctx, _, cancel, err := m.Start(context.Background(), r.ID, childTicket)
			if err != nil {
				t.Fatal(err)
			}
			defer cancel()
			challenge, err := m.DelegationChallenge(p.ID, grantedOperation(p, "list"), time.Minute)
			if err != nil {
				t.Fatal(err)
			}
			switch action {
			case "revoke":
				current, _ := m.Get(p.ID)
				proof, _ := RevokeProof(current, prove)
				err = m.Revoke(p.ID, proof)
			case "withdraw":
				err = m.Withdraw(p.ID, ticket)
			case "lock":
				m.Lock()
			case "host":
				m.InvalidateHost(p.Operation.Host)
			}
			if err != nil || ctx.Err() == nil {
				t.Fatal("parent invalidation did not cancel child", err)
			}
			if _, _, err = m.MintDelegated(challenge.ID, ed25519.Sign(key, DelegationProofMessage(challenge))); err == nil {
				t.Fatal("invalidated parent minted from an old challenge")
			}
		})
	}
}

func TestDelegationIdleAbsoluteAndChallengeExpiryAreIndependent(t *testing.T) {
	for _, action := range []string{"idle", "absolute", "challenge"} {
		t.Run(action, func(t *testing.T) {
			m, p, _, key := grantFixture(t, "full")
			now := time.Now()
			m.now = func() time.Time { return now }
			approve(t, m, p)
			r, ticket := delegated(t, m, p, key, "exec")
			ctx, _, cancel, err := m.Start(context.Background(), r.ID, ticket)
			if err != nil {
				t.Fatal(err)
			}
			defer cancel()
			challenge, err := m.DelegationChallenge(p.ID, grantedOperation(p, "list"), time.Minute)
			if err != nil {
				t.Fatal(err)
			}
			switch action {
			case "idle":
				now = now.Add(15 * time.Minute)
			case "absolute":
				now = now.Add(2 * time.Hour)
			case "challenge":
				now = now.Add(30 * time.Second)
			}
			m.Sweep()
			if action != "challenge" && ctx.Err() == nil {
				t.Fatal("expired parent left child running")
			}
			if _, _, err = m.MintDelegated(challenge.ID, ed25519.Sign(key, DelegationProofMessage(challenge))); err == nil {
				t.Fatal("expired challenge minted authority")
			}
			if action == "challenge" {
				if _, err = m.Get(p.ID); err != nil {
					t.Fatal("challenge expiry killed an otherwise valid parent")
				}
			}
		})
	}
	// Activity refreshes idle time but cannot extend the absolute deadline.
	m, p, _, key := grantFixture(t, "full")
	now := time.Now()
	m.now = func() time.Time { return now }
	approve(t, m, p)
	parent, _ := m.Get(p.ID)
	for range 9 {
		now = now.Add(13 * time.Minute)
		r, _ := delegated(t, m, p, key, "list")
		if r.Expires.After(parent.Expires) {
			t.Fatal("child outlives parent")
		}
	}
	now = parent.Expires
	m.Sweep()
	if _, err := m.Get(p.ID); err == nil {
		t.Fatal("activity extended absolute authorization")
	}
}

func TestDelegationChallengeAndChildAreOneUseUnderConcurrency(t *testing.T) {
	m, p, _, key := grantFixture(t, "full")
	approve(t, m, p)
	c, err := m.DelegationChallenge(p.ID, grantedOperation(p, "list"), time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	sig := ed25519.Sign(key, DelegationProofMessage(c))
	var success atomic.Int32
	var wg sync.WaitGroup
	for range 20 {
		wg.Go(func() {
			if _, _, err := m.MintDelegated(c.ID, sig); err == nil {
				success.Add(1)
			}
		})
	}
	wg.Wait()
	if success.Load() != 1 {
		t.Fatal("challenge consumed more than once", success.Load())
	}
}
