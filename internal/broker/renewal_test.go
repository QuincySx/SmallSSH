package broker_test

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"sshclient/internal/approval"
	"sshclient/internal/broker"
	"sshclient/internal/client"
	"sshclient/internal/localipc"
	"sshclient/internal/testserver"
	"sshclient/internal/vault"
)

type renewalHarness struct {
	c        client.Client
	s        *broker.Server
	offset   atomic.Int64
	commands atomic.Int32
	dir      string
	ctx      context.Context
	base     time.Time
}

func newRenewalHarness(t *testing.T) *renewalHarness {
	t.Helper()
	dir, err := os.MkdirTemp("", "renewal-broker-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(dir) })
	if err = localipc.Prepare(dir); err != nil {
		t.Fatal(err)
	}
	h := &renewalHarness{dir: dir, base: time.Now()}
	fixture := testserver.NewWithExec(t, func(cmd string) (string, string, int) { h.commands.Add(1); return cmd, "", 0 })
	v, err := vault.New(pass)
	if err != nil {
		t.Fatal(err)
	}
	if err = v.Put(fixture.Profile()); err != nil {
		t.Fatal(err)
	}
	if err = vault.Save(broker.VaultPath(dir), v, true); err != nil {
		t.Fatal(err)
	}
	h.s, err = broker.OpenSessionWithClockForTest(dir, v, func() time.Time { return h.base.Add(time.Duration(h.offset.Load())) })
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(h.s.Close)
	// The server and context both shut down even after an assertion fails.
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	h.ctx = ctx
	t.Cleanup(cancel)
	h.c = client.Client{Dir: dir, Owner: h.s.Vault}
	return h
}

func (h *renewalHarness) grant(t *testing.T, permission string) (approval.Request, *client.Delegation) {
	t.Helper()
	r, d, err := h.c.RequestSession(h.ctx, "fixture", "test AI", permission, time.Hour, time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(d.Close)
	if err = h.c.DecideReviewedWithOwner(h.ctx, r, true, false, h.s.Vault); err != nil {
		t.Fatal(err)
	}
	if err = d.Save("client.json"); err != nil {
		t.Fatal(err)
	}
	h.offset.Store(int64(61 * time.Second))
	h.s.Approvals.Sweep()
	return r, d
}

func TestAutomaticRenewalCoalescesParallelSSHCallsAndPreservesClientFile(t *testing.T) {
	h := newRenewalHarness(t)
	original, _ := h.grant(t, "full")
	file := filepath.Join(h.dir, "client-sessions", "client.json")
	before, err := os.ReadFile(file)
	if err != nil {
		t.Fatal(err)
	}
	const workers = 6
	notices := make(chan approval.Request, workers)
	results := make(chan error, workers)
	var wg sync.WaitGroup
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			d, err := client.ReadDelegation("client.json", h.dir)
			if err != nil {
				results <- err
				return
			}
			defer d.Close()
			c := client.Client{Dir: h.dir, Delegation: d, OnSessionRenewal: func(r approval.Request) { notices <- r }}
			command := fmt.Sprintf("command-%d", i)
			r, ticket, err := c.Request(h.ctx, approval.Operation{Host: "fixture", Client: "test AI", Kind: "exec", Command: command}, time.Minute)
			if err != nil {
				results <- err
				return
			}
			stream, err := c.Start(h.ctx, r.ID, ticket, 0, 0)
			if err != nil {
				results <- err
				return
			}
			defer stream.Close()
			var out bytes.Buffer
			code, _, err := stream.Copy(&out, &out)
			if err == nil && (code != 0 || out.String() != command) {
				err = fmt.Errorf("unexpected SSH result: %d %q", code, out.String())
			}
			results <- err
		}(i)
	}
	var renewed approval.Request
	for i := 0; i < workers; i++ {
		select {
		case r := <-notices:
			if renewed.ID == "" {
				renewed = r
			}
			if r.ID != original.ID || r.Nonce != renewed.Nonce || r.Status != "pending" || !r.Renewal || r.Operation != original.Operation {
				t.Fatal("parallel callers changed renewal scope or created distinct requests")
			}
		case err := <-results:
			t.Fatal("operation did not wait for consent", err)
		case <-h.ctx.Done():
			t.Fatal("renewal not automatically requested")
		}
	}
	if h.commands.Load() != 0 {
		t.Fatal("SSH command ran before renewed consent")
	}
	list, err := h.c.Call(h.ctx, broker.Message{Action: "pending"})
	if err != nil || len(list.Requests) != 1 {
		t.Fatal("more than one pending renewal", err, len(list.Requests))
	}
	if err = h.c.DecideReviewedWithOwner(h.ctx, original, true, false, h.s.Vault); err == nil {
		t.Fatal("old displayed approval authorized new generation")
	}
	if err = h.c.DecideReviewedWithOwner(h.ctx, renewed, true, false, h.s.Vault); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < workers; i++ {
		select {
		case err := <-results:
			if err != nil {
				t.Fatal(err)
			}
		case <-h.ctx.Done():
			t.Fatal("approved operation did not resume")
		}
	}
	wg.Wait()
	if h.commands.Load() != workers {
		t.Fatal("command replayed or failed to run exactly once", h.commands.Load())
	}
	after, err := os.ReadFile(file)
	if err != nil || !bytes.Equal(before, after) {
		t.Fatal("renewal overwrote client key file", err)
	}
	audit, err := os.ReadFile(filepath.Join(h.dir, "audit.jsonl"))
	if err != nil || bytes.Count(audit, []byte(`"event":"renewalRequested"`)) != 1 || !bytes.Contains(audit, []byte(`"generation"`)) {
		t.Fatal("renewal audit omitted one coalesced generation", err)
	}
}

func TestAutomaticRenewalDenialDoesNotResubmitOrRunAnything(t *testing.T) {
	h := newRenewalHarness(t)
	_, d := h.grant(t, "full")
	notices := make(chan approval.Request, 1)
	done := make(chan error, 1)
	c := client.Client{Dir: h.dir, Delegation: d, OnSessionRenewal: func(r approval.Request) { notices <- r }}
	op := approval.Operation{Host: "fixture", Client: "test AI", Kind: "exec", Command: "must-not-run"}
	go func() { _, _, err := c.Request(h.ctx, op, time.Minute); done <- err }()
	var r approval.Request
	select {
	case r = <-notices:
	case <-h.ctx.Done():
		t.Fatal("no renewal request")
	}
	if err := h.c.DecideReviewedWithOwner(h.ctx, r, false, false, h.s.Vault); err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-done:
		if err == nil {
			t.Fatal("denied renewal succeeded")
		}
	case <-h.ctx.Done():
		t.Fatal("denied operation kept waiting")
	}
	for range 3 {
		if _, _, err := c.Request(h.ctx, op, time.Minute); err == nil {
			t.Fatal("denied grant reused")
		}
	}
	if len(notices) != 0 || h.commands.Load() != 0 {
		t.Fatal("denial triggered another approval or command")
	}
}

func TestExpiredReadScopeDoesNotAskToUpgradeForWrites(t *testing.T) {
	h := newRenewalHarness(t)
	_, d := h.grant(t, "read")
	var notices atomic.Int32
	c := client.Client{Dir: h.dir, Delegation: d, OnSessionRenewal: func(approval.Request) { notices.Add(1) }}
	if _, _, err := c.Request(h.ctx, approval.Operation{Host: "fixture", Client: "test AI", Kind: "exec", Command: "forbidden"}, time.Minute); err == nil {
		t.Fatal("expired read grant upgraded")
	}
	list, err := h.c.Call(h.ctx, broker.Message{Action: "pending"})
	if err != nil || len(list.Requests) != 0 || notices.Load() != 0 {
		t.Fatal("write violation queued a renewal", err)
	}
	if _, err := h.c.Call(h.ctx, broker.Message{Action: "withdraw", ID: d.ParentID, Ticket: d.Ticket}); err != nil {
		t.Fatal("could not revoke expired capability", err)
	}
	if _, _, err := c.Request(h.ctx, approval.Operation{Host: "fixture", Client: "test AI", Kind: "list", Path: "/"}, time.Minute); err == nil {
		t.Fatal("withdrawn expired session requested again")
	}
	if notices.Load() != 0 {
		t.Fatal("withdrawal caused a renewal prompt")
	}
}

func TestBuiltCLIAutomaticallyRenewsBeforeRunningOnLoopbackSSH(t *testing.T) {
	binary := os.Getenv("SSH_CLIENT_CLI_QA_BINARY")
	if binary == "" {
		t.Skip("opt-in test of the newly built CLI executable")
	}
	if !filepath.IsAbs(binary) {
		t.Fatal("absolute CLI binary required")
	}
	h := newRenewalHarness(t)
	_, _ = h.grant(t, "full")
	command := exec.CommandContext(h.ctx, binary, "--data-dir", h.dir, "--client", "test AI", "exec", "fixture", "process-renewal")
	for _, variable := range os.Environ() {
		if !strings.HasPrefix(variable, "SSH_CLIENT_SESSION=") {
			command.Env = append(command.Env, variable)
		}
	}
	command.Env = append(command.Env, "SSH_CLIENT_SESSION=client.json")
	var out, stderr bytes.Buffer
	command.Stdout = &out
	command.Stderr = &stderr
	done := make(chan error, 1)
	if err := command.Start(); err != nil {
		t.Fatal(err)
	}
	go func() { done <- command.Wait() }()
	tick := time.NewTicker(20 * time.Millisecond)
	defer tick.Stop()
	var pending approval.Request
waiting:
	for {
		select {
		case err := <-done:
			t.Fatal("CLI finished before requesting consent", err, stderr.String())
		case <-h.ctx.Done():
			t.Fatal("CLI renewal did not arrive")
		case <-tick.C:
			list, err := h.c.Call(h.ctx, broker.Message{Action: "pending"})
			if err != nil {
				t.Fatal(err)
			}
			for _, r := range list.Requests {
				if r.Renewal && r.Status == "pending" {
					pending = r
					break waiting
				}
			}
		}
	}
	if h.commands.Load() != 0 {
		t.Fatal("CLI ran while consent was pending")
	}
	if err := h.c.DecideReviewedWithOwner(h.ctx, pending, true, false, h.s.Vault); err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-done:
		if err != nil || out.String() != "process-renewal" || h.commands.Load() != 1 || strings.Count(stderr.String(), "等待确认续期") != 1 {
			t.Fatal("CLI did not resume once after renewal", err, out.String(), stderr.String(), h.commands.Load())
		}
	case <-h.ctx.Done():
		t.Fatal("approved CLI did not resume")
	}
}
