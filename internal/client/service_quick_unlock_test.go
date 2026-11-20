package client

import (
	"bytes"
	"context"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"sshclient/internal/localipc"
	"sshclient/internal/protocol"
)

func TestQuickUnlockLivesInServiceMemoryAcrossLocks(t *testing.T) {
	fixture := newPrepareSDKFixture(t)
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	if state, err := fixture.sdk.QuickUnlockStatus(ctx); err != nil || state.Available {
		t.Fatal("a fresh service offered quick unlock", state, err)
	}
	if _, err := fixture.sdk.QuickUnlockRecord(ctx); !errors.Is(err, ErrQuickUnlockUnavailable) {
		t.Fatal("record without deposit", err)
	}

	prepared, err := fixture.sdk.PrepareOwner(ctx, fixture.source, 0)
	if err != nil {
		t.Fatal(err)
	}
	owner, err := prepared.Commit(ctx)
	prepared.Close()
	if err != nil {
		t.Fatal(err)
	}
	envelope := []byte(`{"version":1,"vaultHash":"synthetic","data":"opaque"}`)
	// A rejected deposit answers on the owner connection without ending it.
	if _, err := owner.DepositQuickUnlock(ctx, bytes.Repeat([]byte("x"), 65<<10)); err == nil {
		t.Fatal("oversized envelope accepted")
	}
	select {
	case <-owner.Done():
		t.Fatal("a rejected deposit closed the owner session")
	default:
	}
	state, err := owner.DepositQuickUnlock(ctx, envelope)
	if err != nil || !state.Available || time.Until(state.Expires) < 47*time.Hour || time.Until(state.Expires) > 49*time.Hour {
		t.Fatal("deposit", state, err)
	}
	if _, err := owner.Lock(ctx); err != nil {
		t.Fatal(err)
	}
	if fixture.host.Status().State != protocol.ServiceStateLocked {
		t.Fatal("lock did not close the core")
	}
	// The envelope is the point of quick unlock: it outlives the lock.
	got, err := fixture.sdk.QuickUnlockRecord(ctx)
	if err != nil || !bytes.Equal(got, envelope) {
		t.Fatal("envelope lost across lock", string(got), err)
	}
	clear(got)
	if state, err := fixture.sdk.QuickUnlockStatus(ctx); err != nil || !state.Available {
		t.Fatal("status after lock", state, err)
	}
	if err := fixture.sdk.ClearQuickUnlock(ctx); err != nil {
		t.Fatal(err)
	}
	if _, err := fixture.sdk.QuickUnlockRecord(ctx); !errors.Is(err, ErrQuickUnlockUnavailable) {
		t.Fatal("cleared envelope still returned", err)
	}
}

func TestQuickUnlockDepositNeedsAnOwnerConnection(t *testing.T) {
	fixture := newPrepareSDKFixture(t)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	wire, err := localipc.ConnectExecutable(ctx, filepath.Join(fixture.dir, "service"), fixture.sdk.Executable)
	if err != nil {
		t.Fatal(err)
	}
	defer wire.Close()
	hello, err := exchangeServiceHello(ctx, wire)
	if err != nil {
		t.Fatal(err)
	}
	if err := serviceSend(ctx, wire, protocol.ServiceMessage{
		Action: "depositQuickUnlock", Major: protocol.ServiceProtocolMajor, Minor: hello.Minor, Epoch: hello.Epoch,
		QuickUnlock: []byte("planted envelope"),
	}); err != nil {
		t.Fatal(err)
	}
	var response protocol.ServiceMessage
	if err := serviceReceive(ctx, wire, &response); err != nil {
		t.Fatal(err)
	}
	if response.ErrorCode != "owner_required" {
		t.Fatal("a connection without an owner session deposited", response.Action, response.ErrorCode)
	}
	if state, err := fixture.sdk.QuickUnlockStatus(ctx); err != nil || state.Available {
		t.Fatal("planted envelope became available", state, err)
	}
}
