package broker

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"

	"sshclient/internal/approval"
	"sshclient/internal/auditlog"
)

func TestAuditPersistsOnlyMetadataAndSafeReasonCodes(t *testing.T) {
	dir := t.TempDir()
	a, err := openAudit(dir)
	if err != nil {
		t.Fatal(err)
	}
	op := approval.Operation{Host: "fixture", Client: "label\n<script>", Kind: "exec", Command: "generated command secret", Path: "generated path secret"}
	r := approval.Request{ID: "generated-id", Nonce: "generated private nonce", Operation: op}
	if err = a.writeHost("approved", &r, "Generated host"); err != nil {
		t.Fatal(err)
	}
	if err = a.reject(op, "parent", "protected_path", "Generated host"); err != nil {
		t.Fatal(err)
	}
	if err = a.reject(op, "", "generated error containing sensitive text", ""); err == nil {
		t.Fatal("accepted raw rejection error")
	}
	if err = a.close(); err != nil {
		t.Fatal(err)
	}
	a, err = openAudit(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer a.close()
	p, err := a.history(auditlog.Query{})
	if err != nil || len(p.Events) != 2 {
		t.Fatal(p, err)
	}
	if p.Events[0].Reason != "protected_path" || p.Events[0].ParentID != "parent" || p.Events[0].RequestID == "" || p.Events[1].Client != op.Client || p.Events[1].HostName != "Generated host" {
		t.Fatal(p)
	}
	data, err := os.ReadFile(filepath.Join(dir, "audit.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	for _, secret := range []string{op.Command, op.Path, r.Nonce, "generated error containing sensitive text"} {
		if bytes.Contains(data, []byte(secret)) {
			t.Fatal("persisted operation content")
		}
	}
	if p.Events[1].OperationHash == "" || p.Events[1].Generation == "" {
		t.Fatal("lost compatibility hashes", p.Events[1])
	}
}

func TestAuditHistoryProofBindsChallengeLimitAndCursor(t *testing.T) {
	prove := func(b []byte) ([]byte, error) { return b, nil }
	q := auditlog.Query{Limit: 20, Before: "generated-cursor"}
	p, err := AuditHistoryProof("challenge", q, prove)
	if err != nil {
		t.Fatal(err)
	}
	for _, changed := range []struct {
		id string
		q  auditlog.Query
	}{{"another", q}, {"challenge", auditlog.Query{Limit: 21, Before: q.Before}}, {"challenge", auditlog.Query{Limit: 20, Before: "another"}}} {
		other, _ := AuditHistoryProof(changed.id, changed.q, prove)
		if bytes.Equal(p, other) {
			t.Fatal("proof failed to bind query")
		}
	}
}
