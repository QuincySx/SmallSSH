package broker_test

import (
	"os"
	"testing"
	"time"

	"sshclient/internal/approval"
	"sshclient/internal/testserver"
)

func TestRequestCarriesThePeerProcessNotItsOwnLabel(t *testing.T) {
	f := testserver.New(t)
	p := f.Profile()
	_, cli, ctx := policyHarness(t, p)
	r, _, err := cli.Request(ctx, approval.Operation{Host: p.ID, Kind: "exec", Command: "true", Client: "claimed name"}, time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	if r.Peer.PID != os.Getpid() {
		t.Fatalf("peer pid %d, want %d", r.Peer.PID, os.Getpid())
	}
	if r.Operation.Client != "claimed name" {
		t.Fatal("fixture lost the self-reported label")
	}
	if r.Peer.Path == "" {
		t.Log("process path unavailable on this platform; the pid still identifies the caller")
	}
}
