package client

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"sshclient/internal/broker"
	"sshclient/internal/localipc"
	"sshclient/internal/protocol"
	"sshclient/internal/service"
)

const (
	ownerControlTestBound  = 2 * time.Second
	ownerControlStateBound = 5 * time.Second
)

type ownerControlTestResult struct {
	status protocol.ServiceStatus
	err    error
}

type ownerControlTestCall func(context.Context) (protocol.ServiceStatus, error)

func callOwnerControlBounded(t *testing.T, ctx context.Context, call ownerControlTestCall) ownerControlTestResult {
	t.Helper()
	result := make(chan ownerControlTestResult, 1)
	go func() {
		status, err := call(ctx)
		result <- ownerControlTestResult{status: status, err: err}
	}()
	select {
	case result := <-result:
		return result
	case <-time.After(ownerControlTestBound):
		t.Fatal("owner control call did not return within 2 seconds")
		return ownerControlTestResult{}
	}
}

func requireOwnerControlStatus(t *testing.T, status protocol.ServiceStatus, state string, generation uint64, minor int) {
	t.Helper()
	if status.State != state || status.Generation != generation {
		t.Fatalf("control status = %q generation %d, want %q/%d", status.State, status.Generation, state, generation)
	}
	if status.ProtocolMajor != protocol.ServiceProtocolMajor || status.ProtocolMinor != minor {
		t.Fatalf("control status protocol = %d.%d, want %d.%d", status.ProtocolMajor, status.ProtocolMinor, protocol.ServiceProtocolMajor, minor)
	}
	if status.StartedAt.IsZero() {
		t.Fatal("control status has no start time")
	}
}

func waitOwnerControlHostDone(t *testing.T, host *service.Host, timeout time.Duration) {
	t.Helper()
	select {
	case <-host.Done():
	case <-time.After(timeout):
		t.Fatalf("host Done did not close within %v", timeout)
	}
}

func closeOwnerControlHostBounded(t *testing.T, host *service.Host) {
	t.Helper()
	closed := make(chan struct{})
	go func() {
		_ = host.Close()
		close(closed)
	}()
	select {
	case <-closed:
	case <-time.After(ownerControlStateBound):
		t.Fatal("host Close did not return within 5 seconds")
	}
}

func requireDirectoryAcquirable(t *testing.T, dir string) {
	t.Helper()
	release, err := localipc.Acquire(dir)
	if err != nil {
		t.Fatalf("directory lock remained held for %s: %v", dir, err)
	}
	release()
}

func TestOwnerSessionLockRevokesOwnersAndKeepsServiceLocked(t *testing.T) {
	fixture := newPrepareSDKFixture(t)
	preparedA, sessionA := ownerSessionTestPrepareAndCommit(t, fixture)
	preparedB, sessionB := ownerSessionTestPrepareAndCommit(t, fixture)
	authCtxC, cancelAuthC := context.WithCancel(context.Background())
	t.Cleanup(cancelAuthC)
	preparedC := ownerSessionTestPrepare(t, fixture, authCtxC)
	t.Cleanup(preparedA.Close)
	t.Cleanup(preparedB.Close)
	t.Cleanup(preparedC.Close)
	t.Cleanup(sessionA.Close)
	t.Cleanup(sessionB.Close)
	requireOwnerSessionListSnippets(t, fixture, sessionA)
	requireOwnerSessionListSnippets(t, fixture, sessionB)

	oldGeneration := fixture.host.Status().Generation
	ctx, cancel := context.WithTimeout(context.Background(), ownerControlStateBound)
	result := callOwnerControlBounded(t, ctx, sessionA.Lock)
	cancel()
	if result.err != nil {
		t.Fatal(result.err)
	}
	requireOwnerControlStatus(t, result.status, protocol.ServiceStateLocked, oldGeneration+1, sessionA.minor)

	waitOwnerSessionClosed(t, sessionA, ownerControlTestBound)
	waitOwnerSessionClosed(t, sessionB, ownerControlTestBound)
	requireOwnerSessionProofFails(t, sessionA)
	requireOwnerSessionProofFails(t, sessionB)
	requireOwnerSessionListSnippetsFails(t, fixture, sessionA)
	requireOwnerSessionListSnippetsFails(t, fixture, sessionB)

	commitCtx, cancelCommit := context.WithTimeout(context.Background(), ownerControlStateBound)
	pendingSession, commitErr := preparedC.Commit(commitCtx)
	cancelCommit()
	if pendingSession != nil {
		pendingSession.Close()
	}
	if commitErr == nil {
		t.Fatal("pending owner commit succeeded after owner lock")
	}
	waitPreparedClosed(t, preparedC)
	requireNoPrepareBrokerListener(t, fixture.dir)
	requireOwnerSessionHostState(t, fixture, protocol.ServiceStateLocked)
	requireDirectoryAcquirable(t, fixture.dir)

	freshPrepared1, _ := ownerSessionTestPrepareLive(t, fixture)
	t.Cleanup(freshPrepared1.Close)
	freshPrepared2, _ := ownerSessionTestPrepareLive(t, fixture)
	t.Cleanup(freshPrepared2.Close)
	freshPrepared2.Close()
	freshSession := ownerSessionTestCommit(t, freshPrepared1)
	t.Cleanup(freshSession.Close)
	requireOwnerSessionListSnippets(t, fixture, freshSession)
}

func TestOwnerSessionStopAcknowledgesThenReleasesService(t *testing.T) {
	fixture := newPrepareSDKFixture(t)
	preparedA, sessionA := ownerSessionTestPrepareAndCommit(t, fixture)
	preparedB, sessionB := ownerSessionTestPrepareAndCommit(t, fixture)
	t.Cleanup(preparedA.Close)
	t.Cleanup(preparedB.Close)
	t.Cleanup(sessionA.Close)
	t.Cleanup(sessionB.Close)

	oldGeneration := fixture.host.Status().Generation
	ctx, cancel := context.WithTimeout(context.Background(), ownerControlStateBound)
	result := callOwnerControlBounded(t, ctx, sessionA.Stop)
	cancel()
	if result.err != nil {
		t.Fatal(result.err)
	}
	requireOwnerControlStatus(t, result.status, protocol.ServiceStateStopping, oldGeneration+1, sessionA.minor)

	waitOwnerSessionClosed(t, sessionA, ownerControlTestBound)
	waitOwnerSessionClosed(t, sessionB, ownerControlTestBound)
	waitOwnerControlHostDone(t, fixture.host, ownerControlStateBound)
	requireDirectoryAcquirable(t, fixture.dir)
	requireDirectoryAcquirable(t, filepath.Join(fixture.dir, "service"))

	reopened, err := service.Open(fixture.dir)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = reopened.Close() })
	if status := reopened.Status(); status.State != protocol.ServiceStateLocked {
		t.Fatalf("reopened host state = %q, want %q", status.State, protocol.ServiceStateLocked)
	}
	requireOwnerSessionProofFails(t, sessionA)
	requireOwnerSessionProofFails(t, sessionB)
	requireOwnerSessionListSnippetsFails(t, fixture, sessionA)
	requireOwnerSessionListSnippetsFails(t, fixture, sessionB)
}

func TestOwnerSessionLockReportsAuditFailureWithLockedStatus(t *testing.T) {
	fixture := newPrepareSDKFixture(t)
	prepared, session := ownerSessionTestPrepareAndCommit(t, fixture)
	t.Cleanup(prepared.Close)
	t.Cleanup(session.Close)
	requireOwnerSessionListSnippets(t, fixture, session)

	vaultPath := broker.VaultPath(fixture.dir)
	backupPath := vaultPath + ".sdk-control-audit-backup"
	if err := os.Rename(vaultPath, backupPath); err != nil {
		t.Fatalf("could not move synthetic vault aside: %v", err)
	}
	t.Cleanup(func() {
		if err := os.Remove(vaultPath); err != nil && !os.IsNotExist(err) {
			t.Errorf("could not remove synthetic audit-failure path: %v", err)
		}
		if err := os.Rename(backupPath, vaultPath); err != nil {
			t.Errorf("could not restore synthetic vault: %v", err)
		}
	})
	if err := os.Mkdir(vaultPath, 0700); err != nil {
		t.Fatalf("could not create synthetic audit-failure path: %v", err)
	}

	oldGeneration := fixture.host.Status().Generation
	ctx, cancel := context.WithTimeout(context.Background(), ownerControlStateBound)
	result := callOwnerControlBounded(t, ctx, session.Lock)
	cancel()
	if result.err == nil {
		t.Fatal("owner lock hid the audit sealing failure")
	}
	if !strings.Contains(result.err.Error(), ownerControlAuditFailure) {
		t.Fatalf("owner lock error = %q, want %q", result.err, ownerControlAuditFailure)
	}
	requireOwnerControlStatus(t, result.status, protocol.ServiceStateLocked, oldGeneration+1, session.minor)

	waitOwnerSessionClosed(t, session, ownerControlTestBound)
	requireOwnerSessionProofFails(t, session)
	requireOwnerSessionListSnippetsFails(t, fixture, session)
	requireNoPrepareBrokerListener(t, fixture.dir)
	requireOwnerSessionHostState(t, fixture, protocol.ServiceStateLocked)
	requireDirectoryAcquirable(t, fixture.dir)
}

func TestOwnerSessionControlRejectsInvalidCallsAndHandlesEOF(t *testing.T) {
	t.Run("nil session", func(t *testing.T) {
		var session *OwnerSession
		if result := callOwnerControlBounded(t, nil, session.Lock); result.err == nil {
			t.Fatal("nil OwnerSession.Lock succeeded")
		}
		if result := callOwnerControlBounded(t, nil, session.Stop); result.err == nil {
			t.Fatal("nil OwnerSession.Stop succeeded")
		}
	})

	operations := []struct {
		name string
		call func(*OwnerSession) ownerControlTestCall
	}{
		{name: "lock", call: func(session *OwnerSession) ownerControlTestCall { return session.Lock }},
		{name: "stop", call: func(session *OwnerSession) ownerControlTestCall { return session.Stop }},
	}
	for _, operation := range operations {
		for _, invalidContext := range []struct {
			name string
			make func() (context.Context, context.CancelFunc)
		}{
			{name: "nil context", make: func() (context.Context, context.CancelFunc) { return nil, nil }},
			{name: "canceled context", make: func() (context.Context, context.CancelFunc) {
				ctx, cancel := context.WithCancel(context.Background())
				cancel()
				return ctx, nil
			}},
		} {
			t.Run(operation.name+"/"+invalidContext.name, func(t *testing.T) {
				fixture := newPrepareSDKFixture(t)
				preparedA, sessionA := ownerSessionTestPrepareAndCommit(t, fixture)
				preparedB, sessionB := ownerSessionTestPrepareAndCommit(t, fixture)
				t.Cleanup(preparedA.Close)
				t.Cleanup(preparedB.Close)
				t.Cleanup(sessionA.Close)
				t.Cleanup(sessionB.Close)

				ctx, cancel := invalidContext.make()
				result := callOwnerControlBounded(t, ctx, operation.call(sessionA))
				if cancel != nil {
					cancel()
				}
				if result.err == nil {
					t.Fatal("owner control succeeded with an invalid context")
				}
				waitOwnerSessionClosed(t, sessionA, ownerControlTestBound)
				requireOwnerSessionProofFails(t, sessionA)
				requireOwnerSessionListSnippets(t, fixture, sessionB)
				requireOwnerSessionHostState(t, fixture, protocol.ServiceStateUnlocked)
			})
		}
	}

	for _, operation := range operations {
		for iteration := 0; iteration < 3; iteration++ {
			t.Run(fmt.Sprintf("%s/eof/%d", operation.name, iteration+1), func(t *testing.T) {
				fixture := newPrepareSDKFixture(t)
				prepared, session := ownerSessionTestPrepareAndCommit(t, fixture)
				t.Cleanup(prepared.Close)
				t.Cleanup(session.Close)

				oldGeneration := fixture.host.Status().Generation
				ctx, cancel := context.WithTimeout(context.Background(), ownerControlStateBound)
				result := callOwnerControlBounded(t, ctx, operation.call(session))
				cancel()
				if result.err != nil {
					t.Fatal(result.err)
				}
				if operation.name == "lock" {
					requireOwnerControlStatus(t, result.status, protocol.ServiceStateLocked, oldGeneration+1, session.minor)
				} else {
					requireOwnerControlStatus(t, result.status, protocol.ServiceStateStopping, oldGeneration+1, session.minor)
				}
				waitOwnerSessionClosed(t, session, ownerControlTestBound)
				if operation.name == "lock" {
					closeOwnerControlHostBounded(t, fixture.host)
				} else {
					waitOwnerControlHostDone(t, fixture.host, ownerControlStateBound)
				}
				requireDirectoryAcquirable(t, fixture.dir)
				requireDirectoryAcquirable(t, filepath.Join(fixture.dir, "service"))
			})
		}
	}
}
