package vault

import (
	"path/filepath"
	"reflect"
	"testing"
)

func TestConnectionOrderPersistsMembershipAndPreservesCredentials(t *testing.T) {
	pass := []byte("synthetic ordering vault password")
	s, err := New(pass)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	file := filepath.Join(t.TempDir(), "vault.json")
	if err = Save(file, s, true); err != nil {
		t.Fatal(err)
	}
	for _, p := range []Profile{{ID: "bridge", Name: "Bridge", Host: "192.0.2.1", Port: 22, User: "ops", Fingerprint: "SHA256:synthetic", Auth: "password", Password: "synthetic-order-only", Group: "B"}, {ID: "web", Name: "Web", Host: "192.0.2.2", Port: 22, User: "ops", Fingerprint: "SHA256:synthetic", Auth: "password", Password: "synthetic-other-only", Group: "A"}} {
		if err = s.SaveProfile(file, p); err != nil {
			t.Fatal(err)
		}
	}
	change := ConnectionOrder{Revision: ConnectionOrderRevision(s.Summaries()), Items: []ConnectionPlacement{{"web", "B"}, {"bridge", "B"}}}
	if err = s.SaveConnectionOrder(file, change); err != nil {
		t.Fatal(err)
	}
	opened, err := Read(file, pass)
	if err != nil {
		t.Fatal(err)
	}
	defer opened.Close()
	rows := opened.Summaries()
	if rows[0].ID != "web" || rows[1].ID != "bridge" || rows[0].Group != "B" {
		t.Fatal(rows)
	}
	for id, want := range map[string]string{"bridge": "synthetic-order-only", "web": "synthetic-other-only"} {
		p, err := opened.Profile(id)
		if err != nil || p.Password != want || p.User != "ops" {
			t.Fatal("placement changed login identity", id)
		}
	}
	before := opened.Summaries()
	if err = opened.SaveConnectionOrder(file, change); err == nil || !reflect.DeepEqual(before, opened.Summaries()) {
		t.Fatal("stale snapshot changed order")
	}
	bad := ConnectionOrder{Revision: ConnectionOrderRevision(before), Items: []ConnectionPlacement{{"web", "A"}, {"web", "A"}}}
	if err = opened.SaveConnectionOrder(file, bad); err == nil || !reflect.DeepEqual(before, opened.Summaries()) {
		t.Fatal("duplicate IDs changed order")
	}
	bad.Items = []ConnectionPlacement{{"bridge", "A"}, {"web", "A"}}
	if err = opened.SaveConnectionOrder(filepath.Join(file, "not-a-directory"), bad); err == nil || !reflect.DeepEqual(before, opened.Summaries()) {
		t.Fatal("failed durable save published placement")
	}
}
