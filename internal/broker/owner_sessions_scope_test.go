package broker_test

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"os"
	"strings"
	"testing"
	"time"

	"sshclient/internal/approval"
	"sshclient/internal/broker"
	"sshclient/internal/client"
)

func TestManagedOwnerCannotDecideExternalOrCreateGrant(t *testing.T) {
	fixture := newManagedSessionsFixture(t)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	foreign := client.Client{Dir: fixture.dir, BrokerKey: fixture.server.PublicKeyPin()}
	external, _, err := foreign.Request(ctx, approval.Operation{
		Host:    fixture.profile.ID,
		Kind:    "exec",
		Command: "printf foreign-owner-test",
		Client:  "synthetic external AI",
	}, time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	if external.OwnerIssued || external.Status != "pending" {
		t.Fatalf("foreign request unexpectedly carried owner authority: %#v", external)
	}
	assertPending := func(stage string) {
		t.Helper()
		stored, getErr := fixture.server.Approvals.Get(external.ID)
		if getErr != nil || stored.Status != "pending" {
			t.Fatalf("%s: stored external request = %#v, %v", stage, stored, getErr)
		}
	}
	assertPending("before managed decisions")

	for _, decision := range []struct {
		name   string
		allow  bool
		revoke bool
	}{
		{name: "allow", allow: true},
		{name: "deny", allow: false},
		{name: "revoke", revoke: true},
	} {
		err = fixture.cli.DecideReviewedWithOwner(ctx, external, decision.allow, decision.revoke, fixture.signer)
		if err == nil {
			t.Fatalf("managed session %s decision accepted for a foreign request", decision.name)
		}
		assertPending("after managed " + decision.name)
	}

	root := client.Client{Dir: fixture.dir, BrokerKey: fixture.server.PublicKeyPin(), Owner: fixture.server.Vault}
	if err = root.DecideReviewedWithOwner(ctx, external, true, false, fixture.server.Vault); err != nil {
		t.Fatalf("root proof could not approve the foreign request: %v", err)
	}
	approved, err := fixture.server.Approvals.Get(external.ID)
	if err != nil || approved.Status != "approved" {
		t.Fatalf("root-approved external request = %#v, %v", approved, err)
	}

	public, _, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	grant := approval.GrantScope{
		ReadRoot:        "/tmp",
		PublicKey:       base64.RawURLEncoding.EncodeToString(public),
		Permission:      "read",
		LifetimeSeconds: 60,
		IdleSeconds:     60,
	}
	if err = grant.Validate(); err != nil {
		t.Fatalf("test grant is not valid: %v", err)
	}
	_, _, err = fixture.cli.RequestWithOwner(ctx, approval.Operation{
		Host:   fixture.profile.ID,
		Kind:   "grant",
		Client: "synthetic managed grant",
		Grant:  grant,
	}, fixture.signer)
	if err == nil {
		t.Fatal("managed session created a grant request")
	}
}

func TestManagedOwnerSelfOperationAndExactReview(t *testing.T) {
	fixture := newManagedSessionsFixture(t)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	const clientLabel = "synthetic self-declared label"
	request, ticket, err := fixture.cli.RequestWithOwner(ctx, approval.Operation{
		Host:    fixture.profile.ID,
		Kind:    "exec",
		Command: "printf managed-owner-test",
		Client:  clientLabel,
	}, fixture.signer)
	if err != nil {
		t.Fatal(err)
	}
	if !request.OwnerIssued {
		t.Fatal("managed request was not marked OwnerIssued")
	}
	if request.Peer.PID != os.Getpid() || request.Peer.Instance == "" || request.Peer.CodeID == "" {
		t.Fatalf("managed request peer = %#v", request.Peer)
	}
	if request.Operation.Client != clientLabel {
		t.Fatalf("managed request client label = %q, want %q", request.Operation.Client, clientLabel)
	}
	assertPending := func(stage string) {
		t.Helper()
		stored, getErr := fixture.server.Approvals.Get(request.ID)
		if getErr != nil || stored.Status != "pending" {
			t.Fatalf("%s: stored managed request = %#v, %v", stage, stored, getErr)
		}
	}

	tampered := request
	tampered.Nonce = "forged-review-nonce"
	if err = fixture.cli.DecideReviewedWithOwner(ctx, tampered, true, false, fixture.signer); err == nil {
		t.Fatal("managed session approved a request with a changed nonce")
	}
	assertPending("after changed nonce")

	raw := client.Client{Dir: fixture.dir, BrokerKey: fixture.server.PublicKeyPin()}
	denyProof, err := approval.DecisionProof(request, false, fixture.signer.Proof)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = raw.Call(ctx, broker.Message{Action: "decide", ID: request.ID, Allow: true, Proof: denyProof}); err == nil {
		t.Fatal("a deny proof was accepted as an allow decision")
	}
	assertPending("after changed allow")

	if err = fixture.cli.DecideReviewedWithOwner(ctx, request, true, false, fixture.signer); err != nil {
		t.Fatalf("managed owner could not approve its exact request: %v", err)
	}
	stream, err := fixture.cli.Start(ctx, request.ID, ticket, 80, 24)
	if err != nil {
		t.Fatal(err)
	}
	defer stream.Close()

	var stdout, stderr bytes.Buffer
	code, files, err := stream.Copy(&stdout, &stderr)
	if err != nil {
		t.Fatal(err)
	}
	if code != 0 || len(files) != 0 {
		t.Fatalf("managed exec result: code=%d files=%d stderr=%q", code, len(files), stderr.String())
	}
	if !strings.Contains(stdout.String(), "fixture stdout: printf managed-owner-test") {
		t.Fatalf("managed exec stdout = %q", stdout.String())
	}
	if !strings.Contains(stderr.String(), "fixture stderr") {
		t.Fatalf("managed exec stderr = %q", stderr.String())
	}
}
