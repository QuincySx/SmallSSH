package broker_test

import (
	"context"
	"io"
	"os"
	"path/filepath"
	"testing"

	"sshclient/internal/approval"
	"sshclient/internal/broker"
	"sshclient/internal/testserver"
	"sshclient/internal/vault"
)

func TestFileMutationsRequireApprovalAndBindDestination(t *testing.T) {
	c, _ := setup(t)
	ctx := context.Background()
	dir := t.TempDir()
	src := filepath.Join(dir, "original.txt")
	dest := filepath.Join(dir, "renamed.txt")
	os.WriteFile(src, []byte("original"), 0600)
	op := approval.Operation{Host: "fixture", Kind: "rename", Path: testserver.RemotePath(src), Destination: testserver.RemotePath(dest), Client: "UI test"}
	r, ticket := request(t, c, op)
	if _, e := c.Start(ctx, r.ID, ticket, 0, 0); e == nil {
		t.Fatal("unapproved mutation executed")
	}
	owner, e := vault.Read(broker.VaultPath(c.Dir), pass)
	if e != nil {
		t.Fatal(e)
	}
	changed := r
	changed.Operation.Destination = testserver.RemotePath(filepath.Join(dir, "different.txt"))
	proof, e := approval.DecisionProof(changed, true, owner.Proof)
	owner.Close()
	if e != nil {
		t.Fatal(e)
	}
	if _, e = c.Call(ctx, broker.Message{Action: "decide", ID: r.ID, Allow: true, Proof: proof}); e == nil {
		t.Fatal("changed destination was approved")
	}
	approve(t, c, r)
	s, e := c.Start(ctx, r.ID, ticket, 0, 0)
	if e != nil {
		t.Fatal(e)
	}
	_, _, e = s.Copy(io.Discard, io.Discard)
	s.Close()
	if e != nil {
		t.Fatal(e)
	}
	if _, e = os.Stat(src); !os.IsNotExist(e) {
		t.Fatal("source remained", e)
	}
	if data, _ := os.ReadFile(dest); string(data) != "original" {
		t.Fatal(string(data))
	}
	for _, kind := range []string{"chmod", "remove"} {
		r, ticket = request(t, c, approval.Operation{Host: "fixture", Kind: kind, Path: testserver.RemotePath(dest), Mode: 0600, Client: "UI test"})
		approve(t, c, r)
		s, e = c.Start(ctx, r.ID, ticket, 0, 0)
		if e != nil {
			t.Fatal(e)
		}
		_, _, e = s.Copy(io.Discard, io.Discard)
		s.Close()
		if e != nil {
			t.Fatal(e)
		}
	}
	if _, e = os.Stat(dest); !os.IsNotExist(e) {
		t.Fatal(e)
	}
}
func TestProfileDeletionRequiresOwnerProof(t *testing.T) {
	c, _ := setup(t)
	ctx := context.Background()
	challenge, e := c.Call(ctx, broker.Message{Action: "profileChallenge"})
	if e != nil {
		t.Fatal(e)
	}
	if _, e = c.Call(ctx, broker.Message{Action: "profileDelete", ID: challenge.ID, Target: "fixture", Proof: []byte("forged")}); e == nil {
		t.Fatal("deleted without owner proof")
	}
	v, e := vault.Read(broker.VaultPath(c.Dir), pass)
	if e != nil {
		t.Fatal(e)
	}
	defer v.Close()
	if e = c.DeleteProfile(ctx, "fixture", v); e != nil {
		t.Fatal(e)
	}
	h, e := c.Call(ctx, broker.Message{Action: "hosts"})
	if e != nil || len(h.Connections) != 0 {
		t.Fatal(h, e)
	}
}
