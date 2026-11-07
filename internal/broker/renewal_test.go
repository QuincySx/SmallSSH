package broker_test

import (
	"bytes"
	"context"
	"encoding/json"
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
	h.offset.Store(int64(61 * time.Second))
	h.s.Approvals.Sweep()
	return r, d
}

func TestOwnerChosenNoIdleLifetimeRenewsAndRunsThroughTheClient(t *testing.T) {
	h := newRenewalHarness(t)
	original, d, err := h.c.RequestSession(h.ctx, "fixture", "test AI", "full", time.Hour, 15*time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()
	if err := h.c.DecideReviewedWithLifetime(h.ctx, original, true, false, approval.MaxGrantLifetime, approval.MaxGrantLifetime, h.s.Vault); err != nil {
		t.Fatal(err)
	}
	h.offset.Store(int64(24*time.Hour + time.Second))
	h.s.Approvals.Sweep()
	notices := make(chan approval.Request, 1)
	type result struct {
		request approval.Request
		ticket  string
		err     error
	}
	results := make(chan result, 1)
	c := h.c
	c.OnSessionRenewal = func(r approval.Request) { notices <- r }
	go func() {
		r, ticket, err := c.RequestDelegated(h.ctx, approval.Operation{Host: "fixture", Client: "test AI", Kind: "exec", Command: "no-idle-renewal"}, time.Minute, d)
		results <- result{r, ticket, err}
	}()
	var renewed approval.Request
	select {
	case renewed = <-notices:
	case got := <-results:
		t.Fatal("client rejected an unchanged no-idle scope instead of asking for renewal", got.err)
	case <-h.ctx.Done():
		t.Fatal("client did not request renewal")
	}
	if renewed.ID != original.ID || !renewed.Renewal || renewed.Operation.Grant.IdleSeconds != approval.MaxGrantLifetime || h.commands.Load() != 0 {
		t.Fatal("renewal changed scope or executed before owner approval", renewed)
	}
	if err := h.c.DecideReviewedWithLifetime(h.ctx, renewed, true, false, 6*3600, 6*3600, h.s.Vault); err != nil {
		t.Fatal(err)
	}
	var got result
	select {
	case got = <-results:
	case <-h.ctx.Done():
		t.Fatal("approved renewal did not resume the waiting operation")
	}
	if got.err != nil || got.request.ParentID != original.ID {
		t.Fatal("renewal did not produce an authorized child", got.err, got.request)
	}
	stream, err := c.Start(h.ctx, got.request.ID, got.ticket, 0, 0)
	if err != nil {
		t.Fatal(err)
	}
	defer stream.Close()
	var out bytes.Buffer
	code, _, err := stream.Copy(&out, &out)
	if err != nil || code != 0 || out.String() != "no-idle-renewal" || h.commands.Load() != 1 {
		t.Fatal("renewed command did not execute exactly once", err, code, out.String(), h.commands.Load())
	}
}

func TestAutomaticRenewalCoalescesParallelCallsOnOneLiveSession(t *testing.T) {
	h := newRenewalHarness(t)
	original, delegation := h.grant(t, "full")
	file := filepath.Join(h.dir, "client-sessions", "client.json")
	var err error
	const workers = 6
	notices := make(chan approval.Request, workers)
	results := make(chan error, workers)
	var wg sync.WaitGroup
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			d := delegation
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
	if _, err := os.Stat(file); !os.IsNotExist(err) {
		t.Fatal("persisted a session key", err)
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

func TestBuiltCLIRejectsSavedSessionAfterProcessRestart(t *testing.T) {
	binary := os.Getenv("SSH_CLIENT_CLI_QA_BINARY")
	if binary == "" {
		t.Skip("opt-in test of the newly built CLI executable")
	}
	if !filepath.IsAbs(binary) {
		t.Fatal("absolute CLI binary required")
	}
	h := newRenewalHarness(t)
	_, _ = h.grant(t, "full")
	command := exec.CommandContext(h.ctx, binary, "--data-dir", h.dir, "--broker-key", h.s.PublicKeyPin(), "--session", "client.json", "exec", "fixture", "must-not-run")
	var out, stderr bytes.Buffer
	command.Stdout, command.Stderr = &out, &stderr
	if err := command.Run(); err == nil || h.commands.Load() != 0 || !strings.Contains(stderr.String(), "session serve") {
		t.Fatal("CLI reused another process session", err, out.String(), stderr.String())
	}
}

func TestBuiltCLIStdioSessionRequiresConsentAndEndsAtProcessExit(t *testing.T) {
	binary := os.Getenv("SSH_CLIENT_CLI_QA_BINARY")
	if binary == "" {
		t.Skip("opt-in built CLI process-boundary test")
	}
	h := newRenewalHarness(t)
	command := exec.CommandContext(h.ctx, binary, "--data-dir", h.dir, "--broker-key", h.s.PublicKeyPin(), "session", "serve", "fixture", "full", "2m", "1m")
	command.Stdin = strings.NewReader("{\"id\":\"native\",\"operation\":{\"kind\":\"exec\",\"command\":\"native-stdio\"}}\n")
	var out, stderr bytes.Buffer
	command.Stdout, command.Stderr = &out, &stderr
	if err := command.Start(); err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() { done <- command.Wait() }()
	tick := time.NewTicker(20 * time.Millisecond)
	defer tick.Stop()
	var parent approval.Request
waiting:
	for {
		select {
		case err := <-done:
			t.Fatal("CLI ended before consent", err, stderr.String())
		case <-h.ctx.Done():
			t.Fatal("CLI never requested consent")
		case <-tick.C:
			page, err := h.c.Call(h.ctx, broker.Message{Action: "pending"})
			if err != nil {
				t.Fatal(err)
			}
			for _, r := range page.Requests {
				if r.Operation.Kind == "grant" && r.Status == "pending" {
					parent = r
					break waiting
				}
			}
		}
	}
	if h.commands.Load() != 0 || parent.Peer.PID != command.Process.Pid || parent.Peer.Instance == "" {
		t.Fatal("CLI bypassed consent or hid its process identity")
	}
	if err := h.c.DecideReviewedWithOwner(h.ctx, parent, true, false, h.s.Vault); err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err, stderr.String())
		}
	case <-h.ctx.Done():
		t.Fatal("approved CLI never finished")
	}
	var result struct {
		ID     string
		Stdout []byte
		Error  string
	}
	if err := json.Unmarshal(out.Bytes(), &result); err != nil || result.ID != "native" || string(result.Stdout) != "native-stdio" || result.Error != "" || h.commands.Load() != 1 {
		t.Fatal("native CLI did not run exactly once", err, out.String(), stderr.String())
	}
	waitRevoked(t, h.c, h.ctx, parent.ID)
}
