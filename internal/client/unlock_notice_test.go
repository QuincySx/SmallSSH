package client

import (
	"context"
	"errors"
	"testing"
	"time"

	"sshclient/internal/approval"
	"sshclient/internal/protocol"
)

func TestLockedServerRequestSignalsUnlockWithoutCreatingAnApproval(t *testing.T) {
	f := newPrepareSDKFixture(t)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	c := Client{Dir: f.dir, ServiceExecutable: f.sdk.Executable}
	_, _ = c.Call(ctx, protocol.Message{Action: "hosts"})
	if status, err := f.sdk.Status(ctx); err != nil || status.UnlockNoticeUntil != nil {
		t.Fatalf("query queued an unlock notice: %#v %v", status, err)
	}
	request, ticket, err := c.Request(ctx, approval.Operation{Host: "fixture", Kind: "list", Path: "/"}, time.Minute)
	if !errors.Is(err, ErrVaultLocked) || request.ID != "" || ticket != "" {
		t.Fatalf("locked request returned an approval or the wrong error: id=%q err=%v", request.ID, err)
	}
	status, err := f.sdk.Status(ctx)
	if err != nil || status.State != protocol.ServiceStateLocked || status.UnlockNoticeUntil == nil {
		t.Fatalf("desktop cannot observe the notice: %#v %v", status, err)
	}
	_, _, err = c.RequestSession(ctx, "fixture", "AI", "guarded", time.Hour, 10*time.Minute)
	if !errors.Is(err, ErrVaultLocked) {
		t.Fatalf("session request did not report the lock: %v", err)
	}
	if got := f.host.Status().UnlockNoticeUntil; got == nil || !got.Equal(*status.UnlockNoticeUntil) {
		t.Fatalf("repeat requests did not coalesce: %v", got)
	}
	requireNoPrepareBrokerListener(t, f.dir)
	prepared, err := f.sdk.PrepareOwner(ctx, f.source, 0)
	if err != nil {
		t.Fatal(err)
	}
	defer prepared.Close()
	owner, err := prepared.Commit(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer owner.Close()
	if locked, err := f.sdk.RequestUnlock(ctx); locked || err != nil || f.host.Status().UnlockNoticeUntil != nil {
		t.Fatalf("unlock notice survived unlock: locked=%v err=%v", locked, err)
	}
	if status, err := owner.Lock(ctx); err != nil || status.UnlockNoticeUntil != nil || f.host.Status().UnlockNoticeUntil != nil {
		t.Fatalf("unlock notice survived into the next lock: %#v %v", status, err)
	}
}
