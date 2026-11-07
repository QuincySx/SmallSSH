package service_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"sshclient/internal/broker"
	"sshclient/internal/client"
	"sshclient/internal/localipc"
	"sshclient/internal/protocol"
	"sshclient/internal/service"
)

func expectAcquirableB04c3d(t *testing.T, dirs ...string) {
	t.Helper()
	for _, dir := range dirs {
		release, err := localipc.Acquire(dir)
		if err != nil {
			t.Fatalf("directory lock remained held for %s: %v", dir, err)
		}
		release()
	}
}

func waitServiceReadyB04c3d(t *testing.T, ctx context.Context, dir string, runResult <-chan error) protocol.ServiceStatus {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()
	for {
		statusCtx, cancel := context.WithTimeout(ctx, time.Second)
		status, err := (client.ServiceClient{Dir: dir, Executable: b03bExecutable(t)}).Status(statusCtx)
		cancel()
		if err == nil {
			if status.State != protocol.ServiceStateLocked {
				t.Fatalf("run service ready state=%q, want locked", status.State)
			}
			return status
		}
		if !time.Now().Before(deadline) {
			t.Fatalf("run service did not become ready: %v", err)
		}
		select {
		case runErr := <-runResult:
			t.Fatalf("service.Run exited before becoming ready: %v", runErr)
		case <-ticker.C:
		}
	}
}

func TestOwnerStopAcknowledgesThenReleasesServiceAndCore(t *testing.T) {
	fixture := newB04c2b1Fixture(t)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	preparedA := prepareB04c3c(t, ctx, fixture, "prepare-a")
	committedA := commitB04c3c(t, fixture, preparedA)
	if _, err := committedA.data.ListSnippets(ctx, preparedA.signer); err != nil {
		t.Fatalf("initial owner A query failed: %v", err)
	}
	preparedB := prepareB04c3c(t, ctx, fixture, "prepare-pending-b")
	if pending := service.PendingOwnersForTesting(fixture.host); pending != 1 {
		t.Fatalf("pending owners before owner stop=%d, want 1", pending)
	}

	oldGeneration := preparedA.hello.generation
	stopMessage := b04c3dControlMessage(
		t, preparedA.hello, committedA, preparedA.signer, "stop",
		b04c3dControl(preparedA.hello, committedA, "stop"),
	)
	defer clear(stopMessage.Proof)
	accepted := exchangeB04c2b1(t, "owner stop", preparedA.hello.wire, stopMessage)
	if accepted.Action != "stopAccepted" || accepted.ErrorCode != "" || accepted.Status == nil {
		t.Fatalf("owner stop: action=%q errorCode=%q statusPresent=%t", accepted.Action, accepted.ErrorCode, accepted.Status != nil)
	}
	if accepted.Epoch != preparedA.hello.epoch || accepted.Generation != oldGeneration+1 || accepted.Status.Generation != accepted.Generation {
		t.Fatalf("owner stop header: epochMatches=%t generation=%d statusGeneration=%d", accepted.Epoch == preparedA.hello.epoch, accepted.Generation, accepted.Status.Generation)
	}
	if accepted.Status.State != protocol.ServiceStateStopping || len(accepted.UnlockKey) != 0 || len(accepted.Proof) != 0 {
		t.Fatalf("owner stop payload: state=%q unlockKey=%d proof=%d", accepted.Status.State, len(accepted.UnlockKey), len(accepted.Proof))
	}

	select {
	case <-fixture.host.Done():
	case <-time.After(5 * time.Second):
		t.Fatal("owner stop did not close the host")
	}
	if pending := service.PendingOwnersForTesting(fixture.host); pending != 0 {
		t.Fatalf("pending owners after owner stop=%d, want 0", pending)
	}
	if core := service.CoreForTesting(fixture.host); core != nil {
		t.Fatal("owner stop left the broker core published")
	}
	expectControlEndedB04c3c(t, preparedB.hello.wire, "owner B control after owner stop")
	expectQueryFailsB04c3c(t, ctx, committedA.data, preparedA.signer, "owner A after owner stop")
	expectAcquirableB04c3d(t, fixture.dir, filepath.Join(fixture.dir, "service"))

	restarted, err := service.Open(fixture.dir)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = restarted.Close() })
	if status := restarted.Status(); status.State != protocol.ServiceStateLocked {
		t.Fatalf("restarted host state=%q, want locked", status.State)
	}
	restartedHello := helloB04c2b1(t, ctx, fixture.dir)
	if restartedHello.epoch == preparedA.hello.epoch {
		t.Fatal("restarted host reused the stopped service epoch")
	}
	if status := statusB04c2b1(t, restartedHello.wire, restartedHello); status.State != protocol.ServiceStateLocked {
		t.Fatalf("restarted wire state=%q, want locked", status.State)
	}
}

func TestServiceRunReturnsNormallyAfterAuthenticatedStop(t *testing.T) {
	fixture := newB04c2b1Fixture(t)
	dir, source := fixture.dir, fixture.source
	if err := fixture.host.Close(); err != nil {
		t.Fatalf("initial fixture host close failed: %v", err)
	}

	runCtx, runCancel := context.WithCancel(context.Background())
	defer runCancel()
	runResult := make(chan error, 1)
	go func() {
		runResult <- service.Run(runCtx, dir)
	}()
	if status := waitServiceReadyB04c3d(t, runCtx, dir, runResult); status.State != protocol.ServiceStateLocked {
		t.Fatalf("run service ready state=%q, want locked", status.State)
	}
	select {
	case err := <-runResult:
		t.Fatalf("service.Run exited after readiness: %v", err)
	default:
	}

	fixture = b04c2b1Fixture{dir: dir, source: source}
	prepared := prepareB04c3c(t, runCtx, fixture, "prepare-run-owner")
	committed := commitB04c3c(t, fixture, prepared)
	if _, err := committed.data.ListSnippets(runCtx, prepared.signer); err != nil {
		t.Fatalf("run owner query failed: %v", err)
	}

	oldGeneration := prepared.hello.generation
	stopMessage := b04c3dControlMessage(
		t, prepared.hello, committed, prepared.signer, "stop",
		b04c3dControl(prepared.hello, committed, "stop"),
	)
	defer clear(stopMessage.Proof)
	accepted := exchangeB04c2b1(t, "run owner stop", prepared.hello.wire, stopMessage)
	if accepted.Action != "stopAccepted" || accepted.ErrorCode != "" || accepted.Status == nil {
		t.Fatalf("run owner stop: action=%q errorCode=%q statusPresent=%t", accepted.Action, accepted.ErrorCode, accepted.Status != nil)
	}
	if accepted.Epoch != prepared.hello.epoch || accepted.Generation != oldGeneration+1 || accepted.Status.Generation != accepted.Generation {
		t.Fatalf("run owner stop header: epochMatches=%t generation=%d statusGeneration=%d", accepted.Epoch == prepared.hello.epoch, accepted.Generation, accepted.Status.Generation)
	}
	if accepted.Status.State != protocol.ServiceStateStopping || len(accepted.UnlockKey) != 0 || len(accepted.Proof) != 0 {
		t.Fatalf("run owner stop payload: state=%q unlockKey=%d proof=%d", accepted.Status.State, len(accepted.UnlockKey), len(accepted.Proof))
	}

	select {
	case err := <-runResult:
		if err != nil {
			t.Fatalf("service.Run returned error after authenticated stop: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("service.Run did not return within five seconds of owner stop")
	}
	expectAcquirableB04c3d(t, dir, filepath.Join(dir, "service"))
}

func TestOwnerLockReportsAuditFailureButRevokesAuthority(t *testing.T) {
	fixture := newB04c2b1Fixture(t)
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()

	prepared := prepareB04c3c(t, ctx, fixture, "prepare-audit-failure")
	committed := commitB04c3c(t, fixture, prepared)
	if _, err := committed.data.ListSnippets(ctx, prepared.signer); err != nil {
		t.Fatalf("initial owner query failed: %v", err)
	}

	vaultPath := broker.VaultPath(fixture.dir)
	backupPath := vaultPath + ".b04c3d-backup"
	if err := os.Rename(vaultPath, backupPath); err != nil {
		t.Fatalf("could not move synthetic vault aside: %v", err)
	}
	t.Cleanup(func() {
		if err := os.Remove(vaultPath); err != nil && !os.IsNotExist(err) {
			t.Errorf("could not remove audit-failure directory: %v", err)
		}
		if err := os.Rename(backupPath, vaultPath); err != nil {
			t.Errorf("could not restore synthetic vault: %v", err)
		}
	})
	if err := os.Mkdir(vaultPath, 0700); err != nil {
		t.Fatalf("could not create audit-failure directory: %v", err)
	}

	oldGeneration := fixture.host.Status().Generation
	lockMessage := b04c3dControlMessage(
		t, prepared.hello, committed, prepared.signer, "lock",
		b04c3dControl(prepared.hello, committed, "lock"),
	)
	defer clear(lockMessage.Proof)
	locked := exchangeB04c2b1(t, "owner lock with audit failure", prepared.hello.wire, lockMessage)
	if locked.Action != "locked" || locked.Status == nil {
		t.Fatalf("owner lock with audit failure: action=%q statusPresent=%t", locked.Action, locked.Status != nil)
	}
	if locked.ErrorCode != "audit_unavailable" || locked.Error != "已锁定，但审计封存失败" {
		t.Fatalf("owner lock with audit failure: errorCode=%q genericErrorMatches=%t", locked.ErrorCode, locked.Error == "已锁定，但审计封存失败")
	}
	if locked.Epoch != prepared.hello.epoch || locked.Generation != oldGeneration+1 || locked.Status.Generation != locked.Generation || locked.Status.State != protocol.ServiceStateLocked {
		t.Fatalf("owner lock with audit failure header: epochMatches=%t generation=%d statusGeneration=%d state=%q", locked.Epoch == prepared.hello.epoch, locked.Generation, locked.Status.Generation, locked.Status.State)
	}
	if len(locked.UnlockKey) != 0 || len(locked.Proof) != 0 {
		t.Fatalf("owner lock with audit failure exposed material: unlockKey=%d proof=%d", len(locked.UnlockKey), len(locked.Proof))
	}
	if pending := service.PendingOwnersForTesting(fixture.host); pending != 0 {
		t.Fatalf("pending owners after audit-failure lock=%d, want 0", pending)
	}
	if core := service.CoreForTesting(fixture.host); core != nil {
		t.Fatal("audit-failure lock left the broker core published")
	}
	expectQueryFailsB04c3c(t, ctx, committed.data, prepared.signer, "owner after audit-failure lock")
	expectAcquirableB04c3d(t, fixture.dir)

	statusCtx, statusCancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer statusCancel()
	sdkStatus, err := (client.ServiceClient{Dir: fixture.dir, Executable: b03bExecutable(t)}).Status(statusCtx)
	if err != nil || sdkStatus.State != protocol.ServiceStateLocked {
		t.Fatalf("SDK state after audit-failure lock=%q err=%v, want locked", sdkStatus.State, err)
	}
}
