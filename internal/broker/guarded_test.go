package broker_test

import (
	"errors"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"

	"sshclient/internal/accesspolicy"
	"sshclient/internal/approval"
	"sshclient/internal/auditlog"
	"sshclient/internal/broker"
	"sshclient/internal/client"
	"sshclient/internal/testserver"
)

func TestGuardedMediumFileReviewOptOutRequiresOwnerProof(t *testing.T) {
	f := testserver.New(t)
	p := f.Profile()
	p.Policy = accesspolicy.Policy{Unrestricted: true}
	s, owner, ctx := policyHarness(t, p)
	caller := client.Client{Dir: owner.Dir, BrokerKey: s.PublicKeyPin()}
	r, d, err := caller.RequestSession(ctx, p.ID, "guarded SDK", "guarded", 15*time.Minute, 5*time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()
	forged := r.Operation
	forged.Grant.SkipMediumReview = true
	if _, _, err := caller.Request(ctx, forged, time.Minute); err == nil {
		t.Fatal("caller supplied owner preference")
	}
	proof, err := approval.DecisionProof(r, true, s.Vault.Proof)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := owner.Call(ctx, broker.Message{Action: "decide", ID: r.ID, Allow: true, SkipMediumReview: true, Proof: proof}); err == nil {
		t.Fatal("switch changed without matching proof")
	}
	if err := owner.DecideReviewedWithTerms(ctx, r, true, false, 0, 0, true, s.Vault); err != nil {
		t.Fatal(err)
	}
	caller.Delegation = d
	dir, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(dir, "new-folder")
	op := approval.Operation{Host: p.ID, Kind: "mkdir", Client: "guarded SDK", Path: testserver.RemotePath(target)}
	req, ticket, err := caller.Request(ctx, op, time.Minute)
	if err != nil || req.Status != "approved" || req.ParentID != r.ID {
		t.Fatal(req, err)
	}
	stream, err := caller.Start(ctx, req.ID, ticket, 0, 0)
	if err != nil {
		t.Fatal(err)
	}
	code, _, err := stream.Copy(io.Discard, io.Discard)
	stream.Close()
	if err != nil || code != 0 {
		t.Fatal(code, err)
	}
	if info, err := os.Stat(target); err != nil || !info.IsDir() {
		t.Fatal("approved mutation did not run", err)
	}
	if runtime.GOOS != "windows" {
		alias := filepath.Join(dir, "alias")
		if err := os.Symlink(dir, alias); err != nil {
			t.Fatal(err)
		}
		op.Path = testserver.RemotePath(filepath.Join(alias, "through-alias"))
		req, ticket, err := caller.Request(ctx, op, time.Minute)
		if err != nil {
			t.Fatal(err)
		}
		if stream, err = caller.Start(ctx, req.ID, ticket, 0, 0); err == nil {
			stream.Close()
			t.Fatal("unreviewed mutation followed an alias")
		}
	}
	op.Path = "/etc/definitely-a-fixture"
	if _, _, err := caller.Request(ctx, op, time.Minute); !errors.Is(err, client.ErrAutomationPolicy) {
		t.Fatal("critical mutation escaped review", err)
	}
	op.Kind, op.Path, op.Command = "exec", "", "systemctl restart nginx"
	req, _, err = caller.Request(ctx, op, time.Minute)
	if err != nil || req.Status != "pending" {
		t.Fatal("unknown command lost review", req, err)
	}
	page, err := owner.History(ctx, auditlog.Query{Limit: 100}, s.Vault)
	if err != nil {
		t.Fatal(err)
	}
	approved, delegated := false, false
	for _, event := range page.Events {
		approved = approved || event.RequestID == r.ID && event.Event == "approved" && event.SkipMediumReview
		delegated = delegated || event.ParentID == r.ID && event.Event == "delegated" && event.Path == testserver.RemotePath(target) &&
			event.ParentGeneration == r.Generation() && event.MediumReview != nil && !*event.MediumReview
	}
	if !approved || !delegated {
		t.Fatal("opt-out or delegated operation missing from audit")
	}
}

func TestGuardedSDKSessionChecksEveryOperationAndKeepsOneAuthority(t *testing.T) {
	var mu sync.Mutex
	var executed []string
	f := testserver.NewWithExec(t, func(command string) (string, string, int) {
		mu.Lock()
		executed = append(executed, command)
		mu.Unlock()
		return "ran:" + command, "", 0
	})
	p := f.Profile()
	p.Policy = accesspolicy.Policy{Unrestricted: true}
	s, owner, ctx := policyHarness(t, p)
	d := policySession(t, s, owner, ctx, p.ID, "guarded")
	caller := client.Client{Dir: owner.Dir, BrokerKey: s.PublicKeyPin(), Delegation: d}
	execute := func(r approval.Request, ticket, command string) {
		t.Helper()
		stream, err := caller.Start(ctx, r.ID, ticket, 0, 0)
		if err != nil {
			t.Fatal(err)
		}
		var out strings.Builder
		code, _, err := stream.Copy(&out, io.Discard)
		stream.Close()
		if err != nil || code != 0 || out.String() != "ran:"+command {
			t.Fatal(code, out.String(), err)
		}
		if _, err = caller.Start(ctx, r.ID, ticket, 0, 0); err == nil {
			t.Fatal("operation replayed")
		}
	}
	op := approval.Operation{Host: p.ID, Kind: "exec", Client: "guarded SDK", Command: "uname -a"}
	low, ticket, err := caller.Request(ctx, op, time.Minute)
	if err != nil || low.Status != "approved" {
		t.Fatal(low, err)
	}
	execute(low, ticket, op.Command)
	op.Command = "systemctl restart nginx"
	medium, ticket, err := caller.Request(ctx, op, time.Minute)
	if err != nil || medium.Status != "pending" || medium.ParentID != d.ParentID {
		t.Fatal(medium, err)
	}
	if _, err = caller.Start(ctx, medium.ID, ticket, 0, 0); err == nil {
		t.Fatal("unreviewed operation reached SSH")
	}
	duplicate := op
	duplicate.Client = "changed label"
	if _, _, err = caller.Request(ctx, duplicate, time.Minute); !errors.Is(err, approval.ErrReviewPending) {
		t.Fatal("duplicate review not recognized over IPC", err)
	}
	mu.Lock()
	count := len(executed)
	mu.Unlock()
	if count != 1 {
		t.Fatal("pending operation executed", count)
	}
	if err = owner.DecideReviewedWithOwner(ctx, medium, true, false, s.Vault); err != nil {
		t.Fatal(err)
	}
	if err = caller.Wait(ctx, medium.ID, ticket); err != nil {
		t.Fatal(err)
	}
	execute(medium, ticket, op.Command)
	op.Command = "systemctl restart nginx --other-arguments"
	next, nextTicket, err := caller.Request(ctx, op, time.Minute)
	if err != nil || next.Status != "pending" {
		t.Fatal("operation approval widened scope", next, err)
	}
	oldProof, _ := approval.DecisionProof(medium, true, s.Vault.Proof)
	if _, err = owner.Call(ctx, broker.Message{Action: "decide", ID: next.ID, Allow: true, Proof: oldProof}); err == nil {
		t.Fatal("old proof approved changed arguments")
	}
	if err = owner.DecideReviewedWithOwner(ctx, next, false, false, s.Vault); err != nil {
		t.Fatal(err)
	}
	if err = caller.Wait(ctx, next.ID, nextTicket); !errors.Is(err, client.ErrOperationDenied) {
		t.Fatal("operation denial not distinguished from revoked session", err)
	}
	for _, command := range []string{"mkfs.ext4 /dev/sda", "systemctl stop sshd"} {
		op.Command = command
		if _, _, err = caller.Request(ctx, op, time.Minute); !errors.Is(err, client.ErrAutomationPolicy) {
			t.Fatal("high risk bypassed unrestricted server policy", command, err)
		}
	}
	if _, _, err = caller.Request(ctx, approval.Operation{Host: p.ID, Kind: "remove", Client: "SDK", Path: "/etc"}, time.Minute); !errors.Is(err, client.ErrAutomationPolicy) {
		t.Fatal("typed mutation bypassed the high-risk check", err)
	}
	op.Command = "id -un"
	last, lastTicket, err := caller.Request(ctx, op, time.Minute)
	if err != nil || last.Status != "approved" {
		t.Fatal("refusal ended or expanded the session", last, err)
	}
	execute(last, lastTicket, op.Command)
	assertPolicyNoPending(t, owner, ctx)
	page, err := owner.History(ctx, auditlog.Query{Limit: 100}, s.Vault)
	if err != nil {
		t.Fatal(err)
	}
	high, reviewed, delegated := 0, 0, 0
	for _, e := range page.Events {
		if e.Reason == "high_risk_operation" {
			high++
			if e.ParentID != d.ParentID {
				t.Fatal("high-risk audit lost parent", e)
			}
		}
		if e.ParentID == d.ParentID && e.Event == "requested" {
			reviewed++
		}
		if e.ParentID == d.ParentID && e.Event == "delegated" {
			delegated++
		}
	}
	if high != 3 || reviewed != 2 || delegated != 2 {
		t.Fatal("risk decisions not auditable", high, reviewed, delegated)
	}
}

func TestGuardedPendingOperationIsCancelledWithParent(t *testing.T) {
	f := testserver.New(t)
	p := f.Profile()
	p.Policy.Unrestricted = true
	s, owner, ctx := policyHarness(t, p)
	d := policySession(t, s, owner, ctx, p.ID, "guarded")
	caller := client.Client{Dir: owner.Dir, BrokerKey: s.PublicKeyPin(), Delegation: d}
	r, ticket, err := caller.Request(ctx, approval.Operation{Host: p.ID, Kind: "exec", Client: "SDK", Command: "systemctl restart nginx"}, time.Minute)
	if err != nil || r.Status != "pending" {
		t.Fatal(r, err)
	}
	if err = owner.DecideWithOwner(ctx, d.ParentID, false, true, s.Vault); err != nil {
		t.Fatal(err)
	}
	if err = owner.DecideReviewedWithOwner(ctx, r, true, false, s.Vault); err == nil {
		t.Fatal("revoked session's pending operation approved")
	}
	if _, err = caller.Start(ctx, r.ID, ticket, 0, 0); err == nil {
		t.Fatal("revoked pending operation ran")
	}
	if f.Auths.Load() != 0 {
		t.Fatal("pending or revoked operation contacted SSH")
	}
}
