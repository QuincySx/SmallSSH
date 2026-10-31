package broker_test

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"sshclient/internal/approval"
	"sshclient/internal/auditlog"
	"sshclient/internal/broker"
	"sshclient/internal/client"
	"sshclient/internal/localipc"
	"sshclient/internal/testserver"
	"sshclient/internal/vault"
)

func TestAuditHistoryRequiresExactOneUseOwnerProofAndDoesNotLogReads(t *testing.T) {
	c, _ := setup(t)
	ctx := context.Background()
	owner, err := vault.Read(broker.VaultPath(c.Dir), pass)
	if err != nil {
		t.Fatal(err)
	}
	defer owner.Close()
	q := auditlog.Query{Limit: 3}
	if r, err := c.Call(ctx, broker.Message{Action: "auditHistory", AuditQuery: &q}); err == nil || r.AuditPage != nil {
		t.Fatal("unproved caller read history", err)
	}
	challenge, err := c.Call(ctx, broker.Message{Action: "profileChallenge"})
	if err != nil {
		t.Fatal(err)
	}
	proof, err := broker.AuditHistoryProof(challenge.ID, q, owner.Proof)
	if err != nil {
		t.Fatal(err)
	}
	changed := auditlog.Query{Limit: 4}
	if _, err = c.Call(ctx, broker.Message{Action: "auditHistory", ID: challenge.ID, Proof: proof, AuditQuery: &changed}); err == nil {
		t.Fatal("changed query accepted")
	}
	if _, err = c.Call(ctx, broker.Message{Action: "auditHistory", ID: challenge.ID, Proof: proof, AuditQuery: &q}); err == nil {
		t.Fatal("failed proof did not consume challenge")
	}
	challenge, err = c.Call(ctx, broker.Message{Action: "profileChallenge"})
	if err != nil {
		t.Fatal(err)
	}
	proof, err = broker.AuditHistoryProof(challenge.ID, q, owner.Proof)
	if err != nil {
		t.Fatal(err)
	}
	changed = auditlog.Query{Limit: q.Limit, Before: "forged cursor"}
	if _, err = c.Call(ctx, broker.Message{Action: "auditHistory", ID: challenge.ID, Proof: proof, AuditQuery: &changed}); err == nil {
		t.Fatal("changed cursor accepted")
	}
	challenge, err = c.Call(ctx, broker.Message{Action: "profileChallenge"})
	if err != nil {
		t.Fatal(err)
	}
	proof, err = broker.AuditHistoryProof(challenge.ID, q, owner.Proof)
	if err != nil {
		t.Fatal(err)
	}
	request := broker.Message{Action: "auditHistory", ID: challenge.ID, Proof: proof, AuditQuery: &q}
	if r, err := c.Call(ctx, request); err != nil || r.AuditPage == nil {
		t.Fatal("owner could not read history", err)
	}
	if _, err = c.Call(ctx, request); err == nil {
		t.Fatal("history challenge replay accepted")
	}
	path := filepath.Join(c.Dir, "audit.jsonl")
	before, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	page, err := c.History(ctx, q, owner)
	if err != nil || len(page.Events) != 3 {
		t.Fatal(page, err)
	}
	after, err := os.ReadFile(path)
	if err != nil || !bytes.Equal(before, after) {
		t.Fatal("successful reads grew audit", err)
	}
	if _, err = c.History(ctx, q, nil); err == nil {
		t.Fatal("locked owner read history")
	}
	if page.Next == "" {
		t.Fatal("missing history cursor")
	}
	later, _, err := c.Request(ctx, approval.Operation{Host: "fixture", Kind: "list", Path: "/generated-list", Client: "Generated pagination caller"}, time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	if err = c.Decide(ctx, later.ID, false, false, pass); err != nil {
		t.Fatal(err)
	}
	older, err := c.History(ctx, auditlog.Query{Limit: 3, Before: page.Next}, owner)
	if err != nil || len(older.Events) != 3 {
		t.Fatal("cursor changed after newer events", older, err)
	}
	seen := map[string]bool{}
	for _, v := range page.Events {
		seen[v.RequestID] = true
	}
	for _, v := range older.Events {
		if v.RequestID == later.ID || v.RequestID != "" && seen[v.RequestID] {
			t.Fatal("page repeated or included new records", v)
		}
	}
}

func TestAuditHistorySurvivesBrokerRestart(t *testing.T) {
	fixture := testserver.New(t)
	dir, err := os.MkdirTemp("", "ssh-audit-restart-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(dir) })
	if err = localipc.Prepare(dir); err != nil {
		t.Fatal(err)
	}
	owner, err := vault.New(pass)
	if err != nil {
		t.Fatal(err)
	}
	p := fixture.Profile()
	p.Name = "Generated audit fixture"
	owner.Put(p)
	blocked := p
	blocked.ID = "blocked-fixture"
	blocked.Name = "Generated blocked fixture"
	blocked.Policy.Disabled = true
	owner.Put(blocked)
	if err = vault.Save(broker.VaultPath(dir), owner, true); err != nil {
		owner.Close()
		t.Fatal(err)
	}
	s, err := broker.OpenSession(dir, owner)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(s.Close)
	c := client.Client{Dir: dir}
	ctx := context.Background()
	r, ticket, err := c.Request(ctx, approval.Operation{Host: p.ID, Kind: "exec", Command: "generated sensitive command", Client: "Generated caller"}, time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	if err = c.Decide(ctx, r.ID, false, false, pass); err != nil {
		t.Fatal(err)
	}
	policyOp := approval.Operation{Host: blocked.ID, Kind: "put", Path: "/generated-sensitive-file", ContentHash: "generated-content-hash", Client: "Generated policy caller"}
	if _, _, err = c.Request(ctx, policyOp, time.Minute); err == nil {
		t.Fatal("disabled fixture accepted automation")
	}
	s.Close()
	s, err = broker.Open(dir, pass)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(s.Close)
	owner, err = vault.Read(broker.VaultPath(dir), pass)
	if err != nil {
		t.Fatal(err)
	}
	defer owner.Close()
	page, err := c.History(ctx, auditlog.Query{}, owner)
	if err != nil {
		t.Fatal(err)
	}
	found, foundPolicy := false, false
	for _, v := range page.Events {
		if v.RequestID == r.ID && v.Event == "denied" {
			found = true
			if v.Client != "Generated caller" || v.HostName != p.Name || v.Host != p.ID {
				t.Fatal("lost meaningful operation metadata", v)
			}
		}
		if v.Host == blocked.ID && v.Event == "denied" && v.Reason == "automation_disabled" {
			foundPolicy = true
			if v.Client != policyOp.Client || v.HostName != blocked.Name || v.RequestID == "" {
				t.Fatal("lost policy refusal metadata", v)
			}
		}
	}
	if !found || !foundPolicy {
		t.Fatal("restart lost denied operation or policy refusal", page)
	}
	b, _ := os.ReadFile(filepath.Join(dir, "audit.jsonl"))
	for _, secret := range []string{p.Password, ticket, r.Nonce} {
		if bytes.Contains(b, []byte(secret)) {
			t.Fatal("history persisted a credential, ticket or nonce")
		}
	}
	for _, kept := range []string{r.Operation.Command, policyOp.Path} {
		if !bytes.Contains(b, []byte(kept)) {
			t.Fatal("history lost the operation the owner reviewed", kept)
		}
	}
	if fixture.Auths.Load() != 0 {
		t.Fatal("history/rejection reached remote fixture")
	}
}
