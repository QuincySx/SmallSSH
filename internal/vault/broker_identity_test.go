package vault

import (
	"bytes"
	"crypto/ed25519"
	"path/filepath"
	"testing"
)

func TestBrokerIdentityStableAcrossUnlockAndSeparateFromClientSessions(t *testing.T) {
	s, err := New(testPass)
	if err != nil {
		t.Fatal(err)
	}
	key, err := s.BrokerIdentity()
	if err != nil || len(key) != ed25519.PrivateKeySize {
		t.Fatal(err)
	}
	defer clear(key)
	pin, err := s.BrokerPublicKeyPin()
	if err != nil {
		t.Fatal(err)
	}
	name := filepath.Join(t.TempDir(), "vault.json")
	if err = Save(name, s, true); err != nil {
		t.Fatal(err)
	}
	s.Close()
	if _, err = s.BrokerIdentity(); err == nil {
		t.Fatal("locked vault returned a signing identity")
	}
	reopened, err := Read(name, testPass)
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	again, err := reopened.BrokerIdentity()
	if err != nil {
		t.Fatal(err)
	}
	defer clear(again)
	got, err := reopened.BrokerPublicKeyPin()
	if err != nil || got != pin || !bytes.Equal(key, again) {
		t.Fatal("unlock changed the broker identity", err)
	}
	other, err := New(testPass)
	if err != nil {
		t.Fatal(err)
	}
	defer other.Close()
	otherPin, err := other.BrokerPublicKeyPin()
	if err != nil || otherPin == pin {
		t.Fatal("different vaults shared a broker identity", err)
	}
}
