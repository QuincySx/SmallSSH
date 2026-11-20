package auditlog

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"sshclient/internal/vault"
)

func TestRotationKeepsDurableVaultAnchorAcrossCrash(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "vault.json")
	pass := []byte("checkpoint regression password")
	owner, err := vault.New(pass)
	if err != nil {
		t.Fatal(err)
	}
	defer owner.Close()
	if err := vault.Save(file, owner, true); err != nil {
		t.Fatal(err)
	}
	s, path := openTest(t)
	appendEvent(t, s, 0)
	if err := owner.SaveAuditHead(file, s.Head()); err != nil {
		t.Fatal(err)
	}
	rotations := 0
	s.SetRotationCheckpoint(func(head string) error {
		rotations++
		return owner.SaveAuditHead(file, head)
	})
	s.maxBytes = 500
	for n := 1; n < 30; n++ {
		appendEvent(t, s, n)
	}
	if rotations < 3 {
		t.Fatal("did not exercise anchor eviction across multiple rotations")
	}
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	// Simulate a crash: no final anchor update at close.
	reopened, err := vault.Read(file, pass)
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	s, err = Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if found, err := s.Contains(reopened.AuditHead()); err != nil || !found {
		t.Fatal("a legitimate crash lost the durable vault anchor", found, err)
	}
	// Retention coordination must not accept an independently rewritten chain.
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(path + ".1"); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("{\"event\":\"forged\"}\n"), 0600); err != nil {
		t.Fatal(err)
	}
	s, err = Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if found, err := s.Contains(reopened.AuditHead()); err != nil || found {
		t.Fatal("rewritten history matched the committed anchor", found, err)
	}
}

func TestFailedCheckpointDoesNotRotateOrChangeTheVaultAnchor(t *testing.T) {
	s, path := openTest(t)
	appendEvent(t, s, 0)
	before, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	head := s.Head()
	s.maxBytes = int64(len(before))
	want := errors.New("checkpoint storage failed")
	s.SetRotationCheckpoint(func(string) error { return want })
	if err := s.Append(event(1)); !errors.Is(err, want) {
		t.Fatal("checkpoint failure did not reject append", err)
	}
	after, err := os.ReadFile(path)
	if err != nil || !bytes.Equal(before, after) || s.Head() != head {
		t.Fatal("failed checkpoint changed retained history", err)
	}
	if _, err := os.Stat(path + ".1"); !os.IsNotExist(err) {
		t.Fatal("failed checkpoint rotated the current file", err)
	}
}
