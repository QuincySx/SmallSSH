package approval

import (
	"strings"
	"testing"
	"time"
)

func TestOperationSchemaRejectsUnrelatedAndOversizedEncodedFields(t *testing.T) {
	m := New(prove)
	for _, op := range []Operation{
		{Kind: "list", Path: "/", PrefixHash: strings.Repeat("a", 600<<10)},
		{Kind: "get", Path: "/safe", TransferID: "unexpected"},
		{Kind: "remove", Path: "/safe", Mode: 0600},
		{Kind: "put", Path: "/safe", ContentHash: "not a digest"},
		{Kind: "exec", Command: strings.Repeat("\x01", 32768)},
	} {
		op.Host, op.Client = "fixture", "schema fixture"
		if _, _, err := m.Create(op, time.Minute); err == nil {
			t.Fatal("invalid operation stored", op.Kind)
		}
	}
	if len(m.List()) != 0 {
		t.Fatal("invalid operation created authority")
	}
}

func TestScopedGrantBindsRootsAndRejectsOpaqueAccess(t *testing.T) {
	m, parent, _, _ := grantFixture(t, "full")
	parent.Operation.Grant.ReadRoot, parent.Operation.Grant.WriteRoot = "/srv/read", "/srv/write"
	// Build a fresh request so these roots also participate in owner approval.
	parent, _, err := m.Create(parent.Operation, time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	changed := parent
	changed.Operation.Grant.ReadRoot = "/"
	proof, _ := DecisionProof(changed, true, prove)
	if m.Decide(parent.ID, true, proof) == nil {
		t.Fatal("changed roots were approved")
	}
	approve(t, m, parent)
	for _, op := range []Operation{
		{Kind: "get", Path: "/srv/read2/secret"},
		{Kind: "remove", Path: "/srv/write"},
		{Kind: "rename", Path: "/srv/write/a", Destination: "/outside"},
		{Kind: "exec", Command: "true"}, {Kind: "monitor"},
	} {
		op.Host, op.Client, op.Endpoint, op.HostFingerprint = parent.Operation.Host, "scoped", parent.Operation.Endpoint, parent.Operation.HostFingerprint
		if _, err = m.DelegationChallenge(parent.ID, op, time.Minute); err == nil {
			t.Fatal("scoped grant expanded", op.Kind)
		}
	}
}
