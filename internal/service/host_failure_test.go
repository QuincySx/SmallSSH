package service

import (
	"context"
	"errors"
	"net"
	"os"
	"runtime"
	"testing"
	"time"

	"sshclient/internal/protocol"
)

func TestUnexpectedListenerCloseConvergesAsFailure(t *testing.T) {
	base := b03bFailureTestDir(t)
	host, err := Open(base)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = host.Close() })
	if err := host.listener.Close(); err != nil {
		t.Fatal(err)
	}
	select {
	case <-host.Done():
	case <-time.After(2 * time.Second):
		t.Fatal("unexpected listener closure did not converge")
	}
	failure := host.failureError()
	if failure == nil || !errors.Is(failure, net.ErrClosed) {
		t.Fatalf("unexpected host failure: %v", failure)
	}
	reopened, err := Open(base)
	if err != nil {
		t.Fatalf("failed host kept the service lock: %v", err)
	}
	defer reopened.Close()
	if status := reopened.Status(); status.State != protocol.ServiceStateLocked {
		t.Fatalf("reopened host is not locked: %#v", status)
	}
}

func TestHostCloseDoesNotRecordFailure(t *testing.T) {
	base := b03bFailureTestDir(t)
	host, err := Open(base)
	if err != nil {
		t.Fatal(err)
	}
	if err := host.Close(); err != nil {
		t.Fatal(err)
	}
	if failure := host.failureError(); failure != nil {
		t.Fatalf("normal close recorded a failure: %v", failure)
	}
}

func b03bFailureTestDir(t *testing.T) string {
	t.Helper()
	base := ""
	if runtime.GOOS != "windows" {
		base = "/tmp"
	}
	dir, err := os.MkdirTemp(base, "b03b-failure-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(dir) })
	if err := os.Chmod(dir, 0700); err != nil {
		t.Fatal(err)
	}
	return dir
}

func TestRunCancelledContextDoesNotRecordFailure(t *testing.T) {
	base := b03bFailureTestDir(t)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := Run(ctx, base); err != nil {
		t.Fatal(err)
	}
	host, err := Open(base)
	if err != nil {
		t.Fatal(err)
	}
	defer host.Close()
	if status := host.Status(); status.State != protocol.ServiceStateLocked {
		t.Fatalf("reopened host is not locked: %#v", status)
	}
}
