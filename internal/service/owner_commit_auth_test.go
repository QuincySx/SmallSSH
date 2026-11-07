package service_test

import (
	"context"
	"testing"
	"time"

	"sshclient/internal/broker"
	"sshclient/internal/client"
	"sshclient/internal/localipc"
	"sshclient/internal/ownercap"
	"sshclient/internal/protocol"
	"sshclient/internal/service"
	"sshclient/internal/snippet"
	"sshclient/internal/vault"
)

type b04c3cPrepared struct {
	hello       b04c2b1Hello
	pendingID   string
	signer      *ownercap.Signer
	expectedPin string
}

type b04c3cCommitted struct {
	info ownercap.SessionInfo
	data client.Client
}

func prepareB04c3c(t *testing.T, ctx context.Context, fixture b04c2b1Fixture, label string) b04c3cPrepared {
	t.Helper()
	hello := helloB04c2b1(t, ctx, fixture.dir)
	signer, err := ownercap.NewSigner()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(signer.Close)
	message := newB04c2b1PrepareMessage(t, fixture.source, hello)
	defer clearB04c2b1Message(&message)
	if message.OwnerPrepare == nil {
		t.Fatalf("%s prepare message has no owner payload", label)
	}
	message.OwnerPrepare.PublicKey = signer.PublicKey()
	resignB04c2b1Message(t, fixture.source, &message)

	response := exchangeB04c2b1(t, label, hello.wire, message)
	if response.Action != "ownerPrepared" || response.ErrorCode != "" || response.ID == "" {
		t.Fatalf("%s: action=%q errorCode=%q idPresent=%t", label, response.Action, response.ErrorCode, response.ID != "")
	}
	expectedPin, err := fixture.source.BrokerPublicKeyPin()
	if err != nil {
		t.Fatal(err)
	}
	return b04c3cPrepared{
		hello:       hello,
		pendingID:   response.ID,
		signer:      signer,
		expectedPin: expectedPin,
	}
}

func b04c3cCommitMessage(hello b04c2b1Hello, pendingID string) protocol.ServiceMessage {
	return protocol.ServiceMessage{
		Action:     "commitOwner",
		Major:      protocol.ServiceProtocolMajor,
		Minor:      hello.minor,
		Epoch:      hello.epoch,
		Generation: hello.generation,
		ID:         pendingID,
	}
}

func commitB04c3c(t *testing.T, fixture b04c2b1Fixture, prepared b04c3cPrepared) b04c3cCommitted {
	t.Helper()
	response := exchangeB04c2b1(t, "commitOwner", prepared.hello.wire, b04c3cCommitMessage(prepared.hello, prepared.pendingID))
	if response.Action != "ownerCommitted" || response.ErrorCode != "" {
		t.Fatalf("commitOwner: action=%q errorCode=%q", response.Action, response.ErrorCode)
	}
	if response.Epoch != prepared.hello.epoch || response.Generation != prepared.hello.generation {
		t.Fatalf("commitOwner header: epochMatches=%t generationMatches=%t", response.Epoch == prepared.hello.epoch, response.Generation == prepared.hello.generation)
	}
	if response.OwnerInfo == nil {
		t.Fatal("commitOwner returned no owner info")
	}
	info := response.OwnerInfo
	if info.ID == "" || info.Epoch != prepared.hello.epoch || !info.Expires.After(time.Now()) || info.BrokerKey != prepared.expectedPin {
		t.Fatalf("commitOwner returned invalid public metadata: idPresent=%t epochPresent=%t expiresValid=%t pinMatches=%t", info.ID != "", info.Epoch != "", info.Expires.After(time.Now()), info.BrokerKey == prepared.expectedPin)
	}
	if response.OwnerPrepare != nil || len(response.UnlockKey) != 0 || len(response.Proof) != 0 {
		t.Fatal("commitOwner exposed root or prepare material")
	}
	session := ownercap.SessionInfo{ID: info.ID, Epoch: info.Epoch, Expires: info.Expires}
	if err := prepared.signer.Bind(session); err != nil {
		t.Fatal(err)
	}
	return b04c3cCommitted{
		info: session,
		data: client.Client{Dir: fixture.dir, BrokerKey: info.BrokerKey, Owner: prepared.signer},
	}
}

func saveOfflineB04c3c(t *testing.T, fixture b04c2b1Fixture, command snippet.Command) {
	t.Helper()
	key, err := fixture.source.UnlockKey()
	if err != nil {
		t.Fatal(err)
	}
	defer clear(key)
	release, err := localipc.Acquire(fixture.dir)
	if err != nil {
		t.Fatal(err)
	}
	offline, err := vault.ReadKey(broker.VaultPath(fixture.dir), key)
	if err != nil {
		release()
		t.Fatal(err)
	}
	if err := offline.SaveSnippet(broker.VaultPath(fixture.dir), command, false); err != nil {
		offline.Close()
		release()
		t.Fatal(err)
	}
	offline.Close()
	release()
}

func hasSnippetB04c3c(commands []snippet.Command, id string) bool {
	for _, command := range commands {
		if command.ID == id {
			return true
		}
	}
	return false
}

func TestOwnerCommitReadsLatestVaultAndPublishesManagedCore(t *testing.T) {
	fixture := newB04c2b1Fixture(t)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	prepared := prepareB04c3c(t, ctx, fixture, "prepare-latest")
	if status := fixture.host.Status(); status.State != protocol.ServiceStateLocked {
		t.Fatalf("prepare changed host state: %q", status.State)
	}
	if got := service.PendingOwnersForTesting(fixture.host); got != 1 {
		t.Fatalf("pending owners after prepare=%d, want 1", got)
	}

	offline := snippet.Command{ID: "b04c3c-offline", Name: "Offline update", Body: "echo latest-vault"}
	saveOfflineB04c3c(t, fixture, offline)
	if hasSnippetB04c3c(fixture.source.Snippets(), offline.ID) {
		t.Fatal("source session unexpectedly observed the offline update")
	}

	committed := commitB04c3c(t, fixture, prepared)
	if got := service.PendingOwnersForTesting(fixture.host); got != 0 {
		t.Fatalf("pending owners after commit=%d, want 0", got)
	}
	if status := fixture.host.Status(); status.State != protocol.ServiceStateUnlocked {
		t.Fatalf("commit host state=%q, want unlocked", status.State)
	}
	wireStatus := statusB04c2b1(t, prepared.hello.wire, prepared.hello)
	if wireStatus.State != protocol.ServiceStateUnlocked {
		t.Fatalf("wire state=%q, want unlocked", wireStatus.State)
	}
	sdkStatus, err := (client.ServiceClient{Dir: fixture.dir, Executable: b03bExecutable(t)}).Status(ctx)
	if err != nil || sdkStatus.State != protocol.ServiceStateUnlocked {
		t.Fatalf("SDK state=%q err=%v, want unlocked", sdkStatus.State, err)
	}

	commands, err := committed.data.ListSnippets(ctx, prepared.signer)
	if err != nil {
		t.Fatal(err)
	}
	if !hasSnippetB04c3c(commands, offline.ID) {
		t.Fatal("managed core did not read the latest vault")
	}
	heartbeat := exchangeB04c2b1(t, "heartbeat", prepared.hello.wire, protocol.ServiceMessage{
		Action: "heartbeat", Major: protocol.ServiceProtocolMajor, Minor: prepared.hello.minor,
		Epoch: prepared.hello.epoch, Generation: prepared.hello.generation,
	})
	if heartbeat.Action != "heartbeat" || heartbeat.ErrorCode != "" || heartbeat.Epoch != prepared.hello.epoch || heartbeat.Generation != prepared.hello.generation {
		t.Fatalf("heartbeat: action=%q errorCode=%q epochMatches=%t generationMatches=%t", heartbeat.Action, heartbeat.ErrorCode, heartbeat.Epoch == prepared.hello.epoch, heartbeat.Generation == prepared.hello.generation)
	}
}

func TestOwnerCommitCannotMoveToAnotherConnection(t *testing.T) {
	fixture := newB04c2b1Fixture(t)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	first := prepareB04c3c(t, ctx, fixture, "prepare-a")
	second := helloB04c2b1(t, ctx, fixture.dir)

	rejected := exchangeB04c2b1(t, "cross-connection commit", second.wire, b04c3cCommitMessage(second, first.pendingID))
	if rejected.Action != "error" || rejected.ErrorCode == "" {
		t.Fatalf("cross-connection commit: action=%q errorCode=%q", rejected.Action, rejected.ErrorCode)
	}
	if status := fixture.host.Status(); status.State != protocol.ServiceStateLocked {
		t.Fatalf("cross-connection commit changed host state: %q", status.State)
	}
	if got := service.PendingOwnersForTesting(fixture.host); got != 1 {
		t.Fatalf("pending owners after cross-connection commit=%d, want 1", got)
	}

	committed := commitB04c3c(t, fixture, first)
	if _, err := committed.data.ListSnippets(ctx, first.signer); err != nil {
		t.Fatalf("original owner query failed: %v", err)
	}
	third := helloB04c2b1(t, ctx, fixture.dir)
	heartbeat := exchangeB04c2b1(t, "public-id heartbeat", third.wire, protocol.ServiceMessage{
		Action: "heartbeat", Major: protocol.ServiceProtocolMajor, Minor: third.minor,
		Epoch: third.epoch, Generation: third.generation, ID: committed.info.ID,
	})
	if heartbeat.Action != "error" || heartbeat.ErrorCode == "" {
		t.Fatalf("public-id heartbeat: action=%q errorCode=%q", heartbeat.Action, heartbeat.ErrorCode)
	}
	if _, err := committed.data.ListSnippets(ctx, first.signer); err != nil {
		t.Fatalf("owner query after public-id heartbeat failed: %v", err)
	}
}

func TestOwnerCommitRejectsWrongLocatorAndCannotCommitTwice(t *testing.T) {
	t.Run("wrong-locator", func(t *testing.T) {
		fixture := newB04c2b1Fixture(t)
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		prepared := prepareB04c3c(t, ctx, fixture, "prepare-wrong-locator")

		response := exchangeB04c2b1(t, "wrong locator", prepared.hello.wire, b04c3cCommitMessage(prepared.hello, prepared.pendingID+"-wrong"))
		if response.Action != "error" || response.ErrorCode == "" {
			t.Fatalf("wrong locator: action=%q errorCode=%q", response.Action, response.ErrorCode)
		}
		waitB04c2b2Pending(t, fixture.host, 0)
		if status := fixture.host.Status(); status.State != protocol.ServiceStateLocked {
			t.Fatalf("wrong locator changed host state: %q", status.State)
		}
		dialCtx, dialCancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer dialCancel()
		connection, err := localipc.Dial(dialCtx, fixture.dir)
		if err == nil {
			_ = connection.Close()
			t.Fatal("wrong locator started a broker listener")
		}
	})

	t.Run("cannot-commit-twice", func(t *testing.T) {
		fixture := newB04c2b1Fixture(t)
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		prepared := prepareB04c3c(t, ctx, fixture, "prepare-duplicate")
		committed := commitB04c3c(t, fixture, prepared)
		if _, err := committed.data.ListSnippets(ctx, prepared.signer); err != nil {
			t.Fatalf("initial owner query failed: %v", err)
		}

		duplicate := exchangeB04c2b1(t, "duplicate commit", prepared.hello.wire, b04c3cCommitMessage(prepared.hello, prepared.pendingID))
		if duplicate.Action != "error" || duplicate.ErrorCode == "" {
			t.Fatalf("duplicate commit: action=%q errorCode=%q", duplicate.Action, duplicate.ErrorCode)
		}
		if err := prepared.hello.wire.Conn.SetDeadline(time.Now().Add(3 * time.Second)); err != nil {
			t.Fatal(err)
		}
		var ended protocol.ServiceMessage
		if err := prepared.hello.wire.Receive(&ended); err == nil {
			t.Fatal("control connection remained open after duplicate commit")
		} else if !b04c2b1ConnectionEnded(err) {
			t.Fatalf("control connection did not end: %v", err)
		}

		queryCtx, queryCancel := context.WithTimeout(ctx, 2*time.Second)
		defer queryCancel()
		if _, err := committed.data.ListSnippets(queryCtx, prepared.signer); err == nil {
			t.Fatal("old signer still queried after its control connection ended")
		}
		if status := fixture.host.Status(); status.State != protocol.ServiceStateUnlocked {
			t.Fatalf("duplicate commit changed host state: %q", status.State)
		}
		root := client.Client{Dir: fixture.dir, BrokerKey: committed.data.BrokerKey, Owner: fixture.source}
		if _, err := root.ListSnippets(ctx, fixture.source); err != nil {
			t.Fatalf("root query failed after owner control closed: %v", err)
		}
	})
}
