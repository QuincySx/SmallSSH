package vault

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func TestUnverifiedProfilesSaveOfflineButCannotDial(t *testing.T) {
	s, err := New(testPass)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	p := profile()
	p.Fingerprint = ""
	file := filepath.Join(t.TempDir(), "vault")
	change, err := s.ResolveChange(ProfileChange{Profile: p})
	if err != nil {
		t.Fatal(err)
	}
	if err = s.SaveProfile(file, change); err != nil {
		t.Fatal(err)
	}
	if Validate(p) == nil {
		t.Fatal("unverified profile crossed the dialing boundary")
	}
	loaded, err := Read(file, testPass)
	if err != nil {
		t.Fatal(err)
	}
	defer loaded.Close()
	got, err := loaded.Profile(p.ID)
	if err != nil || !reflect.DeepEqual(got, p) {
		t.Fatal("unverified config did not round trip", err)
	}
}

func TestHostTrustPinsOnlyMatchingEndpointAndPublishesAfterSave(t *testing.T) {
	s, err := New(testPass)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	p := profile()
	p.Fingerprint = ""
	file := filepath.Join(t.TempDir(), "vault")
	if err = s.SaveProfile(file, p); err != nil {
		t.Fatal(err)
	}
	fingerprint := "SHA256:AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA"
	trust := HostTrustChange{ID: p.ID, Host: p.Host, Port: p.Port, Fingerprint: fingerprint}
	stale := trust
	stale.Port++
	if err = s.SaveHostTrust(file, stale); err == nil {
		t.Fatal("stale endpoint pinned")
	}
	for _, value := range []string{"SHA256:", fingerprint + "\n", "invalid-prefix"} {
		bad := trust
		bad.Fingerprint = value
		if err = s.SaveHostTrust(file, bad); err == nil {
			t.Fatal("invalid fingerprint pinned", value)
		}
	}
	blocker := filepath.Join(t.TempDir(), "file")
	if err = os.WriteFile(blocker, []byte("fixture"), 0600); err != nil {
		t.Fatal(err)
	}
	if err = s.SaveHostTrust(filepath.Join(blocker, "vault"), trust); err == nil {
		t.Fatal("save failure ignored")
	}
	got, _ := s.Profile(p.ID)
	if !reflect.DeepEqual(got, p) {
		t.Fatal("failed write published trust")
	}
	// A newer credential edit must be preserved by the narrow trust mutation.
	p.Password = "newer local credential"
	if err = s.SaveProfile(file, p); err != nil {
		t.Fatal(err)
	}
	if err = s.SaveHostTrust(file, trust); err != nil {
		t.Fatal(err)
	}
	got, _ = s.Profile(p.ID)
	p.Fingerprint = fingerprint
	if !reflect.DeepEqual(got, p) {
		t.Fatal("pinning clobbered configuration")
	}
	if err = s.SaveHostTrust(file, trust); err == nil {
		t.Fatal("stale trust confirmation replayed")
	}
	loaded, err := Read(file, testPass)
	if err != nil {
		t.Fatal(err)
	}
	defer loaded.Close()
	got, _ = loaded.Profile(p.ID)
	if !reflect.DeepEqual(got, p) {
		t.Fatal("fingerprint was not persisted")
	}
}
