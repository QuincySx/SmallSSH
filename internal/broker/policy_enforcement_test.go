package broker_test

import (
	"context"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"sshclient/internal/accesspolicy"
	"sshclient/internal/approval"
	"sshclient/internal/auditlog"
	"sshclient/internal/broker"
	"sshclient/internal/client"
	"sshclient/internal/localipc"
	"sshclient/internal/testserver"
	"sshclient/internal/vault"
)

func policyHarness(t *testing.T, profiles ...vault.Profile) (*broker.Server, client.Client, context.Context) {
	t.Helper()
	dir, err := os.MkdirTemp("", "ssh-policy-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(dir) })
	if err = localipc.Prepare(dir); err != nil {
		t.Fatal(err)
	}
	v, err := vault.New(pass)
	if err != nil {
		t.Fatal(err)
	}
	for _, p := range profiles {
		if err = v.Put(p); err != nil {
			t.Fatal(err)
		}
	}
	if err = vault.Save(broker.VaultPath(dir), v, true); err != nil {
		t.Fatal(err)
	}
	s, err := broker.OpenSession(dir, v)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(s.Close)
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	t.Cleanup(cancel)
	return s, client.Client{Dir: dir, Owner: s.Vault}, ctx
}

func policySession(t *testing.T, s *broker.Server, cli client.Client, ctx context.Context, host, permission string) *client.Delegation {
	t.Helper()
	r, d, err := cli.RequestSession(ctx, host, "policy test AI", permission, time.Minute, time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	if err = cli.DecideReviewedWithOwner(ctx, r, true, false, s.Vault); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(d.Close)
	return d
}

func assertPolicyNoPending(t *testing.T, cli client.Client, ctx context.Context) {
	t.Helper()
	p, err := cli.Call(ctx, broker.Message{Action: "pending"})
	if err != nil {
		t.Fatal(err)
	}
	for _, r := range p.Requests {
		if r.Status == "pending" {
			t.Fatal("refusal created an approval", r.Operation.Kind)
		}
	}
}

func TestAutomationDisabledNeverCreatesApprovalAndDoesNotBlockOwner(t *testing.T) {
	f := testserver.NewWithExec(t, func(command string) (string, string, int) { return "owner-ok", "", 0 })
	p := f.Profile()
	p.Policy = accesspolicy.Policy{Disabled: true}
	s, cli, ctx := policyHarness(t, p)
	for _, op := range []approval.Operation{
		{Host: p.ID, Kind: "exec", Command: "safe", Client: "desktop"},
		{Host: p.ID, Kind: "monitor", Client: "内部系统采样"},
	} {
		if _, _, err := cli.Request(ctx, op, time.Minute); err == nil {
			t.Fatal("disabled server accepted", op.Kind)
		}
	}
	if _, _, err := cli.RequestSession(ctx, p.ID, "AI", "full", time.Minute, time.Minute); err == nil {
		t.Fatal("disabled server accepted grant")
	}
	assertPolicyNoPending(t, cli, ctx)
	if f.Auths.Load() != 0 {
		t.Fatal("disabled request reached SSH")
	}
	page, err := cli.History(ctx, auditlog.Query{Limit: 100}, s.Vault)
	if err != nil {
		t.Fatal(err)
	}
	denials := 0
	for _, e := range page.Events {
		if e.Reason == "automation_disabled" {
			denials++
			if e.Host != p.ID || e.HostName != p.Name {
				t.Fatal("denial lost target")
			}
		}
	}
	if denials != 3 {
		t.Fatal("missing silent denial history", denials)
	}
	r, ticket, err := cli.RequestWithOwner(ctx, approval.Operation{Host: p.ID, Kind: "exec", Command: "safe", Client: "owner"}, s.Vault)
	if err != nil {
		t.Fatal(err)
	}
	if err = cli.DecideReviewedWithOwner(ctx, r, true, false, s.Vault); err != nil {
		t.Fatal(err)
	}
	stream, err := cli.Start(ctx, r.ID, ticket, 0, 0)
	if err != nil {
		t.Fatal(err)
	}
	defer stream.Close()
	var out strings.Builder
	code, _, err := stream.Copy(&out, io.Discard)
	if err != nil || code != 0 || out.String() != "owner-ok" {
		t.Fatal("owner was blocked", err, out.String())
	}
}

func TestProtectedAutomationRejectsOpaqueAndLexicalWritesBeforeApproval(t *testing.T) {
	f := testserver.New(t)
	p := f.Profile()
	p.Policy = accesspolicy.Policy{ProtectedPaths: []string{"/srv/protected"}}
	s, cli, ctx := policyHarness(t, p)
	for _, op := range []approval.Operation{
		{Kind: "exec", Command: "python harmless"}, {Kind: "shell"},
		{Kind: "remove", Path: "/srv/protected/value"},
		{Kind: "rename", Path: "/srv", Destination: "/safe"},
		{Kind: "rename", Path: "/safe", Destination: "/srv/protected/new"},
		{Kind: "remove", Path: "/safe/../srv/protected/value"},
		{Kind: "chmod", Path: "/safe/value", Mode: 0600},
	} {
		op.Host, op.Client = p.ID, "policy test AI"
		if _, _, err := cli.Request(ctx, op, time.Minute); err == nil {
			t.Fatal("protected operation accepted", op.Kind, op.Path)
		}
	}
	assertPolicyNoPending(t, cli, ctx)
	if f.Auths.Load() != 0 {
		t.Fatal("lexically blocked operation reached SSH")
	}
	d := policySession(t, s, cli, ctx, p.ID, "full")
	if _, _, err := cli.RequestDelegated(ctx, approval.Operation{Host: p.ID, Kind: "exec", Command: "safe", Client: "AI"}, time.Minute, d); err == nil {
		t.Fatal("full grant bypassed server protection")
	}
	assertPolicyNoPending(t, cli, ctx)
}

func TestProtectedSFTPRejectsAliasesButPermitsSafeTypedWrite(t *testing.T) {
	f := testserver.New(t)
	root, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	critical, safe := filepath.Join(root, "protected"), filepath.Join(root, "safe")
	for _, dir := range []string{critical, safe} {
		if err = os.Mkdir(dir, 0700); err != nil {
			t.Fatal(err)
		}
	}
	valuable := filepath.Join(critical, "valuable.txt")
	if err = os.WriteFile(valuable, []byte("unchanged"), 0600); err != nil {
		t.Fatal(err)
	}
	if err = os.Symlink(critical, filepath.Join(safe, "alias")); err != nil {
		t.Skip(err)
	}
	p := f.Profile()
	p.Policy = accesspolicy.Policy{ProtectedPaths: []string{filepath.ToSlash(critical)}}
	s, cli, ctx := policyHarness(t, p)
	d := policySession(t, s, cli, ctx, p.ID, "full")
	remove := approval.Operation{Host: p.ID, Kind: "remove", Path: filepath.ToSlash(filepath.Join(safe, "alias", "valuable.txt")), Client: "AI"}
	r, ticket, err := cli.RequestDelegated(ctx, remove, time.Minute, d)
	if err != nil {
		t.Fatal("safe-looking alias should reach remote validation", err)
	}
	if stream, err := cli.Start(ctx, r.ID, ticket, 0, 0); err == nil {
		stream.Close()
		t.Fatal("alias reached mutation")
	}
	data, err := os.ReadFile(valuable)
	if err != nil || string(data) != "unchanged" {
		t.Fatal("protected target modified", err)
	}
	op := approval.Operation{Host: p.ID, Kind: "mkdir", Path: filepath.ToSlash(filepath.Join(safe, "created")), Client: "AI"}
	r, ticket, err = cli.RequestDelegated(ctx, op, time.Minute, d)
	if err != nil {
		t.Fatal(err)
	}
	stream, err := cli.Start(ctx, r.ID, ticket, 0, 0)
	if err != nil {
		t.Fatal(err)
	}
	_, _, err = stream.Copy(io.Discard, io.Discard)
	stream.Close()
	if err != nil {
		t.Fatal(err)
	}
	if _, err = os.Stat(filepath.Join(safe, "created")); err != nil {
		t.Fatal("safe typed write failed", err)
	}
	page, err := cli.History(ctx, auditlog.Query{Limit: 100}, s.Vault)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, e := range page.Events {
		if e.Reason == "protected_path" && e.Kind == "remove" {
			found = true
		}
	}
	if !found {
		t.Fatal("remote path rejection missing from history")
	}
}

func TestDelegatedScopeAndCrossHostRefusalsAreAudited(t *testing.T) {
	f := testserver.New(t)
	a, b := f.Profile(), f.Profile()
	a.ID, a.Name = "alpha", "Alpha"
	b.ID, b.Name = "beta", "Beta"
	s, cli, ctx := policyHarness(t, a, b)
	d := policySession(t, s, cli, ctx, a.ID, "read")
	for _, op := range []approval.Operation{
		{Host: a.ID, Kind: "exec", Command: "safe", Client: "read AI"},
		{Host: b.ID, Kind: "list", Path: "/", Client: "read AI"},
	} {
		if _, _, err := cli.RequestDelegated(ctx, op, time.Minute, d); err == nil {
			t.Fatal("scope violation accepted")
		}
	}
	assertPolicyNoPending(t, cli, ctx)
	page, err := cli.History(ctx, auditlog.Query{Limit: 100}, s.Vault)
	if err != nil {
		t.Fatal(err)
	}
	seen := map[string]bool{}
	for _, e := range page.Events {
		if e.Reason == "scope_denied" {
			seen[e.Host] = true
		}
	}
	if !seen[a.ID] || !seen[b.ID] {
		t.Fatal("scope/cross-host attempts missing", seen)
	}
	if f.Auths.Load() != 0 {
		t.Fatal("scope rejection reached SSH")
	}
}

func TestDisablingAutomationCancelsActiveGrantAndLeavesNoRenewal(t *testing.T) {
	f := testserver.New(t)
	p := f.Profile()
	s, cli, ctx := policyHarness(t, p)
	d := policySession(t, s, cli, ctx, p.ID, "full")
	r, ticket, err := cli.RequestDelegated(ctx, approval.Operation{Host: p.ID, Kind: "shell", Client: "AI"}, time.Minute, d)
	if err != nil {
		t.Fatal(err)
	}
	stream, err := cli.Start(ctx, r.ID, ticket, 80, 24)
	if err != nil {
		t.Fatal(err)
	}
	defer stream.Close()
	ownerRequest, ownerTicket, err := cli.RequestWithOwner(ctx, approval.Operation{Host: p.ID, Kind: "shell", Client: "human"}, s.Vault)
	if err != nil {
		t.Fatal(err)
	}
	if err = cli.DecideReviewedWithOwner(ctx, ownerRequest, true, false, s.Vault); err != nil {
		t.Fatal(err)
	}
	ownerStream, err := cli.Start(ctx, ownerRequest.ID, ownerTicket, 80, 24)
	if err != nil {
		t.Fatal(err)
	}
	defer ownerStream.Close()
	done := make(chan error, 1)
	go func() { _, _, e := stream.Copy(io.Discard, io.Discard); done <- e }()
	details := s.Vault.Summaries()[0]
	details.Policy.Disabled = true
	if err = cli.PutDetails(ctx, details, s.Vault); err != nil {
		t.Fatal(err)
	}
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("disable did not close active channel")
	}
	if _, err = ownerStream.Write([]byte("owner-still-connected")); err != nil {
		t.Fatal("policy-only change closed owner terminal", err)
	}
	var output strings.Builder
	for !strings.Contains(output.String(), "owner-still-connected") {
		response, err := ownerStream.Receive()
		if err != nil || response.Action != "data" {
			t.Fatal("owner terminal did not continue after policy change", response.Action, err)
		}
		output.Write(response.Data)
	}
	if _, _, err = cli.RequestDelegated(ctx, approval.Operation{Host: p.ID, Kind: "list", Path: "/", Client: "AI"}, time.Minute, d); err == nil {
		t.Fatal("disabled grant reused")
	}
	assertPolicyNoPending(t, cli, ctx)
	page, err := cli.History(ctx, auditlog.Query{Limit: 100}, s.Vault)
	if err != nil {
		t.Fatal(err)
	}
	changed, denied := false, false
	for _, e := range page.Events {
		changed = changed || e.Event == "policy_changed"
		denied = denied || e.Reason == "automation_disabled"
	}
	if !changed || !denied {
		t.Fatal("policy transition/refusal missing", changed, denied)
	}
}

func TestInvalidRenewalProofAndOwnerSpoofAreAuditedWithoutApproval(t *testing.T) {
	h := newRenewalHarness(t)
	original, d := h.grant(t, "read")
	challenge, err := h.c.Call(h.ctx, broker.Message{Action: "renewalChallenge", ID: original.ID, Ticket: d.Ticket})
	if err != nil || challenge.Renewal == nil {
		t.Fatal("renewal challenge missing", err)
	}
	for _, q := range []broker.Message{
		{Action: "renewSession", ID: challenge.Renewal.ID, Proof: []byte("invalid test proof")},
		{Action: "renewSession", ID: challenge.Renewal.ID, Proof: []byte("invalid test proof")},
		{Action: "renewalChallenge", ID: "unknown test authority"},
		{Action: "ownerRequest", Operation: approval.Operation{Host: "fixture", Kind: "exec", Command: "safe", Client: "所有者"}},
	} {
		if _, err = h.c.Call(h.ctx, q); err == nil {
			t.Fatal("unauthenticated authority accepted", q.Action)
		}
	}
	assertPolicyNoPending(t, h.c, h.ctx)
	page, err := h.c.History(h.ctx, auditlog.Query{Limit: 100}, h.s.Vault)
	if err != nil {
		t.Fatal(err)
	}
	invalid, owner := 0, 0
	for _, e := range page.Events {
		if e.Reason == "invalid_authority" {
			invalid++
		}
		if e.Reason == "owner_required" {
			owner++
			if e.OwnerIssued {
				t.Fatal("self-reported owner gained trusted audit identity")
			}
		}
	}
	if invalid != 3 || owner != 1 || h.commands.Load() != 0 {
		t.Fatal("authority refusals lost or executed", invalid, owner, h.commands.Load())
	}
}
