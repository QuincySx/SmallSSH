package vault

import (
	"errors"
	"path/filepath"
	"reflect"
	"testing"

	"sshclient/internal/accesspolicy"
)

func TestPolicyPersistenceAndIsolation(t *testing.T) {
	s, err := New(testPass)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	p := profile()
	p.Policy = accesspolicy.Policy{Disabled: true, ProtectedPaths: []string{"/srv/critical"}}
	expected := p.Policy.Clone()
	if err := s.Put(p); err != nil {
		t.Fatal(err)
	}
	p.Policy.ProtectedPaths[0] = "/input-mutated"
	got, _ := s.Profile(p.ID)
	if !reflect.DeepEqual(got.Policy, expected) {
		t.Fatal("Put exposed caller slice")
	}
	got.Policy.ProtectedPaths[0] = "/output-mutated"
	summary := s.Summaries()[0]
	if !reflect.DeepEqual(summary.Policy, expected) {
		t.Fatal("Profile exposed stored slice")
	}
	summary.Policy.ProtectedPaths[0] = "/summary-mutated"
	data, err := s.Seal()
	if err != nil {
		t.Fatal(err)
	}
	reopened, err := Open(data, testPass)
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	got, _ = reopened.Profile(p.ID)
	if !reflect.DeepEqual(got.Policy, expected) {
		t.Fatalf("policy roundtrip: %+v", got.Policy)
	}
	file := filepath.Join(t.TempDir(), "vault.json")
	if err := Save(file, s, true); err != nil {
		t.Fatal(err)
	}
	metadata := s.Summaries()[0]
	metadata.Name = "edited connection"
	if err := s.SaveDetails(file, metadata); err != nil {
		t.Fatal(err)
	}
	metadata.Policy.ProtectedPaths[0] = "/save-mutated"
	disk, err := Read(file, testPass)
	if err != nil {
		t.Fatal(err)
	}
	defer disk.Close()
	got, _ = disk.Profile(p.ID)
	if got.Name != "edited connection" || got.Password != profile().Password || !reflect.DeepEqual(got.Policy, expected) {
		t.Fatalf("metadata save changed authority: %+v", got.Policy)
	}
	got, _ = s.Profile(p.ID)
	if !reflect.DeepEqual(got.Policy, expected) {
		t.Fatal("SaveDetails exposed caller slice")
	}
}

func TestConnectionEditRoundTripsPolicy(t *testing.T) {
	s, _ := New(testPass)
	defer s.Close()
	p := profile()
	p.Policy = accesspolicy.Policy{Disabled: true, ProtectedPaths: []string{"/srv/critical"}}
	if err := s.Put(p); err != nil {
		t.Fatal(err)
	}
	edited := p
	edited.Name, edited.Password = "new name", ""
	resolved, err := s.ResolveChange(ProfileChange{Profile: edited, KeepCredential: true})
	if err != nil {
		t.Fatal(err)
	}
	if resolved.Password != p.Password || !reflect.DeepEqual(resolved.Policy, p.Policy) {
		t.Fatal("metadata edit lost credential or policy")
	}
	edited.Policy.ProtectedPaths[0] = "/changed"
	if resolved.Policy.ProtectedPaths[0] != "/srv/critical" {
		t.Fatal("ResolveChange exposed caller slice")
	}
	edited.Policy = accesspolicy.Policy{Unrestricted: true}
	resolved, err = s.ResolveChange(ProfileChange{Profile: edited, KeepCredential: true})
	if err != nil || !resolved.Policy.Unrestricted {
		t.Fatal("explicit policy edit was ignored", err)
	}
	edited.Policy = accesspolicy.Policy{ProtectedPaths: []string{"relative"}}
	if err := Validate(edited); !errors.Is(err, accesspolicy.ErrInvalidPolicy) {
		t.Fatal("invalid policy accepted", err)
	}
}
