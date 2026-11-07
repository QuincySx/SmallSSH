package broker_test

import (
	"errors"
	"testing"
	"time"

	"sshclient/internal/approval"
	"sshclient/internal/broker"
	"sshclient/internal/protocol"
)

func TestOwnerControlRequiresItsLiveManagedLease(t *testing.T) {
	fixture := newManagedSessionsFixture(t)
	info := fixture.lease.Info()
	control := protocol.ServiceOwnerControl{
		Action:     "lock",
		Epoch:      info.Epoch,
		Generation: 7,
		Binding:    "synthetic-control-binding",
		OwnerID:    info.ID,
	}
	payload, err := protocol.ServiceOwnerControlPayload(control)
	if err != nil {
		t.Fatal(err)
	}
	proof, err := fixture.signer.Proof(payload)
	if err != nil {
		t.Fatal(err)
	}
	assertDenied := func(label string, err error) {
		t.Helper()
		if !errors.Is(err, approval.ErrDenied) {
			t.Fatalf("%s: err=%v, want ErrDenied", label, err)
		}
	}
	if err = fixture.server.VerifyOwnerControl(fixture.lease, fixture.identity, payload, proof); err != nil {
		t.Fatalf("valid control failed: %v", err)
	}

	changed := control
	changed.Action = "stop"
	changedPayload, err := protocol.ServiceOwnerControlPayload(changed)
	if err != nil {
		t.Fatal(err)
	}
	assertDenied("changed action", fixture.server.VerifyOwnerControl(fixture.lease, fixture.identity, changedPayload, proof))

	rootProof, err := fixture.server.Vault.Proof(payload)
	if err != nil {
		t.Fatal(err)
	}
	assertDenied("root HMAC", fixture.server.VerifyOwnerControl(fixture.lease, fixture.identity, payload, rootProof))
	assertDenied("nil caller", fixture.server.VerifyOwnerControl(fixture.lease, nil, payload, proof))

	_, additionalSigner, additionalLease, cancelAdditional := newAdditionalManagedOwner(t, fixture.server, fixture.dir)
	defer cancelAdditional()
	crossProof, err := additionalSigner.Proof(payload)
	if err != nil {
		t.Fatal(err)
	}
	assertDenied("cross lease", fixture.server.VerifyOwnerControl(fixture.lease, fixture.identity, payload, crossProof))

	additionalInfo := additionalLease.Info()
	additionalControl := control
	additionalControl.Epoch = additionalInfo.Epoch
	additionalControl.OwnerID = additionalInfo.ID
	additionalPayload, err := protocol.ServiceOwnerControlPayload(additionalControl)
	if err != nil {
		t.Fatal(err)
	}
	additionalProof, err := additionalSigner.Proof(additionalPayload)
	if err != nil {
		t.Fatal(err)
	}
	if err = fixture.server.VerifyOwnerControl(additionalLease, fixture.identity, additionalPayload, additionalProof); err != nil {
		t.Fatalf("valid additional control failed: %v", err)
	}

	fixture.cancelOwner()
	select {
	case <-fixture.lease.Done():
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for revoked lease")
	}
	assertDenied("revoked lease", fixture.server.VerifyOwnerControl(fixture.lease, fixture.identity, payload, proof))
	if err = fixture.server.VerifyOwnerControl(additionalLease, fixture.identity, additionalPayload, additionalProof); err != nil {
		t.Fatalf("additional lease failed after peer revocation: %v", err)
	}

	fixture.server.Close()
	assertDenied("closed core", fixture.server.VerifyOwnerControl(additionalLease, fixture.identity, additionalPayload, additionalProof))
	var nilServer *broker.Server
	assertDenied("nil server", nilServer.VerifyOwnerControl(additionalLease, fixture.identity, additionalPayload, additionalProof))
	assertDenied("nil lease", fixture.server.VerifyOwnerControl(nil, fixture.identity, additionalPayload, additionalProof))
}
