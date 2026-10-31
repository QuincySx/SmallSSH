package broker_test

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"sshclient/internal/auditlog"
	"sshclient/internal/broker"
	"sshclient/internal/testserver"
	"sshclient/internal/vault"
)

// A log rewritten as a whole can keep a chain that verifies on its own. The head
// the broker stored inside the encrypted vault is what exposes it, and rewriting
// that copy needs the master password.
func TestRewrittenAuditHistoryIsRefusedOnNextUnlock(t *testing.T) {
	fixture := testserver.New(t)
	dir, err := os.MkdirTemp("", "sw-anchor-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(dir) })
	if err = os.Chmod(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	pass := []byte("anchor long master password")
	v, err := vault.New(pass)
	if err != nil {
		t.Fatal(err)
	}
	v.Put(fixture.Profile())
	if err = vault.Save(broker.VaultPath(dir), v, true); err != nil {
		t.Fatal(err)
	}
	v.Close()

	srv, err := broker.Open(dir, pass)
	if err != nil {
		t.Fatal(err)
	}
	srv.Close()
	reopened, err := vault.Read(broker.VaultPath(dir), pass)
	if err != nil {
		t.Fatal(err)
	}
	anchor := reopened.AuditHead()
	reopened.Close()
	if anchor == "" {
		t.Fatal("closing did not record an audit head in the vault")
	}

	// A shorter, internally consistent log: its own chain verifies from the start.
	path := filepath.Join(dir, "audit.jsonl")
	forged := `{"time":"2026-10-30T00:00:00Z","event":"requested","kind":"exec","host":"fixture"}` + "\n"
	if err = os.WriteFile(path, []byte(forged), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err = broker.Open(dir, pass); err == nil {
		t.Fatal("a rewritten audit history was accepted")
	}
}

// anchoredVault leaves a private directory holding a vault with one profile and
// the anchor a clean close writes, so the tests below can start from a history
// the vault already committed to.
func anchoredVault(t *testing.T) (string, []byte) {
	t.Helper()
	fixture := testserver.New(t)
	dir, err := os.MkdirTemp("", "sw-anchor-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(dir) })
	if err = os.Chmod(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	pass := []byte("anchor long master password")
	v, err := vault.New(pass)
	if err != nil {
		t.Fatal(err)
	}
	v.Put(fixture.Profile())
	if err = vault.Save(broker.VaultPath(dir), v, true); err != nil {
		t.Fatal(err)
	}
	v.Close()
	srv, err := broker.Open(dir, pass)
	if err != nil {
		t.Fatal(err)
	}
	srv.Close()
	return dir, pass
}

// A crash or a forced quit leaves records the last clean close never saw. They
// start from the anchored record, so they are an uncommitted tail, not a
// rewrite — refusing to unlock would order a legitimate user away from their
// vault over a missing shutdown.
func TestUncommittedTailAfterACrashStillUnlocks(t *testing.T) {
	dir, pass := anchoredVault(t)
	log, err := auditlog.Open(filepath.Join(dir, "audit.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	if err = log.Append(auditlog.Event{Time: time.Now().UTC(), Event: "started", Kind: "exec"}); err != nil {
		t.Fatal(err)
	}
	log.Close()

	srv, err := broker.Open(dir, pass)
	if err != nil {
		t.Fatalf("an uncommitted tail refused the vault: %v", err)
	}
	srv.Close()
}

// The desktop drops the signing key before the broker finishes its cleanup, so
// a sealed vault cannot store the anchor. The lock path commits it first;
// without that the stored anchor falls behind the history it committed to.
func TestLockCommitsTheAnchorBeforeTheVaultCloses(t *testing.T) {
	dir, pass := anchoredVault(t)
	srv, err := broker.Open(dir, pass)
	if err != nil {
		t.Fatal(err)
	}
	if err = srv.SealAudit(); err != nil {
		t.Fatalf("locking could not commit the anchor: %v", err)
	}
	// internal/desktop/app.go closes the vault before the asynchronous cleanup.
	srv.Vault.Close()
	srv.Close()

	reopened, err := vault.Read(broker.VaultPath(dir), pass)
	if err != nil {
		t.Fatal(err)
	}
	anchor := reopened.AuditHead()
	reopened.Close()
	if anchor == "" {
		t.Fatal("locking did not commit an audit anchor")
	}
	next, err := broker.Open(dir, pass)
	if err != nil {
		t.Fatalf("unlocking after a lock refused: %v", err)
	}
	next.Close()
}

// A caller that closes the vault without sealing (or a save that fails) leaves
// the anchor behind the history. That is still a prefix of it.
func TestStaleAnchorIsAcceptedAsAPrefix(t *testing.T) {
	dir, pass := anchoredVault(t)
	srv, err := broker.Open(dir, pass)
	if err != nil {
		t.Fatal(err)
	}
	srv.Vault.Close() // deliberately skips SealAudit
	srv.Close()

	next, err := broker.Open(dir, pass)
	if err != nil {
		t.Fatalf("a stale anchor refused the vault: %v", err)
	}
	next.Close()
}
