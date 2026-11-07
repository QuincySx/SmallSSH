package service_test

import (
	"context"
	"testing"
	"time"

	"sshclient/internal/approval"
	"sshclient/internal/broker"
	"sshclient/internal/client"
	"sshclient/internal/localipc"
	"sshclient/internal/protocol"
	"sshclient/internal/service"
	"sshclient/internal/testserver"
)

type b04c3dProofSource interface {
	Proof([]byte) ([]byte, error)
}

func b04c3dControl(hello b04c2b1Hello, committed b04c3cCommitted, action string) protocol.ServiceOwnerControl {
	return protocol.ServiceOwnerControl{
		Action:     action,
		Epoch:      hello.epoch,
		Generation: hello.generation,
		Binding:    hello.binding,
		OwnerID:    committed.info.ID,
	}
}

func b04c3dControlMessage(
	t *testing.T,
	hello b04c2b1Hello,
	committed b04c3cCommitted,
	source b04c3dProofSource,
	wireAction string,
	control protocol.ServiceOwnerControl,
) protocol.ServiceMessage {
	t.Helper()
	payload, err := protocol.ServiceOwnerControlPayload(control)
	if err != nil {
		t.Fatal(err)
	}
	proof, err := source.Proof(payload)
	if err != nil {
		t.Fatal(err)
	}
	return protocol.ServiceMessage{
		Action:     wireAction,
		Major:      protocol.ServiceProtocolMajor,
		Minor:      hello.minor,
		Epoch:      hello.epoch,
		Generation: hello.generation,
		ID:         committed.info.ID,
		Proof:      proof,
	}
}

func expectRejectedControlB04c3d(t *testing.T, label string, wire *localipc.Wire, message protocol.ServiceMessage) {
	t.Helper()
	response := exchangeB04c2b1(t, label, wire, message)
	if response.Action != "error" || response.ErrorCode == "" {
		t.Fatalf("%s: action=%q errorCode=%q", label, response.Action, response.ErrorCode)
	}
}

func expectRootUnlockedB04c3d(t *testing.T, ctx context.Context, fixture b04c2b1Fixture, brokerKey string) {
	t.Helper()
	root := client.Client{Dir: fixture.dir, BrokerKey: brokerKey, Owner: fixture.source}
	if _, err := root.ListSnippets(ctx, fixture.source); err != nil {
		t.Fatalf("root query after rejected control failed: %v", err)
	}
	if status := fixture.host.Status(); status.State != protocol.ServiceStateUnlocked {
		t.Fatalf("host state after rejected control=%q, want unlocked", status.State)
	}
}

func TestOwnerControlRejectsInvalidAuthorityWithoutGlobalEffects(t *testing.T) {
	type controlCase struct {
		name    string
		message func(*testing.T, b04c2b1Fixture, b04c3cPrepared, b04c3cCommitted) protocol.ServiceMessage
	}
	cases := []controlCase{
		{
			name: "wire-action-differs-from-signed-action",
			message: func(t *testing.T, _ b04c2b1Fixture, prepared b04c3cPrepared, committed b04c3cCommitted) protocol.ServiceMessage {
				control := b04c3dControl(prepared.hello, committed, "lock")
				return b04c3dControlMessage(t, prepared.hello, committed, prepared.signer, "stop", control)
			},
		},
		{
			name: "signed-binding-is-forged",
			message: func(t *testing.T, _ b04c2b1Fixture, prepared b04c3cPrepared, committed b04c3cCommitted) protocol.ServiceMessage {
				control := b04c3dControl(prepared.hello, committed, "lock")
				control.Binding = "synthetic-forged-control-binding"
				return b04c3dControlMessage(t, prepared.hello, committed, prepared.signer, "lock", control)
			},
		},
		{
			name: "generation-bumped-in-payload-and-wire",
			message: func(t *testing.T, _ b04c2b1Fixture, prepared b04c3cPrepared, committed b04c3cCommitted) protocol.ServiceMessage {
				control := b04c3dControl(prepared.hello, committed, "lock")
				control.Generation++
				message := b04c3dControlMessage(t, prepared.hello, committed, prepared.signer, "lock", control)
				message.Generation = control.Generation
				return message
			},
		},
		{
			name: "root-hmac-substituted-for-owner-envelope",
			message: func(t *testing.T, fixture b04c2b1Fixture, prepared b04c3cPrepared, committed b04c3cCommitted) protocol.ServiceMessage {
				control := b04c3dControl(prepared.hello, committed, "lock")
				return b04c3dControlMessage(t, prepared.hello, committed, fixture.source, "lock", control)
			},
		},
		{
			name: "owner-id-changed-in-payload-and-wire",
			message: func(t *testing.T, _ b04c2b1Fixture, prepared b04c3cPrepared, committed b04c3cCommitted) protocol.ServiceMessage {
				control := b04c3dControl(prepared.hello, committed, "lock")
				control.OwnerID = "synthetic-other-owner-id"
				message := b04c3dControlMessage(t, prepared.hello, committed, prepared.signer, "lock", control)
				message.ID = control.OwnerID
				return message
			},
		},
	}

	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			fixture := newB04c2b1Fixture(t)
			ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
			defer cancel()
			prepared := prepareB04c3c(t, ctx, fixture, "prepare-a")
			committed := commitB04c3c(t, fixture, prepared)
			if _, err := committed.data.ListSnippets(ctx, prepared.signer); err != nil {
				t.Fatalf("initial owner query failed: %v", err)
			}

			message := testCase.message(t, fixture, prepared, committed)
			expectRejectedControlB04c3d(t, testCase.name, prepared.hello.wire, message)
			expectRootUnlockedB04c3d(t, ctx, fixture, committed.data.BrokerKey)
		})
	}

	t.Run("signed-control-cannot-move-to-another-connection", func(t *testing.T) {
		fixture := newB04c2b1Fixture(t)
		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		defer cancel()
		preparedA := prepareB04c3c(t, ctx, fixture, "prepare-a")
		committedA := commitB04c3c(t, fixture, preparedA)
		if _, err := committedA.data.ListSnippets(ctx, preparedA.signer); err != nil {
			t.Fatalf("initial owner A query failed: %v", err)
		}

		helloB := helloB04c2b1(t, ctx, fixture.dir)
		control := b04c3dControl(helloB, committedA, "lock")
		message := b04c3dControlMessage(t, helloB, committedA, preparedA.signer, "lock", control)
		expectRejectedControlB04c3d(t, "cross-connection lock", helloB.wire, message)

		if _, err := committedA.data.ListSnippets(ctx, preparedA.signer); err != nil {
			t.Fatalf("owner A query after cross-connection lock failed: %v", err)
		}
		if status := fixture.host.Status(); status.State != protocol.ServiceStateUnlocked {
			t.Fatalf("host state after cross-connection lock=%q, want unlocked", status.State)
		}
	})
}

func TestOwnerLockClosesCoreAndAllowsFreshAuthentication(t *testing.T) {
	fixture := newB04c2b1Fixture(t)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	server := testserver.New(t)
	profile := server.Profile()
	if err := fixture.source.SaveProfile(broker.VaultPath(fixture.dir), profile); err != nil {
		t.Fatal(err)
	}

	preparedA := prepareB04c3c(t, ctx, fixture, "prepare-a")
	committedA := commitB04c3c(t, fixture, preparedA)
	preparedB := prepareB04c3c(t, ctx, fixture, "prepare-b")
	committedB := commitB04c3c(t, fixture, preparedB)
	if _, err := committedA.data.ListSnippets(ctx, preparedA.signer); err != nil {
		t.Fatalf("initial owner A query failed: %v", err)
	}

	foreign := client.Client{Dir: fixture.dir, BrokerKey: committedA.data.BrokerKey}
	foreignRequest, foreignTicket, err := foreign.Request(ctx, approval.Operation{
		Host: profile.ID, Kind: "exec", Command: "printf blocked-after-lock", Client: "synthetic foreign",
	}, time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	root := client.Client{Dir: fixture.dir, BrokerKey: committedA.data.BrokerKey, Owner: fixture.source}
	if err := root.DecideReviewedWithOwner(ctx, foreignRequest, true, false, fixture.source); err != nil {
		t.Fatalf("root approval failed: %v", err)
	}

	oldGeneration := fixture.host.Status().Generation
	lockMessage := b04c3dControlMessage(
		t, preparedA.hello, committedA, preparedA.signer, "lock",
		b04c3dControl(preparedA.hello, committedA, "lock"),
	)
	locked := exchangeB04c2b1(t, "owner lock", preparedA.hello.wire, lockMessage)
	if locked.Action != "locked" || locked.ErrorCode != "" || locked.Status == nil {
		t.Fatalf("owner lock: action=%q errorCode=%q statusPresent=%t", locked.Action, locked.ErrorCode, locked.Status != nil)
	}
	if locked.Epoch != preparedA.hello.epoch || locked.Generation != oldGeneration+1 || locked.Status.Generation != locked.Generation {
		t.Fatalf("owner lock header: epochMatches=%t generation=%d statusGeneration=%d", locked.Epoch == preparedA.hello.epoch, locked.Generation, locked.Status.Generation)
	}
	if locked.Status.State != protocol.ServiceStateLocked || len(locked.UnlockKey) != 0 || len(locked.Proof) != 0 {
		t.Fatalf("owner lock payload: state=%q unlockKey=%d proof=%d", locked.Status.State, len(locked.UnlockKey), len(locked.Proof))
	}
	if core := service.CoreForTesting(fixture.host); core != nil {
		t.Fatal("owner lock left the broker core published")
	}
	if pending := service.PendingOwnersForTesting(fixture.host); pending != 0 {
		t.Fatalf("pending owners after owner lock=%d, want 0", pending)
	}
	expectControlEndedB04c3c(t, preparedB.hello.wire, "owner B control after owner lock")
	expectQueryFailsB04c3c(t, ctx, committedA.data, preparedA.signer, "owner A after owner lock")
	expectQueryFailsB04c3c(t, ctx, committedB.data, preparedB.signer, "owner B after owner lock")

	startCtx, startCancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer startCancel()
	if stream, err := foreign.Start(startCtx, foreignRequest.ID, foreignTicket, 80, 24); err == nil {
		_ = stream.Close()
		t.Fatal("pre-lock foreign ticket started after owner lock")
	}

	release, err := localipc.Acquire(fixture.dir)
	if err != nil {
		t.Fatalf("broker directory lock remained held after owner lock: %v", err)
	}
	release()

	statusCtx, statusCancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer statusCancel()
	sdkStatus, err := (client.ServiceClient{Dir: fixture.dir, Executable: b03bExecutable(t)}).Status(statusCtx)
	if err != nil || sdkStatus.State != protocol.ServiceStateLocked {
		t.Fatalf("SDK state after owner lock=%q err=%v, want locked", sdkStatus.State, err)
	}

	preparedC := prepareB04c3c(t, ctx, fixture, "prepare-c")
	if preparedC.hello.generation != sdkStatus.Generation || preparedC.hello.generation == preparedA.hello.generation {
		t.Fatalf("fresh owner hello generation=%d, want new generation %d", preparedC.hello.generation, sdkStatus.Generation)
	}
	committedC := commitB04c3c(t, fixture, preparedC)
	expectQueryFailsB04c3c(t, ctx, committedA.data, preparedA.signer, "old owner signer against fresh core")
	if _, err := committedC.data.ListSnippets(ctx, preparedC.signer); err != nil {
		t.Fatalf("fresh owner C query failed: %v", err)
	}
}

func TestTrustedHostLockCancelsUncommittedOwner(t *testing.T) {
	fixture := newB04c2b1Fixture(t)
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()

	prepared := prepareB04c3c(t, ctx, fixture, "prepare-pending")
	if pending := service.PendingOwnersForTesting(fixture.host); pending != 1 {
		t.Fatalf("pending owners before trusted lock=%d, want 1", pending)
	}
	oldGeneration := fixture.host.Status().Generation
	if err := fixture.host.Lock(); err != nil {
		t.Fatalf("trusted host lock failed: %v", err)
	}
	status := fixture.host.Status()
	if status.State != protocol.ServiceStateLocked || status.Generation != oldGeneration+1 {
		t.Fatalf("trusted lock state=%q generation=%d, want locked/%d", status.State, status.Generation, oldGeneration+1)
	}
	if pending := service.PendingOwnersForTesting(fixture.host); pending != 0 {
		t.Fatalf("pending owners after trusted lock=%d, want 0", pending)
	}
	expectControlEndedB04c3c(t, prepared.hello.wire, "pending owner control after trusted lock")

	statusCtx, statusCancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer statusCancel()
	sdkStatus, err := (client.ServiceClient{Dir: fixture.dir, Executable: b03bExecutable(t)}).Status(statusCtx)
	if err != nil || sdkStatus.State != protocol.ServiceStateLocked {
		t.Fatalf("SDK state after trusted lock=%q err=%v, want locked", sdkStatus.State, err)
	}

	staleHello := helloB04c2b1(t, ctx, fixture.dir)
	stale := exchangeB04c2b1(t, "stale pending commit", staleHello.wire, b04c3cCommitMessage(staleHello, prepared.pendingID))
	if stale.Action != "error" || stale.ErrorCode == "" {
		t.Fatalf("stale pending commit: action=%q errorCode=%q", stale.Action, stale.ErrorCode)
	}

	freshPrepared := prepareB04c3c(t, ctx, fixture, "prepare-fresh")
	freshCommitted := commitB04c3c(t, fixture, freshPrepared)
	if _, err := freshCommitted.data.ListSnippets(ctx, freshPrepared.signer); err != nil {
		t.Fatalf("fresh owner query after trusted lock cleanup failed: %v", err)
	}
}
