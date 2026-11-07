package ownercap

import (
	"context"
	"crypto/ed25519"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"sshclient/internal/localipc"
)

func TestRegistryRegistersRealIdentityAndVerifiesProof(t *testing.T) {
	start := time.Unix(1_700_000_000, 0)
	installFakeClock(t, start)
	pair := newIdentityPair(t)
	registry, err := NewRegistry(testEpoch, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { registry.Close() })
	signer, err := NewSigner()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(signer.Close)
	public := signer.PublicKey()
	info, err := registry.Register(pair.ctx, pair.caller, public, 0)
	if err != nil {
		t.Fatal(err)
	}
	public[0] ^= 0xff
	if len(info.ID) != 32 || !validEntropyHex(info.ID) || info.Epoch != testEpoch || !info.Expires.Equal(start.Add(defaultLifetime)) {
		t.Fatalf("unexpected session metadata: %#v", info)
	}
	encoded, err := json.Marshal(info)
	if err != nil {
		t.Fatal(err)
	}
	for _, field := range []string{`"id"`, `"epoch"`, `"expires"`} {
		if !strings.Contains(string(encoded), field) {
			t.Fatalf("session metadata omitted %s: %s", field, encoded)
		}
	}
	for _, field := range []string{`"ID"`, `"Epoch"`, `"Expires"`} {
		if strings.Contains(string(encoded), field) {
			t.Fatalf("session metadata used non-camel JSON field %s: %s", field, encoded)
		}
	}
	if err := signer.Bind(info); err != nil {
		t.Fatal(err)
	}
	payload := []byte("synthetic authenticated action")
	proof, err := signer.Proof(payload)
	if err != nil {
		t.Fatal(err)
	}
	sessionID, err := registry.Verify(pair.caller, payload, proof)
	if err != nil || sessionID != info.ID {
		t.Fatalf("Verify = %q, %v", sessionID, err)
	}
	if sessionID, err := registry.Verify(pair.peer, payload, proof); err != nil || sessionID != info.ID {
		t.Fatalf("same process instance from another real connection = %q, %v", sessionID, err)
	}
	if sessionID, err := registry.Verify(pair.caller, payload, proof); err != nil || sessionID != info.ID {
		t.Fatalf("repeated proof verification = %q, %v", sessionID, err)
	}
	limitPayload := make([]byte, localipc.MaxFrame)
	limitProof, err := signer.Proof(limitPayload)
	if err != nil {
		t.Fatal(err)
	}
	if sessionID, err := registry.Verify(pair.caller, limitPayload, limitProof); err != nil || sessionID != info.ID {
		t.Fatalf("payload at MaxFrame = %q, %v", sessionID, err)
	}
	if err := pair.caller.Verify(); err != nil {
		t.Fatalf("registry closed the host-owned identity: %v", err)
	}
}

func TestNewRegistryRejectsInvalidEpoch(t *testing.T) {
	for _, epoch := range []string{"", "0123456789abcdef", strings.Repeat("0", 32), strings.Repeat("A", 32), strings.Repeat("g", 32), testEpoch + "0"} {
		if registry, err := NewRegistry(epoch, nil); err == nil {
			registry.Close()
			t.Fatalf("accepted invalid epoch %q", epoch)
		}
	}
}

func TestRegistryRegisterRejectsUnverifiableInputs(t *testing.T) {
	pair := newIdentityPair(t)
	registry, err := NewRegistry(testEpoch, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(registry.Close)
	signer, err := NewSigner()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(signer.Close)
	public := signer.PublicKey()
	longPublic := append(ed25519.PublicKey(nil), public...)
	longPublic = append(longPublic, 0)
	canceled, cancel := context.WithCancel(pair.ctx)
	cancel()
	for _, test := range []struct {
		name     string
		ctx      context.Context
		identity *localipc.ProcessIdentity
		public   ed25519.PublicKey
		lifetime time.Duration
	}{
		{name: "nil context", ctx: nil, identity: pair.caller, public: public},
		{name: "non-cancellable context", ctx: context.Background(), identity: pair.caller, public: public},
		{name: "cancelled context", ctx: canceled, identity: pair.caller, public: public},
		{name: "nil identity", ctx: pair.ctx, identity: nil, public: public},
		{name: "fabricated identity", ctx: pair.ctx, identity: &localipc.ProcessIdentity{PID: pair.caller.PID, Instance: pair.caller.Instance, CodeID: pair.caller.CodeID}, public: public},
		{name: "short public key", ctx: pair.ctx, identity: pair.caller, public: public[:31]},
		{name: "long public key", ctx: pair.ctx, identity: pair.caller, public: longPublic},
		{name: "too short lifetime", ctx: pair.ctx, identity: pair.caller, public: public, lifetime: time.Minute - time.Nanosecond},
		{name: "too long lifetime", ctx: pair.ctx, identity: pair.caller, public: public, lifetime: maxLifetime + time.Nanosecond},
	} {
		t.Run(test.name, func(t *testing.T) {
			if _, err := registry.Register(test.ctx, test.identity, test.public, test.lifetime); err == nil {
				t.Fatal("accepted unverifiable registration input")
			}
		})
	}
	closedPair := newIdentityPair(t)
	closedPair.caller.Close()
	if _, err := registry.Register(closedPair.ctx, closedPair.caller, public, 0); err == nil {
		t.Fatal("registered a closed identity")
	}
	registry.Close()
	if _, err := registry.Register(pair.ctx, pair.caller, public, 0); err == nil {
		t.Fatal("registered after the registry was closed")
	}
}

func TestRegistryCapacityAndTTLBoundaries(t *testing.T) {
	pair := newIdentityPair(t)
	registry, err := NewRegistry(testEpoch, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(registry.Close)
	signer, err := NewSigner()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(signer.Close)
	public := signer.PublicKey()
	ids := make(map[string]bool)
	var oldest SessionInfo
	for index := range maxSessions {
		info, err := registry.Register(pair.ctx, pair.caller, public, minLifetime)
		if err != nil {
			t.Fatalf("registration %d: %v", index, err)
		}
		if ids[info.ID] {
			t.Fatalf("reused session ID %q", info.ID)
		}
		ids[info.ID] = true
		if index == 0 {
			oldest = info
		}
	}
	if _, err := registry.Register(pair.ctx, pair.caller, public, 0); err == nil {
		t.Fatal("registered more than the live-session limit")
	}
	registry.Revoke(oldest.ID)
	replacement, err := registry.Register(pair.ctx, pair.caller, public, maxLifetime)
	if err != nil {
		t.Fatal(err)
	}
	if replacement.ID == oldest.ID || ids[replacement.ID] {
		t.Fatal("replacement reused a prior session ID")
	}
}

func TestRegistryVerifyRejectsTamperingAndMalformedProofs(t *testing.T) {
	installFakeClock(t, time.Unix(1_700_000_000, 0))
	pair := newIdentityPair(t)
	registry, err := NewRegistry(testEpoch, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(registry.Close)
	info, signer, proof, payload := registerTestSession(t, registry, pair, time.Hour)
	if _, err := registry.Verify(pair.caller, payload, proof); err != nil {
		t.Fatalf("baseline proof failed: %v", err)
	}
	second, err := registry.Register(pair.ctx, pair.caller, signer.PublicKey(), time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	if second.ID == info.ID {
		t.Fatal("registry reused a live session ID")
	}
	signer.mu.Lock()
	private := append(ed25519.PrivateKey(nil), signer.private...)
	signer.mu.Unlock()
	wrongEpochSignature := ed25519.Sign(private, signingInput(testEpoch2, info.ID, payload))
	wrongEpochProof := encodeProof(info.ID, wrongEpochSignature)
	changedID := append([]byte(nil), proof...)
	copy(changedID[len(proofDomain):], strings.Repeat("e", 32))
	crossSession := append([]byte(nil), proof...)
	copy(crossSession[len(proofDomain):], second.ID)
	badDelimiter := append([]byte(nil), proof...)
	badDelimiter[len(proofDomain)+sessionIDSize] = 1
	badPrefix := append([]byte(nil), proof...)
	badPrefix[0] ^= 0xff
	badSignature := append([]byte(nil), proof...)
	badSignature[len(badSignature)-1] ^= 0xff
	badID := append([]byte(nil), proof...)
	badID[len(proofDomain)] = 'g'
	zeroID := append([]byte(nil), proof...)
	copy(zeroID[len(proofDomain):], strings.Repeat("0", 32))
	for _, test := range []struct {
		name    string
		caller  *localipc.ProcessIdentity
		payload []byte
		proof   []byte
	}{
		{name: "changed payload", caller: pair.caller, payload: []byte("changed"), proof: proof},
		{name: "changed session ID", caller: pair.caller, payload: payload, proof: changedID},
		{name: "cross-session proof", caller: pair.caller, payload: payload, proof: crossSession},
		{name: "changed epoch signature", caller: pair.caller, payload: payload, proof: wrongEpochProof},
		{name: "changed signature", caller: pair.caller, payload: payload, proof: badSignature},
		{name: "bad envelope prefix", caller: pair.caller, payload: payload, proof: badPrefix},
		{name: "bad envelope delimiter", caller: pair.caller, payload: payload, proof: badDelimiter},
		{name: "malformed session ID", caller: pair.caller, payload: payload, proof: badID},
		{name: "zero session ID", caller: pair.caller, payload: payload, proof: zeroID},
		{name: "short envelope", caller: pair.caller, payload: payload, proof: proof[:len(proof)-1]},
		{name: "extra envelope data", caller: pair.caller, payload: payload, proof: append(append([]byte(nil), proof...), 0)},
		{name: "nil proof", caller: pair.caller, payload: payload, proof: nil},
		{name: "oversized payload", caller: pair.caller, payload: make([]byte, localipc.MaxFrame+1), proof: proof},
		{name: "nil caller", caller: nil, payload: payload, proof: proof},
		{name: "fabricated caller", caller: &localipc.ProcessIdentity{PID: pair.caller.PID, Instance: pair.caller.Instance, CodeID: pair.caller.CodeID}, payload: payload, proof: proof},
	} {
		t.Run(test.name, func(t *testing.T) {
			if _, err := registry.Verify(test.caller, test.payload, test.proof); err == nil {
				t.Fatal("accepted altered or malformed proof material")
			}
		})
	}
}

func TestRegistryIdleExpiryRefreshAndHardExpiry(t *testing.T) {
	start := time.Unix(1_700_000_000, 0)
	clock := installFakeClock(t, start)
	pair := newIdentityPair(t)
	var revoked []string
	registry, err := NewRegistry(testEpoch, func(id string) { revoked = append(revoked, id) })
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(registry.Close)
	idleInfo, idleSigner, idleProof, payload := registerTestSession(t, registry, pair, time.Hour)
	clock.Advance(14 * time.Minute)
	if _, err := registry.Verify(pair.caller, payload, idleProof); err != nil {
		t.Fatalf("initial idle use failed: %v", err)
	}
	clock.Advance(6 * time.Minute)
	if _, err := registry.Verify(pair.caller, payload, idleProof); err != nil {
		t.Fatalf("successful use did not refresh idle expiry: %v", err)
	}
	clock.Advance(idleLifetime)
	if _, err := idleSigner.Proof(payload); err != nil {
		t.Fatalf("signer expired before the hard session expiry: %v", err)
	}
	if _, err := registry.Verify(pair.caller, payload, idleProof); err == nil {
		t.Fatal("session remained valid beyond refreshed idle expiry")
	}
	if len(revoked) != 0 {
		t.Fatal("Verify invoked onRevoke instead of leaving cleanup to Sweep")
	}
	registry.Sweep()
	if len(revoked) != 1 || revoked[0] != idleInfo.ID {
		t.Fatalf("Sweep did not revoke exactly the expired session: %#v", revoked)
	}

	hardStart := clock.current()
	hardSigner, err := NewSigner()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(hardSigner.Close)
	hardInfo, err := registry.Register(pair.ctx, pair.caller, hardSigner.PublicKey(), minLifetime)
	if err != nil {
		t.Fatal(err)
	}
	if err := hardSigner.Bind(hardInfo); err != nil {
		t.Fatal(err)
	}
	hardProof, err := hardSigner.Proof(payload)
	if err != nil {
		t.Fatal(err)
	}
	clock.Advance(minLifetime - time.Nanosecond)
	if _, err := registry.Verify(pair.caller, payload, hardProof); err != nil {
		t.Fatalf("valid use near the hard expiry failed: %v", err)
	}
	clock.Advance(time.Nanosecond)
	if clock.current() != hardStart.Add(minLifetime) {
		t.Fatal("test clock did not reach the hard expiry")
	}
	if _, err := hardSigner.Proof(payload); err == nil {
		t.Fatal("signer issued proof at the hard expiry")
	}
	if _, err := registry.Verify(pair.caller, payload, hardProof); err == nil {
		t.Fatal("successful use extended the absolute expiry")
	}
	if len(revoked) != 1 {
		t.Fatal("failed Verify synchronously invoked onRevoke")
	}
	registry.Sweep()
	if len(revoked) != 2 || revoked[1] != hardInfo.ID {
		t.Fatalf("Sweep did not revoke the hard-expired session: %#v", revoked)
	}
}

func TestBadSignatureDoesNotRefreshIdleExpiry(t *testing.T) {
	start := time.Unix(1_700_000_000, 0)
	clock := installFakeClock(t, start)
	pair := newIdentityPair(t)
	registry, err := NewRegistry(testEpoch, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(registry.Close)
	_, signer, proof, payload := registerTestSession(t, registry, pair, time.Hour)
	badProof := append([]byte(nil), proof...)
	badProof[len(badProof)-1] ^= 0xff
	clock.Advance(14 * time.Minute)
	if _, err := registry.Verify(pair.caller, payload, badProof); err == nil {
		t.Fatal("accepted a bad signature")
	}
	clock.Advance(time.Minute)
	if _, err := signer.Proof(payload); err != nil {
		t.Fatalf("signer reached hard expiry unexpectedly: %v", err)
	}
	if _, err := registry.Verify(pair.caller, payload, proof); err == nil {
		t.Fatal("a failed signature attempt refreshed idle expiry")
	}
}

func TestRegistryCancellationRevokeRevokeAllAndClose(t *testing.T) {
	pair := newIdentityPair(t)

	t.Run("control cancellation", func(t *testing.T) {
		revoked := make(chan string, 1)
		registry, err := NewRegistry(testEpoch, func(id string) { revoked <- id })
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(registry.Close)
		control, cancel := context.WithCancel(pair.ctx)
		signer, err := NewSigner()
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(signer.Close)
		info, err := registry.Register(control, pair.caller, signer.PublicKey(), time.Hour)
		if err != nil {
			t.Fatal(err)
		}
		if err := signer.Bind(info); err != nil {
			t.Fatal(err)
		}
		proof, err := signer.Proof([]byte("payload"))
		if err != nil {
			t.Fatal(err)
		}
		cancel()
		if id := receiveWithin(t, revoked, "control cancellation revoke"); id != info.ID {
			t.Fatalf("revoked %q, want %q", id, info.ID)
		}
		if _, err := registry.Verify(pair.caller, []byte("payload"), proof); err == nil {
			t.Fatal("cancelled control context left proof usable")
		}
	})

	t.Run("explicit revoke", func(t *testing.T) {
		revoked := make(chan string, 1)
		registry, err := NewRegistry(testEpoch, func(id string) { revoked <- id })
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(registry.Close)
		control, cancel := context.WithCancel(pair.ctx)
		defer cancel()
		signer, err := NewSigner()
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(signer.Close)
		info, err := registry.Register(control, pair.caller, signer.PublicKey(), time.Hour)
		if err != nil {
			t.Fatal(err)
		}
		if err := signer.Bind(info); err != nil {
			t.Fatal(err)
		}
		proof, err := signer.Proof([]byte("payload"))
		if err != nil {
			t.Fatal(err)
		}
		registry.Revoke(info.ID)
		registry.Revoke(info.ID)
		if id := receiveWithin(t, revoked, "explicit revoke"); id != info.ID {
			t.Fatalf("revoked %q, want %q", id, info.ID)
		}
		select {
		case id := <-revoked:
			t.Fatalf("duplicate revoke callback for %q", id)
		default:
		}
		if _, err := registry.Verify(pair.caller, []byte("payload"), proof); err == nil {
			t.Fatal("revoked proof remained usable")
		}
	})

	t.Run("revoke all then register and close", func(t *testing.T) {
		revoked := make(chan string, 2)
		registry, err := NewRegistry(testEpoch, func(id string) { revoked <- id })
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(registry.Close)
		var firstID string
		var firstSigner *Signer
		for index := range 2 {
			control, cancel := context.WithCancel(pair.ctx)
			defer cancel()
			signer, err := NewSigner()
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(signer.Close)
			info, err := registry.Register(control, pair.caller, signer.PublicKey(), time.Hour)
			if err != nil {
				t.Fatal(err)
			}
			if err := signer.Bind(info); err != nil {
				t.Fatal(err)
			}
			if index == 0 {
				firstID, firstSigner = info.ID, signer
			}
		}
		registry.RevokeAll()
		got := map[string]bool{}
		got[receiveWithin(t, revoked, "first RevokeAll callback")] = true
		got[receiveWithin(t, revoked, "second RevokeAll callback")] = true
		if !got[firstID] || len(got) != 2 {
			t.Fatalf("RevokeAll callbacks = %#v", got)
		}
		firstProof, err := firstSigner.Proof([]byte("old"))
		if err != nil {
			t.Fatal(err)
		}
		if _, err := registry.Verify(pair.caller, []byte("old"), firstProof); err == nil {
			t.Fatal("RevokeAll left an old session usable")
		}
		control, cancel := context.WithCancel(pair.ctx)
		defer cancel()
		signer, err := NewSigner()
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(signer.Close)
		info, err := registry.Register(control, pair.caller, signer.PublicKey(), time.Hour)
		if err != nil {
			t.Fatalf("register after RevokeAll: %v", err)
		}
		if info.ID == firstID {
			t.Fatal("RevokeAll reused an old session ID")
		}
		if err := signer.Bind(info); err != nil {
			t.Fatal(err)
		}
		proof, err := signer.Proof([]byte("new"))
		if err != nil {
			t.Fatal(err)
		}
		if _, err := registry.Verify(pair.caller, []byte("new"), proof); err != nil {
			t.Fatalf("new session after RevokeAll failed: %v", err)
		}
		registry.Close()
		registry.Close()
		if _, err := registry.Verify(pair.caller, []byte("new"), proof); err == nil {
			t.Fatal("Close left a session usable")
		}
		if _, err := registry.Register(pair.ctx, pair.caller, signer.PublicKey(), time.Hour); err == nil {
			t.Fatal("closed registry accepted registration")
		}
	})
}

func TestRegistrySweepRejectsInvalidIdentity(t *testing.T) {
	pair := newIdentityPair(t)
	revoked := make(chan string, 1)
	registry, err := NewRegistry(testEpoch, func(id string) { revoked <- id })
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(registry.Close)
	signer, err := NewSigner()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(signer.Close)
	info, err := registry.Register(pair.ctx, pair.caller, signer.PublicKey(), time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	if err := signer.Bind(info); err != nil {
		t.Fatal(err)
	}
	proof, err := signer.Proof([]byte("payload"))
	if err != nil {
		t.Fatal(err)
	}
	pair.caller.Close()
	if _, err := registry.Verify(pair.caller, []byte("payload"), proof); err == nil {
		t.Fatal("closed control identity remained usable")
	} else {
		requireIdentityFailure(t, err)
	}
	select {
	case id := <-revoked:
		t.Fatalf("Verify synchronously revoked session %q", id)
	default:
	}
	registry.Sweep()
	if id := receiveWithin(t, revoked, "identity failure Sweep"); id != info.ID {
		t.Fatalf("Sweep revoked %q, want %q", id, info.ID)
	}
}

func registerTestSession(t *testing.T, registry *Registry, pair *identityPair, lifetime time.Duration) (SessionInfo, *Signer, []byte, []byte) {
	t.Helper()
	signer, err := NewSigner()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(signer.Close)
	info, err := registry.Register(pair.ctx, pair.caller, signer.PublicKey(), lifetime)
	if err != nil {
		t.Fatal(err)
	}
	if err := signer.Bind(info); err != nil {
		t.Fatal(err)
	}
	payload := []byte("synthetic proof payload")
	proof, err := signer.Proof(payload)
	if err != nil {
		t.Fatal(err)
	}
	return info, signer, proof, payload
}

func requireIdentityFailure(t *testing.T, err error) {
	t.Helper()
	if !errors.Is(err, localipc.ErrIdentityUnavailable) {
		t.Fatalf("identity failure = %v", err)
	}
}
