package service

import (
	"testing"
	"time"

	"sshclient/internal/localipc"
)

// The envelope goes only to the executable that deposited it, while fresh.
func TestQuickUnlockMatchesOnlyTheDepositorWhileFresh(t *testing.T) {
	now := time.Now()
	q := quickUnlock{codeID: "desktop-code", record: []byte("envelope"), expires: now.Add(time.Hour)}
	for _, identity := range []*localipc.ProcessIdentity{
		nil,
		{CodeID: ""},
		{CodeID: "cli-code"},        // another program of this installation
		{CodeID: "desktop-code-v2"}, // an updated or replaced desktop
	} {
		if q.matches(identity, now) {
			t.Fatal("released to another identity", identity)
		}
	}
	// Same code identity but no live, verifiable process instance.
	if q.matches(&localipc.ProcessIdentity{CodeID: "desktop-code"}, now) {
		t.Fatal("released without a verifiable process instance")
	}
	expired := quickUnlock{codeID: "desktop-code", record: []byte("envelope"), expires: now.Add(-time.Second)}
	if expired.matches(&localipc.ProcessIdentity{CodeID: "desktop-code"}, now) {
		t.Fatal("expired envelope released")
	}
	q.wipe()
	if q.codeID != "" || q.record != nil {
		t.Fatal("wipe left data behind")
	}
}
