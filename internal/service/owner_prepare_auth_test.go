package service_test

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"net"
	"testing"
	"time"

	"sshclient/internal/broker"
	"sshclient/internal/client"
	"sshclient/internal/localipc"
	"sshclient/internal/ownercap"
	"sshclient/internal/protocol"
	"sshclient/internal/service"
	"sshclient/internal/vault"
)

type b04c2b1Fixture struct {
	dir    string
	host   *service.Host
	source *vault.Session
}

type b04c2b1Hello struct {
	wire       *localipc.Wire
	epoch      string
	challenge  string
	generation uint64
	minor      int
	binding    string
}

func newB04c2b1Fixture(t *testing.T) b04c2b1Fixture {
	t.Helper()
	dir := b03bTestDir(t)
	source, err := vault.New([]byte("synthetic-owner-password"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(source.Close)
	if err := vault.Save(broker.VaultPath(dir), source, true); err != nil {
		t.Fatal(err)
	}
	host, err := service.Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = host.Close() })
	return b04c2b1Fixture{dir: dir, host: host, source: source}
}

func helloB04c2b1(t *testing.T, ctx context.Context, base string) b04c2b1Hello {
	t.Helper()
	wire := b03bConnect(t, ctx, base)
	if err := wire.Conn.SetDeadline(time.Now().Add(5 * time.Second)); err != nil {
		t.Fatal(err)
	}
	if err := wire.Send(protocol.ServiceMessage{
		Action: "hello", Major: protocol.ServiceProtocolMajor, Minor: protocol.ServiceProtocolMinor,
	}); err != nil {
		t.Fatal(err)
	}
	var hello protocol.ServiceMessage
	if err := wire.Receive(&hello); err != nil {
		t.Fatal(err)
	}
	if hello.Action != "hello" || hello.ErrorCode != "" || len(hello.Epoch) != 32 ||
		len(hello.Challenge) != 32 || hello.Generation == 0 {
		t.Fatalf("invalid hello: action=%q errorCode=%q", hello.Action, hello.ErrorCode)
	}
	binding, err := localipc.ChannelBinding(wire.Conn)
	if err != nil {
		t.Fatal(err)
	}
	return b04c2b1Hello{
		wire: wire, epoch: hello.Epoch, challenge: hello.Challenge,
		generation: hello.Generation, minor: hello.Minor, binding: binding,
	}
}

func newB04c2b1PrepareMessage(t *testing.T, source *vault.Session, hello b04c2b1Hello) protocol.ServiceMessage {
	t.Helper()
	signer, err := ownercap.NewSigner()
	if err != nil {
		t.Fatal(err)
	}
	defer signer.Close()
	publicKey := signer.PublicKey()
	unlockKey, err := source.UnlockKey()
	if err != nil {
		t.Fatal(err)
	}
	defer clear(unlockKey)
	prepare := protocol.OwnerPrepare{
		Epoch:           hello.epoch,
		Challenge:       hello.challenge,
		Binding:         hello.binding,
		Generation:      hello.generation,
		PublicKey:       publicKey,
		LifetimeSeconds: 300,
		KeyHash:         b04c2b1KeyHash(unlockKey),
	}
	payload, err := protocol.ServiceOwnerPreparePayload(prepare)
	if err != nil {
		t.Fatal(err)
	}
	proof, err := source.Proof(payload)
	if err != nil {
		t.Fatal(err)
	}
	return protocol.ServiceMessage{
		Action:       "prepareOwner",
		Major:        protocol.ServiceProtocolMajor,
		Minor:        hello.minor,
		Epoch:        hello.epoch,
		Generation:   hello.generation,
		Challenge:    hello.challenge,
		OwnerPrepare: &prepare,
		UnlockKey:    append([]byte(nil), unlockKey...),
		Proof:        proof,
	}
}

func cloneB04c2b1Message(message protocol.ServiceMessage) protocol.ServiceMessage {
	cloned := message
	if message.OwnerPrepare != nil {
		prepare := *message.OwnerPrepare
		prepare.PublicKey = append([]byte(nil), message.OwnerPrepare.PublicKey...)
		cloned.OwnerPrepare = &prepare
	}
	cloned.UnlockKey = append([]byte(nil), message.UnlockKey...)
	cloned.Proof = append([]byte(nil), message.Proof...)
	return cloned
}

func clearB04c2b1Message(message *protocol.ServiceMessage) {
	if message == nil {
		return
	}
	clear(message.UnlockKey)
	message.UnlockKey = nil
	clear(message.Proof)
	message.Proof = nil
	if message.OwnerPrepare != nil {
		clear(message.OwnerPrepare.PublicKey)
		message.OwnerPrepare = nil
	}
}

func b04c2b1KeyHash(key []byte) string {
	digest := sha256.Sum256(key)
	return hex.EncodeToString(digest[:])
}

func replaceB04c2b1UnlockKey(message *protocol.ServiceMessage, key []byte) {
	clear(message.UnlockKey)
	message.UnlockKey = append([]byte(nil), key...)
	message.OwnerPrepare.KeyHash = b04c2b1KeyHash(key)
}

func resignB04c2b1Message(t *testing.T, source *vault.Session, message *protocol.ServiceMessage) {
	t.Helper()
	payload, err := protocol.ServiceOwnerPreparePayload(*message.OwnerPrepare)
	if err != nil {
		t.Fatal(err)
	}
	proof, err := source.Proof(payload)
	if err != nil {
		t.Fatal(err)
	}
	clear(message.Proof)
	message.Proof = proof
}

func exchangeB04c2b1(t *testing.T, action string, wire *localipc.Wire, message protocol.ServiceMessage) protocol.ServiceMessage {
	t.Helper()
	if err := wire.Conn.SetDeadline(time.Now().Add(5 * time.Second)); err != nil {
		t.Fatal(err)
	}
	if err := wire.Send(message); err != nil {
		t.Fatalf("%s send: %v", action, err)
	}
	var response protocol.ServiceMessage
	if err := wire.Receive(&response); err != nil {
		t.Fatalf("%s receive: %v", action, err)
	}
	return response
}

func statusB04c2b1(t *testing.T, wire *localipc.Wire, hello b04c2b1Hello) protocol.ServiceStatus {
	t.Helper()
	response := exchangeB04c2b1(t, "status", wire, protocol.ServiceMessage{
		Action: "status", Major: protocol.ServiceProtocolMajor, Minor: hello.minor,
		Epoch: hello.epoch, Generation: hello.generation,
	})
	if response.Action != "status" || response.ErrorCode != "" || response.Status == nil {
		t.Fatalf("status: action=%q errorCode=%q", response.Action, response.ErrorCode)
	}
	return *response.Status
}

func b04c2b1ConnectionEnded(err error) bool {
	if errors.Is(err, io.EOF) || errors.Is(err, io.ErrUnexpectedEOF) || errors.Is(err, net.ErrClosed) {
		return true
	}
	var networkError net.Error
	return errors.As(err, &networkError) && !networkError.Timeout()
}

func TestOwnerPrepareDoesNotUnlock(t *testing.T) {
	fixture := newB04c2b1Fixture(t)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	hello := helloB04c2b1(t, ctx, fixture.dir)
	message := newB04c2b1PrepareMessage(t, fixture.source, hello)
	defer clearB04c2b1Message(&message)

	response := exchangeB04c2b1(t, "prepare", hello.wire, message)
	if response.Action != "ownerPrepared" || response.ID == "" || response.ErrorCode != "" {
		t.Fatalf("prepare: action=%q errorCode=%q idPresent=%t", response.Action, response.ErrorCode, response.ID != "")
	}
	if response.Epoch != hello.epoch || response.Generation != hello.generation {
		t.Fatalf("prepare: epochMatches=%t generationMatches=%t", response.Epoch == hello.epoch, response.Generation == hello.generation)
	}
	if response.OwnerInfo != nil || response.OwnerPrepare != nil || len(response.UnlockKey) != 0 || len(response.Proof) != 0 {
		t.Fatal("prepare exposed root or owner material")
	}

	wireStatus := statusB04c2b1(t, hello.wire, hello)
	if wireStatus.State != protocol.ServiceStateLocked || wireStatus.Generation < 1 {
		t.Fatalf("wire status: state=%q generation=%d", wireStatus.State, wireStatus.Generation)
	}
	if direct := fixture.host.Status(); direct.State != protocol.ServiceStateLocked || direct.Generation < 1 {
		t.Fatalf("host status: state=%q generation=%d", direct.State, direct.Generation)
	}

	sdkCtx, sdkCancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer sdkCancel()
	sdkStatus, err := (client.ServiceClient{Dir: fixture.dir, Executable: b03bExecutable(t)}).Status(sdkCtx)
	if err != nil || sdkStatus.State != protocol.ServiceStateLocked || sdkStatus.Generation < 1 {
		t.Fatalf("SDK status: state=%q generation=%d err=%v", sdkStatus.State, sdkStatus.Generation, err)
	}

	brokerCtx, brokerCancel := context.WithTimeout(context.Background(), time.Second)
	defer brokerCancel()
	brokerConnection, err := localipc.Dial(brokerCtx, fixture.dir)
	if err == nil {
		_ = brokerConnection.Close()
		t.Fatal("prepare started an unauthenticated broker listener")
	}

	second := cloneB04c2b1Message(message)
	defer clearB04c2b1Message(&second)
	if err := hello.wire.Conn.SetDeadline(time.Now().Add(5 * time.Second)); err != nil {
		t.Fatal(err)
	}
	if err := hello.wire.Send(second); err != nil {
		if !b04c2b1ConnectionEnded(err) {
			t.Fatalf("second prepare send: %v", err)
		}
	} else {
		var rejection protocol.ServiceMessage
		if err := hello.wire.Receive(&rejection); err != nil {
			if !b04c2b1ConnectionEnded(err) {
				t.Fatalf("second prepare receive: %v", err)
			}
		} else if rejection.ErrorCode == "" {
			t.Fatalf("second prepare: action=%q errorCode=%q", rejection.Action, rejection.ErrorCode)
		}
	}
	if final := fixture.host.Status(); final.State != protocol.ServiceStateLocked {
		t.Fatalf("second prepare changed host state: %q", final.State)
	}
}

func TestOwnerPrepareRejectsInvalidAuthority(t *testing.T) {
	fixture := newB04c2b1Fixture(t)
	scenarios := []struct {
		name   string
		mutate func(*testing.T, *vault.Session, *protocol.ServiceMessage)
	}{
		{"proof", func(t *testing.T, source *vault.Session, message *protocol.ServiceMessage) {
			message.Proof[len(message.Proof)/2] ^= 0xff
		}},
		{"unlock-key", func(t *testing.T, source *vault.Session, message *protocol.ServiceMessage) {
			wrongKey := bytes.Repeat([]byte{0x42}, 32)
			defer clear(wrongKey)
			replaceB04c2b1UnlockKey(message, wrongKey)
			resignB04c2b1Message(t, source, message)
		}},
		{"public-key", func(t *testing.T, source *vault.Session, message *protocol.ServiceMessage) {
			message.OwnerPrepare.PublicKey[0] ^= 0xff
		}},
		{"lifetime", func(t *testing.T, source *vault.Session, message *protocol.ServiceMessage) {
			message.OwnerPrepare.LifetimeSeconds++
		}},
		{"binding", func(t *testing.T, source *vault.Session, message *protocol.ServiceMessage) {
			message.OwnerPrepare.Binding = "synthetic-self-reported-binding"
			resignB04c2b1Message(t, source, message)
		}},
		{"generation", func(t *testing.T, source *vault.Session, message *protocol.ServiceMessage) {
			message.Generation++
			message.OwnerPrepare.Generation++
			resignB04c2b1Message(t, source, message)
		}},
		{"short-unlock-key", func(t *testing.T, source *vault.Session, message *protocol.ServiceMessage) {
			shortKey := bytes.Repeat([]byte{0x33}, 31)
			defer clear(shortKey)
			replaceB04c2b1UnlockKey(message, shortKey)
			resignB04c2b1Message(t, source, message)
		}},
		{"lifetime-too-long", func(t *testing.T, source *vault.Session, message *protocol.ServiceMessage) {
			message.OwnerPrepare.LifetimeSeconds = 8*3600 + 1
			resignB04c2b1Message(t, source, message)
		}},
	}

	for _, scenario := range scenarios {
		t.Run(scenario.name, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			hello := helloB04c2b1(t, ctx, fixture.dir)
			original := newB04c2b1PrepareMessage(t, fixture.source, hello)
			defer clearB04c2b1Message(&original)
			candidate := cloneB04c2b1Message(original)
			defer clearB04c2b1Message(&candidate)
			scenario.mutate(t, fixture.source, &candidate)

			response := exchangeB04c2b1(t, scenario.name, hello.wire, candidate)
			if response.Action != "error" || response.ErrorCode != "invalid_owner" {
				t.Fatalf("%s: action=%q errorCode=%q", scenario.name, response.Action, response.ErrorCode)
			}
			if status := fixture.host.Status(); status.State != protocol.ServiceStateLocked {
				t.Fatalf("%s changed host state: %q", scenario.name, status.State)
			}
		})
	}
}

func TestOwnerPrepareCannotMoveToAnotherControlConnection(t *testing.T) {
	fixture := newB04c2b1Fixture(t)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	first := helloB04c2b1(t, ctx, fixture.dir)
	second := helloB04c2b1(t, ctx, fixture.dir)
	message := newB04c2b1PrepareMessage(t, fixture.source, first)
	defer clearB04c2b1Message(&message)

	response := exchangeB04c2b1(t, "cross-connection", second.wire, message)
	if response.Action != "error" || response.ErrorCode != "invalid_owner" {
		t.Fatalf("cross-connection: action=%q errorCode=%q", response.Action, response.ErrorCode)
	}
	_ = second.wire.Close()

	response = exchangeB04c2b1(t, "original-connection", first.wire, message)
	if response.Action != "ownerPrepared" || response.ID == "" || response.ErrorCode != "" {
		t.Fatalf("original-connection: action=%q errorCode=%q idPresent=%t", response.Action, response.ErrorCode, response.ID != "")
	}
	if status := fixture.host.Status(); status.State != protocol.ServiceStateLocked {
		t.Fatalf("cross-connection prepare changed host state: %q", status.State)
	}
}
