package vault

import (
	"path/filepath"
	"testing"
)

func TestEmptyPasswordValidationStorageAndReplacement(t *testing.T) {
	session, err := New(testPass)
	if err != nil {
		t.Fatal(err)
	}
	defer session.Close()
	key, err := session.UnlockKey()
	if err != nil {
		t.Fatal(err)
	}
	defer clear(key)

	p := profile()
	p.Auth = "password"
	p.Password = ""
	if err = Validate(p); err != nil {
		t.Fatal("empty password profile rejected", err)
	}
	if err = session.Put(p); err != nil {
		t.Fatal(err)
	}
	file := filepath.Join(t.TempDir(), "vault.json")
	if err = Save(file, session, true); err != nil {
		t.Fatal(err)
	}
	stored, err := ReadKey(file, key)
	if err != nil {
		t.Fatal(err)
	}
	got, err := stored.Profile(p.ID)
	stored.Close()
	if err != nil || got.Password != "" {
		t.Fatal("empty password did not round-trip through the vault")
	}

	p.Auth = "key"
	p.PrivateKey = ""
	if Validate(p) == nil {
		t.Fatal("missing private key accepted")
	}
	p.Auth = "agent"
	p.AgentFingerprint = ""
	if Validate(p) == nil {
		t.Fatal("missing agent identity accepted")
	}

	p = profile()
	p.Password = "synthetic stored server password"
	if err = session.Put(p); err != nil {
		t.Fatal(err)
	}
	draft := p
	draft.Password = ""
	resolved, err := session.ResolveChange(ProfileChange{Profile: draft, KeepCredential: true})
	if err != nil || resolved.Password != p.Password {
		t.Fatal("keeping credentials erased the stored password")
	}
	resolved, err = session.ResolveChange(ProfileChange{Profile: draft, KeepCredential: false})
	if err != nil || resolved.Password != "" {
		t.Fatal("explicit empty password replacement was not retained")
	}
	if err = session.SaveProfile(file, resolved); err != nil {
		t.Fatal(err)
	}
	stored, err = ReadKey(file, key)
	if err != nil {
		t.Fatal(err)
	}
	got, err = stored.Profile(p.ID)
	stored.Close()
	if err != nil || got.Password != "" {
		t.Fatal("empty password replacement did not persist")
	}
}
