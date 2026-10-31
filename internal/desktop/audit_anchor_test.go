package desktop

import (
	"context"
	"os"
	"testing"

	"sshclient/internal/broker"
	"sshclient/internal/testserver"
	"sshclient/internal/vault"
)

// Locking drops the signing key before the broker finishes its cleanup. The
// anchor therefore has to be committed by the lock itself: a sealed vault cannot
// be saved again, and an anchor left behind the history would make the next
// unlock look like a rewritten audit log.
func TestLockCommitsTheAuditAnchorBeforeTheKeyGoesAway(t *testing.T) {
	fixture := testserver.New(t)
	dir, err := os.MkdirTemp("", "sw-anchor-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(dir) })
	if err = os.Chmod(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	pass := []byte("desktop anchor password")
	v, err := vault.New(pass)
	if err != nil {
		t.Fatal(err)
	}
	v.Put(fixture.Profile())
	if err = vault.Save(broker.VaultPath(dir), v, true); err != nil {
		t.Fatal(err)
	}
	v.Close()
	server, err := broker.Open(dir, pass)
	if err != nil {
		t.Fatal(err)
	}
	a := &app{ctx: context.Background(), online: true, dir: dir, server: server}
	a.lock()
	if a.server != nil || a.online {
		t.Fatal("lock kept the broker")
	}
	if a.unlockCleanup == nil {
		t.Fatal("lock did not schedule the asynchronous cleanup")
	}
	<-a.unlockCleanup

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
