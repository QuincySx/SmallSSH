package broker

import (
	"os"
	"testing"
	"time"

	"sshclient/internal/localipc"
	"sshclient/internal/vault"
)

const coreLifetimeTestPassword = "synthetic core lifetime test password"

func newCoreLifetimeTestServer(t *testing.T) (string, *Server) {
	t.Helper()
	dir, err := os.MkdirTemp("", "core-fault-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(dir) })
	if err = os.Chmod(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	source, err := vault.New([]byte(coreLifetimeTestPassword))
	if err != nil {
		t.Fatal(err)
	}
	if err = vault.Save(VaultPath(dir), source, true); err != nil {
		source.Close()
		t.Fatal(err)
	}
	source.Close()

	core, err := Open(dir, []byte(coreLifetimeTestPassword))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(core.Close)
	return dir, core
}

func waitCoreLifetimeCondition(t *testing.T, label string, condition func() bool) {
	t.Helper()
	timeout := time.NewTimer(5 * time.Second)
	defer timeout.Stop()
	ticker := time.NewTicker(10 * time.Millisecond)
	defer ticker.Stop()
	for {
		if condition() {
			return
		}
		select {
		case <-timeout.C:
			t.Fatalf("%s did not settle within 5 seconds", label)
		case <-ticker.C:
		}
	}
}

func TestCoreListenerFaultEndsAuthorityAndCleansUp(t *testing.T) {
	dir, core := newCoreLifetimeTestServer(t)
	select {
	case <-core.Done():
		t.Fatal("fresh core authority was already closed")
	default:
	}
	if core.Err() != nil {
		t.Fatal("fresh core reported a fault")
	}

	if err := core.listener.Close(); err != nil {
		t.Fatalf("close actual listener: %v", err)
	}
	waitCoreLifetimeCondition(t, "faulted core Done", func() bool {
		select {
		case <-core.Done():
			return true
		default:
			return false
		}
	})
	firstErr := core.Err()
	if firstErr == nil {
		t.Fatal("listener fault did not record an error")
	}

	waitCoreLifetimeCondition(t, "vault cleanup", func() bool {
		key, err := core.Vault.UnlockKey()
		if err != nil {
			return true
		}
		clear(key)
		return false
	})

	var release func()
	waitCoreLifetimeCondition(t, "directory lock release", func() bool {
		release, _ = localipc.Acquire(dir)
		return release != nil
	})
	release()

	closed := make(chan struct{})
	go func() {
		core.Close()
		close(closed)
	}()
	select {
	case <-closed:
	case <-time.After(5 * time.Second):
		t.Fatal("repeat Close did not return")
	}
	if got := core.Err(); got != firstErr {
		t.Fatal("repeat Close changed the first fault")
	}
}

func TestCoreNormalCloseDoesNotRecordFault(t *testing.T) {
	_, core := newCoreLifetimeTestServer(t)
	core.Close()
	select {
	case <-core.Done():
	default:
		t.Fatal("normal Close did not close Done")
	}
	if core.Err() != nil {
		t.Fatal("normal Close recorded a fault")
	}

	var nilCore *Server
	select {
	case <-nilCore.Done():
	default:
		t.Fatal("nil core Done was not closed")
	}
	if nilCore.Err() != nil {
		t.Fatal("nil core reported a fault")
	}
}
