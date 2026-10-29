package approval

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func prove(b []byte) ([]byte, error) {
	h := hmac.New(sha256.New, []byte("owner test secret"))
	h.Write(b)
	return h.Sum(nil), nil
}
func setup(t *testing.T) (*Manager, Request, string) {
	t.Helper()
	m := New(prove)
	r, ticket, e := m.Create(Operation{Host: "server", Kind: "exec", Command: "echo hello", Client: "test"}, time.Minute)
	if e != nil {
		t.Fatal(e)
	}
	return m, r, ticket
}
func approve(t *testing.T, m *Manager, r Request) {
	t.Helper()
	proof, _ := DecisionProof(r, true, prove)
	if e := m.Decide(r.ID, true, proof); e != nil {
		t.Fatal(e)
	}
}
func TestProofBindsEveryFieldAndDecision(t *testing.T) {
	m, r, _ := setup(t)
	if e := m.Decide(r.ID, true, []byte("yes")); e == nil {
		t.Fatal("forged proof")
	}
	fake := r
	fake.Operation.Command = "rm anything"
	p, _ := DecisionProof(fake, true, prove)
	if e := m.Decide(r.ID, true, p); e == nil {
		t.Fatal("modified command")
	}
	p, _ = DecisionProof(r, false, prove)
	if e := m.Decide(r.ID, true, p); e == nil {
		t.Fatal("decision substitution")
	}
	approve(t, m, r)
}
func TestTicketConcurrentOneUseAndRevoke(t *testing.T) {
	m, r, ticket := setup(t)
	if _, _, _, e := m.Start(context.Background(), r.ID, ticket); e == nil {
		t.Fatal("unapproved start")
	}
	approve(t, m, r)
	var successes atomic.Int32
	var wg sync.WaitGroup
	contexts := make(chan context.Context, 20)
	for range 20 {
		wg.Go(func() {
			ctx, _, _, e := m.Start(context.Background(), r.ID, ticket)
			if e == nil {
				successes.Add(1)
				contexts <- ctx
			}
		})
	}
	wg.Wait()
	if successes.Load() != 1 {
		t.Fatal(successes.Load())
	}
	p, _ := RevokeProof(r, prove)
	if e := m.Revoke(r.ID, p); e != nil {
		t.Fatal(e)
	}
	select {
	case <-(<-contexts).Done():
	case <-time.After(time.Second):
		t.Fatal("revoke did not cancel")
	}
	if _, _, _, e := m.Start(context.Background(), r.ID, ticket); e == nil {
		t.Fatal("replayed ticket")
	}
}
func TestExpiryLockAndFinish(t *testing.T) {
	m, r, ticket := setup(t)
	approve(t, m, r)
	ctx, _, _, e := m.Start(context.Background(), r.ID, ticket)
	if e != nil {
		t.Fatal(e)
	}
	m.Lock()
	if ctx.Err() == nil {
		t.Fatal("lock did not cancel")
	}
	if _, e = m.Get(r.ID); e == nil {
		t.Fatal("request survived lock")
	}
	m, r, ticket = setup(t)
	approve(t, m, r)
	ctx, _, _, _ = m.Start(context.Background(), r.ID, ticket)
	m.Finish(r.ID)
	got, _ := m.Get(r.ID)
	if got.Status != "finished" || ctx.Err() == nil {
		t.Fatal("finish did not close operation")
	}
	m, r, ticket = setup(t)
	approve(t, m, r)
	m.now = func() time.Time { return r.Expires }
	if _, _, _, e = m.Start(context.Background(), r.ID, ticket); e == nil {
		t.Fatal("expired start")
	}
}
func TestRejectUnknownUnboundedAndIncomplete(t *testing.T) {
	m := New(prove)
	for _, o := range []Operation{{Host: "x", Client: "x", Kind: "unknown"}, {Host: "x", Client: "x", Kind: "put", Path: "/x"}, {Host: "x", Kind: "shell"}} {
		if _, _, e := m.Create(o, time.Minute); e == nil {
			t.Fatal("accepted malformed request")
		}
	}
	if _, _, e := m.Create(Operation{Host: "x", Client: "x", Kind: "shell"}, 16*time.Minute); e == nil {
		t.Fatal("unbounded TTL")
	}
}

func TestAutomaticServerStatusIsExplicitAndBoundToShellApproval(t *testing.T) {
	m := New(prove)
	for _, op := range []Operation{
		{Host: "x", Client: "x", Kind: "monitor", ServerStatus: true},
		{Host: "x", Client: "x", Kind: "exec", Command: "uname", ServerStatus: true},
		{Host: "x", Client: "x", Kind: "shell", Command: "arbitrary", ServerStatus: true},
	} {
		if _, _, err := m.Create(op, time.Minute); err == nil {
			t.Fatal("invalid automatic status scope accepted", op)
		}
	}
	r, _, err := m.Create(Operation{Host: "x", Client: "x", Kind: "shell", ServerStatus: true}, time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	tampered := r
	tampered.Operation.ServerStatus = false
	proof, _ := DecisionProof(tampered, true, prove)
	if err = m.Decide(r.ID, true, proof); err == nil {
		t.Fatal("status scope changed without invalidating owner proof")
	}
	approve(t, m, r)
}

func TestTicketHolderCanOnlyWithdrawAndCannotReplay(t *testing.T) {
	m, r, ticket := setup(t)
	if e := m.Withdraw(r.ID, "forged"); e == nil {
		t.Fatal("unauthorized cancellation")
	}
	approve(t, m, r)
	ctx, _, cancel, e := m.Start(context.Background(), r.ID, ticket)
	if e != nil {
		t.Fatal(e)
	}
	defer cancel()
	if e = m.Withdraw(r.ID, ticket); e != nil {
		t.Fatal(e)
	}
	if ctx.Err() == nil {
		t.Fatal("withdrawal did not stop work")
	}
	if _, _, _, e = m.Start(context.Background(), r.ID, ticket); e == nil {
		t.Fatal("withdrawn ticket reused")
	}
	proof, _ := DecisionProof(r, true, prove)
	if e = m.Decide(r.ID, true, proof); e == nil {
		t.Fatal("withdrawn request reapproved")
	}
}

func TestCompletedHistoryDoesNotExhaustLiveAuthoritySlots(t *testing.T) {
	m := New(prove)
	live, ticket, err := m.Create(Operation{Host: "host", Kind: "shell", Client: "external"}, time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 300; i++ {
		r, key, err := m.Create(Operation{Host: "host", Kind: "list", Path: "/tmp", Client: "owner"}, time.Minute)
		if err != nil {
			t.Fatal("completed history exhausted slots", i, err)
		}
		approve(t, m, r)
		_, _, cancel, err := m.Start(context.Background(), r.ID, key)
		if err != nil {
			t.Fatal(err)
		}
		m.Finish(r.ID)
		cancel()
	}
	still, err := m.Get(live.ID)
	if err != nil || still.Status != "pending" {
		t.Fatal("live request evicted", err)
	}
	if _, _, _, err = m.Start(context.Background(), live.ID, ticket); err == nil {
		t.Fatal("external request auto-approved")
	}
	if len(m.List()) > 128 {
		t.Fatal("history is not bounded")
	}
}

func TestOwnerShellLifetimeIsSeparateFromExternalGrants(t *testing.T) {
	m := New(prove)
	now := time.Now()
	m.now = func() time.Time { return now }
	op := Operation{Host: "server", Kind: "shell", Client: "desktop spoof"}
	external, externalTicket, err := m.Create(op, time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	tampered := external
	tampered.OwnerIssued = true
	tampered.OwnerSession = true
	forgedProof, _ := DecisionProof(tampered, true, prove)
	if err = m.Decide(external.ID, true, forgedProof); err == nil {
		t.Fatal("scope substitution was accepted")
	}
	if _, _, err = m.Create(Operation{Host: "server", Kind: "shell", Client: "AI", Command: "cat harmless"}, time.Minute); err == nil {
		t.Fatal("ignored command field could disguise full shell")
	}
	if external.OwnerIssued || external.OwnerSession {
		t.Fatal("caller label gained owner scope")
	}
	approve(t, m, external)
	externalCtx, _, externalStop, err := m.Start(context.Background(), external.ID, externalTicket)
	if err != nil {
		t.Fatal(err)
	}
	defer externalStop()
	owner, ticket, err := m.CreateOwner(op)
	if err != nil {
		t.Fatal(err)
	}
	approve(t, m, owner)
	ownerCtx, _, ownerStop, err := m.Start(context.Background(), owner.ID, ticket)
	if err != nil {
		t.Fatal(err)
	}
	defer ownerStop()
	if _, deadline := ownerCtx.Deadline(); deadline {
		t.Fatal("human shell still has a timer")
	}
	bounded, _, err := m.CreateOwner(Operation{Host: "server", Kind: "list", Path: "/tmp", Client: "owner"})
	if err != nil || !bounded.OwnerIssued || bounded.OwnerSession {
		t.Fatal("file operation gained indefinite shell scope", err)
	}
	now = now.Add(16 * time.Minute)
	m.List()
	select {
	case <-externalCtx.Done():
	default:
		t.Fatal("external grant did not expire")
	}
	select {
	case <-ownerCtx.Done():
		t.Fatal("human shell unexpectedly expired")
	default:
	}
	if _, err = m.Get(owner.ID); err != nil {
		t.Fatal("active owner session pruned", err)
	}
	if _, err = m.Get(bounded.ID); err == nil {
		t.Fatal("unused owner request never expires")
	}
	m.Lock()
	select {
	case <-ownerCtx.Done():
	default:
		t.Fatal("lock did not cancel owner session")
	}
}

// One caller cannot occupy the approval queue: its own share is bounded, and
// another caller is unaffected.
func TestOneCallerCannotFillTheApprovalQueue(t *testing.T) {
	m := New(prove)
	op := Operation{Host: "fixture", Kind: "exec", Command: "uptime", Client: "noisy"}
	for i := 0; i < maxPendingPerClient; i++ {
		if _, _, err := m.Create(op, time.Minute); err != nil {
			t.Fatalf("request %d refused early: %v", i, err)
		}
	}
	if _, _, err := m.Create(op, time.Minute); err == nil {
		t.Fatal("the caller's own share was unbounded")
	}
	other := op
	other.Client = "quiet"
	if _, _, err := m.Create(other, time.Minute); err != nil {
		t.Fatalf("another caller was blocked: %v", err)
	}
}

// A caller identity the broker established cannot be bought back by renaming the
// caller: the same process gets sixteen slots however it labels itself.
func TestBucketQuotaIgnoresTheReportedLabel(t *testing.T) {
	m := New(prove)
	op := Operation{Host: "fixture", Kind: "exec", Command: "uptime", Client: "name-1"}
	for i := 0; i < maxPendingPerClient; i++ {
		op.Client = fmt.Sprintf("name-%d", i)
		if _, _, err := m.CreateFrom(op, time.Minute, "pid:1"); err != nil {
			t.Fatalf("request %d refused early: %v", i, err)
		}
	}
	op.Client = "name-retry"
	if _, _, err := m.CreateFrom(op, time.Minute, "pid:1"); err == nil {
		t.Fatal("renaming the caller bought another slot")
	}
	// Another process still gets its own share.
	if _, _, err := m.CreateFrom(op, time.Minute, "pid:2"); err != nil {
		t.Fatalf("a second process was blocked: %v", err)
	}
}
