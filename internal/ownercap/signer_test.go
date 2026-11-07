package ownercap

import (
	"bytes"
	"strings"
	"testing"
	"time"

	"sshclient/internal/localipc"
)

func TestSignerLifecycleCopiesAndExpiry(t *testing.T) {
	start := time.Unix(1_700_000_000, 0)
	clock := installFakeClock(t, start)
	signer, err := NewSigner()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(signer.Close)
	public := signer.PublicKey()
	if len(public) != 32 {
		t.Fatalf("public key length = %d", len(public))
	}
	wantPublic := append([]byte(nil), public...)
	public[0] ^= 0xff
	if !bytes.Equal(signer.PublicKey(), wantPublic) {
		t.Fatal("PublicKey exposed the signer's stored slice")
	}

	session := SessionInfo{ID: strings.Repeat("a", 32), Epoch: testEpoch, Expires: start.Add(time.Minute)}
	for _, invalid := range []SessionInfo{
		{ID: "short", Epoch: testEpoch, Expires: start.Add(time.Minute)},
		{ID: strings.Repeat("A", 32), Epoch: testEpoch, Expires: start.Add(time.Minute)},
		{ID: strings.Repeat("0", 32), Epoch: testEpoch, Expires: start.Add(time.Minute)},
		{ID: strings.Repeat("a", 32), Epoch: "bad", Expires: start.Add(time.Minute)},
		{ID: strings.Repeat("a", 32), Epoch: testEpoch},
	} {
		if err := signer.Bind(invalid); err == nil {
			t.Fatalf("accepted invalid session metadata: %#v", invalid)
		}
	}
	if err := signer.Bind(session); err != nil {
		t.Fatal(err)
	}
	if err := signer.Bind(session); err == nil {
		t.Fatal("rebound a signer to a second session")
	}
	proof, err := signer.Proof([]byte("synthetic payload"))
	if err != nil {
		t.Fatal(err)
	}
	if id, _, err := decodeProof(proof); err != nil || id != session.ID {
		t.Fatalf("proof envelope = %q, %v", id, err)
	}
	atLimit := make([]byte, localipc.MaxFrame)
	if _, err := signer.Proof(atLimit); err != nil {
		t.Fatalf("rejected a payload at MaxFrame: %v", err)
	}
	if _, err := signer.Proof(append(atLimit, 0)); err == nil {
		t.Fatal("signed a payload larger than MaxFrame")
	}
	clock.Advance(time.Minute)
	if _, err := signer.Proof(nil); err == nil {
		t.Fatal("signed after the bound session's absolute expiry")
	}
}

func TestSignerRejectsNilUnboundAndClosed(t *testing.T) {
	var nilSigner *Signer
	if nilSigner.PublicKey() != nil {
		t.Fatal("nil signer returned a public key")
	}
	if _, err := nilSigner.Proof(nil); err == nil {
		t.Fatal("nil signer produced proof")
	}
	nilSigner.Close()

	signer, err := NewSigner()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := signer.Proof(nil); err == nil {
		t.Fatal("unbound signer produced proof")
	}
	info := SessionInfo{ID: strings.Repeat("b", 32), Epoch: testEpoch, Expires: now().Add(time.Hour)}
	if err := signer.Bind(info); err != nil {
		t.Fatal(err)
	}
	signer.Close()
	signer.Close()
	if _, err := signer.Proof([]byte("closed")); err == nil {
		t.Fatal("closed signer produced proof")
	}
	if len(signer.private) != 0 {
		t.Fatal("closed signer retained its private key slice")
	}
}
