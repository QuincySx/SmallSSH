package service_test

import (
	"context"
	"testing"
	"time"

	"sshclient/internal/localipc"
	"sshclient/internal/protocol"
	"sshclient/internal/service"
)

func waitHostCoreLifetimeCondition(t *testing.T, label string, condition func() bool) {
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

func acquireHostCoreLifetimeDir(t *testing.T, dir string) func() {
	t.Helper()
	var release func()
	waitHostCoreLifetimeCondition(t, "broker directory lock release", func() bool {
		release, _ = localipc.Acquire(dir)
		return release != nil
	})
	return release
}

func TestHostRetiresActuallyClosedCoreAndAcceptsFreshCommit(t *testing.T) {
	fixture := newB04c2b1Fixture(t)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	preparedA := prepareB04c3c(t, ctx, fixture, "prepare-core-a")
	committedA := commitB04c3c(t, fixture, preparedA)
	coreA := service.CoreForTesting(fixture.host)
	if coreA == nil {
		t.Fatal("commit did not publish core A")
	}
	generation := fixture.host.Status().Generation

	preparedB := prepareB04c3c(t, ctx, fixture, "prepare-pending-b")
	waitB04c2b2Pending(t, fixture.host, 1)
	if got := fixture.host.Status().Generation; got != generation {
		t.Fatalf("prepare changed generation: got=%d want=%d", got, generation)
	}

	coreA.Close()
	waitHostCoreLifetimeCondition(t, "host core retirement", func() bool {
		status := fixture.host.Status()
		return status.State == protocol.ServiceStateLocked &&
			status.Generation > generation &&
			service.CoreForTesting(fixture.host) == nil &&
			service.PendingOwnersForTesting(fixture.host) == 0
	})
	expectControlEndedB04c3c(t, preparedA.hello.wire, "core A control")
	expectControlEndedB04c3c(t, preparedB.hello.wire, "pending B control")
	expectQueryFailsB04c3c(t, ctx, committedA.data, preparedA.signer, "core A signer")

	release := acquireHostCoreLifetimeDir(t, fixture.dir)
	release()

	freshPrepared := prepareB04c3c(t, ctx, fixture, "prepare-fresh-core")
	freshCommitted := commitB04c3c(t, fixture, freshPrepared)
	freshCore := service.CoreForTesting(fixture.host)
	if freshCore == nil || freshCore == coreA {
		t.Fatal("fresh commit did not publish a different core")
	}
	if _, err := freshCommitted.data.ListSnippets(ctx, freshPrepared.signer); err != nil {
		t.Fatalf("fresh owner query failed: %v", err)
	}

	coreA.Close()
	if got := service.CoreForTesting(fixture.host); got != freshCore {
		t.Fatal("old core Close removed the fresh core")
	}
	if status := fixture.host.Status(); status.State != protocol.ServiceStateUnlocked {
		t.Fatalf("fresh host state after old core Close=%q", status.State)
	}

	closed := make(chan error, 1)
	go func() { closed <- fixture.host.Close() }()
	select {
	case err := <-closed:
		if err != nil {
			t.Fatalf("host close failed: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("host close did not return")
	}
}
