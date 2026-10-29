package broker_test

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
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
	"sshclient/internal/testserver"
	"sshclient/internal/transfer"
)

func TestPrivateQueriesRequireOwnerOrExactTicket(t *testing.T) {
	owner, f := setup(t)
	ctx := context.Background()
	stranger := client.Client{Dir: owner.Dir}
	r, ticket := request(t, owner, approval.Operation{Host: "fixture", Kind: "exec", Command: "synthetic private command", Client: "first client"})
	for _, q := range []broker.Message{
		{Action: "pending"}, {Action: "status", ID: r.ID},
		{Action: "sessionStatus", ID: r.ID}, {Action: "probeHost", Target: f.Profile().Host, Port: f.Profile().Port}, {Action: "agentIdentities"},
		{Action: "status", ID: r.ID, Ticket: "another client's ticket"},
	} {
		if _, err := stranger.Call(ctx, q); err == nil {
			t.Fatal("unproven query allowed", q.Action)
		}
	}
	// Host discovery is the one private query a caller may make without proof,
	// and it must not carry how to reach or log in to a machine.
	redacted, err := stranger.Call(ctx, broker.Message{Action: "hosts"})
	if err != nil || !redacted.Redacted || len(redacted.Connections) == 0 {
		t.Fatalf("unproven host discovery failed: %v %+v", err, redacted)
	}
	for _, h := range redacted.Connections {
		if h.Host != "" || h.Port != 0 || h.User != "" || h.Fingerprint != "" || h.ID == "" {
			t.Fatalf("host discovery leaked reachability: %+v", h)
		}
	}
	if f.Auths.Load() != 0 {
		t.Fatal("unauthorized query reached SSH authentication")
	}
	if got, err := stranger.Call(ctx, broker.Message{Action: "status", ID: r.ID, Ticket: ticket}); err != nil || got.Request == nil || got.Request.Operation != r.Operation {
		t.Fatal("ticket holder lost own visibility", err)
	}
	other, otherTicket := request(t, stranger, approval.Operation{Host: "fixture", Kind: "list", Path: "/", Client: "second client"})
	if _, err := stranger.Call(ctx, broker.Message{Action: "status", ID: other.ID, Ticket: ticket}); err == nil {
		t.Fatal("cross-client ticket accepted")
	}
	if _, err := stranger.Call(ctx, broker.Message{Action: "status", ID: other.ID, Ticket: otherTicket}); err != nil {
		t.Fatal(err)
	}
	challenge, err := stranger.Call(ctx, broker.Message{Action: "profileChallenge"})
	if err != nil {
		t.Fatal(err)
	}
	q := broker.Message{Action: "pending", Challenge: challenge.ID}
	q.Proof, err = broker.OwnerQueryProof(q.Challenge, q, owner.Owner.Proof)
	if err != nil {
		t.Fatal(err)
	}
	changed := q
	changed.Limit = 1
	if _, err = stranger.Call(ctx, changed); err == nil {
		t.Fatal("changed query accepted")
	}
	if _, err = stranger.Call(ctx, q); err == nil {
		t.Fatal("bad proof did not consume challenge")
	}
	challenge, err = stranger.Call(ctx, broker.Message{Action: "profileChallenge"})
	if err != nil {
		t.Fatal(err)
	}
	q.Challenge = challenge.ID
	q.Proof, _ = broker.OwnerQueryProof(q.Challenge, q, owner.Owner.Proof)
	if _, err = stranger.Call(ctx, q); err != nil {
		t.Fatal(err)
	}
	if _, err = stranger.Call(ctx, q); err == nil {
		t.Fatal("owner query replay accepted")
	}
}

func TestPendingRejectsIrrelevantPayloadAndPagesLargeQueue(t *testing.T) {
	owner, _ := setup(t)
	ctx := context.Background()
	c := client.Client{Dir: owner.Dir}
	if _, _, err := c.Request(ctx, approval.Operation{Host: "fixture", Kind: "list", Path: "/", Client: "unapproved", PrefixHash: strings.Repeat("a", 600<<10)}, time.Minute); err == nil {
		t.Fatal("irrelevant large field accepted")
	}
	// One process gets its own share of the queue and no more: renaming the
	// caller in each request does not buy another slot.
	for i := range 16 {
		request(t, c, approval.Operation{Host: "fixture", Kind: "exec", Command: strings.Repeat("a", 32768), Client: fmt.Sprintf("bounded pending fixture %d", i)})
	}
	if _, _, err := c.Request(ctx, approval.Operation{Host: "fixture", Kind: "exec", Command: "one too many", Client: "another label"}, time.Minute); err == nil {
		t.Fatal("one process filled more than its share of the queue")
	}
	all, err := owner.Call(ctx, broker.Message{Action: "pending"})
	if err != nil || len(all.Requests) != 16 {
		t.Fatal("large queue was lost", len(all.Requests), err)
	}
	seen := map[string]bool{}
	for _, r := range all.Requests {
		if seen[r.ID] {
			t.Fatal("duplicate page entry")
		}
		seen[r.ID] = true
	}
}

func TestProtectedReadsAndScopedRootsAreEnforcedAtExecution(t *testing.T) {
	f := testserver.New(t)
	root, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	protected, safe := filepath.Join(root, "protected"), filepath.Join(root, "safe")
	for _, dir := range []string{protected, safe} {
		if err = os.Mkdir(dir, 0700); err != nil {
			t.Fatal(err)
		}
	}
	secret, public := filepath.Join(protected, "synthetic-secret"), filepath.Join(safe, "public")
	if err = os.WriteFile(secret, []byte("synthetic secret"), 0600); err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(public, []byte("public fixture"), 0600); err != nil {
		t.Fatal(err)
	}
	alias := filepath.Join(safe, "alias")
	if err = os.Symlink(secret, alias); err != nil {
		t.Skip(err)
	}
	p := f.Profile()
	p.Policy = accesspolicy.Policy{ProtectedPaths: []string{testserver.RemotePath(protected)}}
	s, owner, ctx := policyHarness(t, p)
	d := policySession(t, s, owner, ctx, p.ID, "read")
	c := client.Client{Dir: owner.Dir, Delegation: d}
	for _, kind := range []string{"list", "get", "get-resume", "file-info"} {
		op := approval.Operation{Host: p.ID, Kind: kind, Path: testserver.RemotePath(secret), Client: "reader"}
		if _, _, err = c.Request(ctx, op, time.Minute); err == nil {
			t.Fatal("protected read accepted", kind)
		}
	}
	r, ticket, err := c.Request(ctx, approval.Operation{Host: p.ID, Kind: "get", Path: testserver.RemotePath(alias), Client: "reader"}, time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	if stream, err := c.Start(ctx, r.ID, ticket, 0, 0); err == nil {
		stream.Close()
		t.Fatal("symlink read escaped protection")
	}
	r, ticket, err = c.Request(ctx, approval.Operation{Host: p.ID, Kind: "file-info", Path: testserver.RemotePath(public), Client: "reader"}, time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	stream, err := c.Start(ctx, r.ID, ticket, 0, 0)
	if err != nil {
		t.Fatal(err)
	}
	info, err := stream.ReadFileInfo()
	stream.Close()
	if err != nil || len(info.Users) != 0 || len(info.Groups) != 0 {
		t.Fatal("untrusted file-info read account databases", err)
	}
	r, scoped, err := owner.RequestScopedSession(ctx, p.ID, "scoped", "full", time.Minute, time.Minute, testserver.RemotePath(safe), testserver.RemotePath(safe))
	if err != nil {
		t.Fatal(err)
	}
	defer scoped.Close()
	if err = owner.DecideReviewedWithOwner(ctx, r, true, false, s.Vault); err != nil {
		t.Fatal(err)
	}
	sc := client.Client{Dir: owner.Dir, Delegation: scoped}
	for _, op := range []approval.Operation{
		{Kind: "get", Path: testserver.RemotePath(secret)}, {Kind: "exec", Command: "true"}, {Kind: "monitor"},
		{Kind: "rename", Path: testserver.RemotePath(public), Destination: testserver.RemotePath(filepath.Join(root, "outside"))},
	} {
		op.Host, op.Client = p.ID, "scoped"
		if _, _, err = sc.Request(ctx, op, time.Minute); err == nil {
			t.Fatal("scoped grant expanded", op.Kind)
		}
	}
	r, ticket, err = sc.Request(ctx, approval.Operation{Host: p.ID, Kind: "get", Path: testserver.RemotePath(public), Client: "scoped"}, time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	stream, err = sc.Start(ctx, r.ID, ticket, 0, 0)
	if err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	_, _, err = stream.Copy(&out, io.Discard)
	stream.Close()
	if err != nil || out.String() != "public fixture" {
		t.Fatal("scoped read failed", err)
	}
}

func TestProtectedFreshCLIUploadWorksAndResumeStaysDenied(t *testing.T) {
	f := testserver.New(t)
	root, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	p := f.Profile()
	p.Policy = accesspolicy.Policy{ProtectedPaths: []string{testserver.RemotePath(filepath.Join(root, "critical"))}}
	s, owner, ctx := policyHarness(t, p)
	d := policySession(t, s, owner, ctx, p.ID, "full")
	c := client.Client{Dir: owner.Dir, Delegation: d}
	src, target := filepath.Join(root, "source"), filepath.Join(root, "target")
	body := []byte("fresh bounded upload")
	if err = os.WriteFile(src, body, 0600); err != nil {
		t.Fatal(err)
	}
	m := transfer.NewWithClient(c)
	j, err := m.Add(p.ID, "upload", src, testserver.RemotePath(target))
	if err != nil {
		t.Fatal(err)
	}
	if err = m.Run(ctx, j.ID, "scoped upload"); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(target)
	if err != nil || !bytes.Equal(got, body) {
		t.Fatal("fresh upload failed", err)
	}
	if _, _, err = c.Request(ctx, approval.Operation{Host: p.ID, Kind: "put-resume", Path: testserver.RemotePath(target), TransferID: j.ID, Size: j.Size, ContentHash: j.Hash, Client: "resume"}, time.Minute); err == nil {
		t.Fatal("resume writes were enabled")
	}
}

func TestCompletionAuditFailureRevokesAuthorityBeforeSuccessAndLatches(t *testing.T) {
	started, release := make(chan struct{}), make(chan struct{})
	f := testserver.NewWithExec(t, func(string) (string, string, int) { close(started); <-release; return "fixture", "", 0 })
	s, c, ctx := policyHarness(t, f.Profile())
	d := policySession(t, s, c, ctx, f.Profile().ID, "read")
	r, ticket := request(t, c, approval.Operation{Host: f.Profile().ID, Kind: "exec", Command: "synthetic fault", Client: "fault fixture"})
	if err := c.DecideReviewedWithOwner(ctx, r, true, false, s.Vault); err != nil {
		t.Fatal(err)
	}
	stream, err := c.Start(ctx, r.ID, ticket, 0, 0)
	if err != nil {
		t.Fatal(err)
	}
	defer stream.Close()
	<-started
	log := filepath.Join(c.Dir, "audit.jsonl")
	if err = os.Mkdir(log+".1", 0700); err != nil {
		close(release)
		t.Fatal(err)
	}
	if err = os.Truncate(log, auditlog.MaxFileBytes); err != nil {
		close(release)
		t.Fatal(err)
	}
	close(release)
	if _, _, err = stream.Copy(io.Discard, io.Discard); err == nil {
		t.Fatal("audit failure reported success")
	}
	if _, err = s.Approvals.Get(d.ParentID); err == nil {
		t.Fatal("other authority survived audit failure")
	}
	if err = os.Remove(log + ".1"); err != nil {
		t.Fatal(err)
	}
	if err = os.Truncate(log, 0); err != nil {
		t.Fatal(err)
	}
	if _, _, err = c.Request(ctx, approval.Operation{Host: f.Profile().ID, Kind: "list", Path: "/", Client: "fault fixture"}, time.Minute); err == nil {
		t.Fatal("audit fault silently recovered without relock")
	}
}

func TestAuditCompletionRecordsOutcomeWithoutOperationBody(t *testing.T) {
	c, _ := setup(t)
	r, ticket := request(t, c, approval.Operation{Host: "fixture", Kind: "exec", Command: "exit7", Client: "outcome fixture"})
	approve(t, c, r)
	stream, err := c.Start(context.Background(), r.ID, ticket, 0, 0)
	if err != nil {
		t.Fatal(err)
	}
	if code, _, err := stream.Copy(io.Discard, io.Discard); err != nil || code != 7 {
		t.Fatal(code, err)
	}
	stream.Close()
	data, err := os.ReadFile(filepath.Join(c.Dir, "audit.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, line := range bytes.Split(data, []byte{'\n'}) {
		var event auditlog.Event
		if json.Unmarshal(line, &event) == nil && event.RequestID == r.ID && event.Event == "ended" {
			found = event.Outcome == "failed" && event.ExitCode != nil && *event.ExitCode == 7
		}
	}
	if !found || bytes.Contains(data, []byte("exit7")) {
		t.Fatal("completion outcome missing or command leaked")
	}
}
