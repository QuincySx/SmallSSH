package approval

import (
	"context"
	"crypto/ed25519"
	"testing"
)

func TestDisconnectCancelsChildrenWithoutRewritingCompletedHistory(t *testing.T) {
	m, parent, _, key := grantFixture(t, "full")
	approve(t, m, parent)
	finished, ticket := delegated(t, m, parent, key, "exec")
	_, _, cancel, err := m.Start(context.Background(), finished.ID, ticket)
	if err != nil {
		t.Fatal(err)
	}
	cancel()
	m.Finish(finished.ID)
	running, ticket := delegated(t, m, parent, key, "exec")
	ctx, _, cancel, err := m.Start(context.Background(), running.ID, ticket)
	if err != nil {
		t.Fatal(err)
	}
	defer cancel()
	m.Disconnect(parent.ID)
	if ctx.Err() == nil {
		t.Fatal("disconnect left child running")
	}
	got, err := m.Get(finished.ID)
	if err != nil || got.Status != "finished" {
		t.Fatal("disconnect rewrote completed history", got.Status, err)
	}
}

func TestDisconnectInvalidatesDormantRenewalAndCapturedSignatures(t *testing.T) {
	m, parent, _, key, _ := expiredGrant(t, "full")
	challenge := renewalChallenge(t, m, parent.ID)
	proof := ed25519.Sign(key, RenewalProofMessage(*challenge))
	m.Disconnect(parent.ID)
	if _, _, err := m.Renew(challenge.ID, proof); err == nil {
		t.Fatal("renewed a disconnected session")
	}
	if _, _, err := m.RenewalChallenge(parent.ID); err == nil {
		t.Fatal("disconnected dormant session requested renewal")
	}
}
