package vault

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

var testPass = []byte("a long independent test passphrase")

func profile() Profile {
	return Profile{ID: "test", Name: "private server", Host: "127.0.0.1", Port: 22, User: "tester", Fingerprint: "SHA256:test", Auth: "password", Password: "never-plaintext-password"}
}
func TestEncryptedRoundTripAndFreshNonce(t *testing.T) {
	s, e := New(testPass)
	if e != nil {
		t.Fatal(e)
	}
	defer s.Close()
	if e = s.Put(profile()); e != nil {
		t.Fatal(e)
	}
	a, _ := s.Seal()
	b, _ := s.Seal()
	if bytes.Equal(a, b) {
		t.Fatal("nonce reused")
	}
	for _, secret := range []string{"127.0.0.1", "private server", "never-plaintext-password"} {
		if bytes.Contains(a, []byte(secret)) {
			t.Fatal("plaintext leaked")
		}
	}
	v, e := Open(a, testPass)
	if e != nil {
		t.Fatal(e)
	}
	defer v.Close()
	p, e := v.Profile("test")
	if e != nil || p.Password != profile().Password {
		t.Fatal("roundtrip failed", e)
	}
	x, _ := s.Proof([]byte("request"))
	y, _ := v.Proof([]byte("request"))
	if !bytes.Equal(x, y) {
		t.Fatal("unstable owner proof")
	}
	s.Close()
	if _, e = s.Seal(); e == nil {
		t.Fatal("locked vault usable")
	}
}
func TestWrongPasswordTamperAndHeader(t *testing.T) {
	s, _ := New(testPass)
	defer s.Close()
	b, _ := s.Seal()
	if _, e := Open(b, []byte("wrong")); e != ErrUnlock {
		t.Fatal(e)
	}
	var env envelope
	json.Unmarshal(b, &env)
	env.Ciphertext[0] ^= 1
	bad, _ := json.Marshal(env)
	if _, e := Open(bad, testPass); e != ErrUnlock {
		t.Fatal(e)
	}
	env.Ciphertext[0] ^= 1
	env.Header.ID[0] ^= 1
	bad, _ = json.Marshal(env)
	if _, e := Open(bad, testPass); e != ErrUnlock {
		t.Fatal("header not authenticated")
	}
	env.Header.Version = 99
	bad, _ = json.Marshal(env)
	if _, e := Open(bad, testPass); e != ErrUnlock {
		t.Fatal("accepted version")
	}
}
func TestSavePermissionsAndNoClobber(t *testing.T) {
	s, _ := New(testPass)
	defer s.Close()
	p := filepath.Join(t.TempDir(), "vault.json")
	if e := Save(p, s, true); e != nil {
		t.Fatal(e)
	}
	if e := Save(p, s, true); e == nil {
		t.Fatal("overwrote existing vault")
	}
	v, e := Read(p, testPass)
	if e != nil {
		t.Fatal(e)
	}
	v.Close()
	if runtime.GOOS != "windows" {
		os.Chmod(p, 0644)
		if _, e = Read(p, testPass); e == nil {
			t.Fatal("accepted public vault")
		}
	}
}

func TestFailedSaveDoesNotPublishNewCredentials(t *testing.T) {
	s, _ := New(testPass)
	defer s.Close()
	p := profile()
	s.Put(p)
	p.Password = "replacement"
	parent := filepath.Join(t.TempDir(), "file")
	os.WriteFile(parent, []byte("occupied"), 0600)
	if e := s.SaveProfile(filepath.Join(parent, "vault.json"), p); e == nil {
		t.Fatal("expected disk error")
	}
	got, _ := s.Profile(p.ID)
	if got.Password == "replacement" {
		t.Fatal("unsaved credentials published")
	}
}
