package approval

import (
	"sshclient/internal/fileattrs"
	"testing"
	"time"
)

func TestFileAttributesProofBindsOwnersModeAndExpectedSnapshot(t *testing.T) {
	m := New(prove)
	op := Operation{Host: "fixture", Client: "test", Kind: "file-attributes", Path: "/srv/file", Attributes: fileattrs.Change{
		Expected: fileattrs.Snapshot{Valid: true, Mode: 0644, UID: 1000, GID: 100, OwnershipKnown: true}, SetMode: true, Mode: 0600, SetUID: true, UID: 0, SetGID: true, GID: 0}}
	r, _, err := m.Create(op, time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	for _, change := range []func(*Operation){
		func(o *Operation) { o.Attributes.UID = 1001 }, func(o *Operation) { o.Attributes.GID = 1001 }, func(o *Operation) { o.Attributes.Mode = 0777 },
		func(o *Operation) { o.Attributes.Expected.UID++ }, func(o *Operation) { o.Attributes.Expected.Modified++ }, func(o *Operation) { o.Path = "/other" },
	} {
		altered := r
		change(&altered.Operation)
		proof, _ := DecisionProof(altered, true, prove)
		if err := m.Decide(r.ID, true, proof); err == nil {
			t.Fatal("modified attribute request approved")
		}
	}
	approve(t, m, r)
	for _, kind := range []string{"list", "file-info", "exec", "grant"} {
		bad := op
		bad.Kind = kind
		if _, _, err := m.Create(bad, time.Minute); err == nil {
			t.Fatal("attribute payload smuggled into", kind)
		}
	}
	bad := op
	bad.Attributes.UID = ^uint32(0)
	if _, _, err := m.Create(bad, time.Minute); err == nil {
		t.Fatal("-1 ownership sentinel accepted")
	}
}
