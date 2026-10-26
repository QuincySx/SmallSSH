package broker_test

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"sshclient/internal/approval"
	"sshclient/internal/broker"
	"sshclient/internal/client"
	"sshclient/internal/testserver"
	"sshclient/internal/transfer"
)

func TestDelegatedClientEndToEndSSHOneApprovalAndCascadeRevoke(t *testing.T) {
	c, f := setup(t)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	r, d, err := c.RequestSession(ctx, "fixture", "AI claimed name", "full", 2*time.Hour, 15*time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()
	delegated := client.Client{Dir: c.Dir, Delegation: d}
	op := approval.Operation{Host: "fixture", Kind: "exec", Command: "delegate-once", Client: "AI claimed name"}
	if _, _, err = delegated.Request(ctx, op, time.Minute); err == nil {
		t.Fatal("parent used before approval")
	}
	approve(t, c, r)
	if err = d.Save("ai.json"); err != nil {
		t.Fatal(err)
	}
	stored, err := client.ReadDelegation("ai.json", c.Dir)
	if err != nil {
		t.Fatal(err)
	}
	defer stored.Close()
	delegated.Delegation = stored
	data, err := os.ReadFile(filepath.Join(c.Dir, "client-sessions", "ai.json"))
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(data, []byte(f.Profile().Password)) || bytes.Contains(data, []byte("PRIVATE KEY")) {
		t.Fatal("SSH credential persisted with client authorization")
	}
	for range 2 {
		child, ticket, err := delegated.Request(ctx, op, 15*time.Minute)
		if err != nil {
			t.Fatal(err)
		}
		if child.Status != "approved" || child.ParentID != r.ID || time.Until(child.Expires) > 5*time.Minute {
			t.Fatal("child not bounded or was not auto-approved", child)
		}
		stream, err := delegated.Start(ctx, child.ID, ticket, 0, 0)
		if err != nil {
			t.Fatal(err)
		}
		var out bytes.Buffer
		code, _, err := stream.Copy(&out, &out)
		stream.Close()
		if err != nil || code != 0 || !bytes.Contains(out.Bytes(), []byte("delegate-once")) {
			t.Fatal("delegated SSH failed", code, err, out.String())
		}
		if _, err = delegated.Start(ctx, child.ID, ticket, 0, 0); err == nil {
			t.Fatal("child replayed")
		}
	}
	// The client name carries no authority; another public request still waits.
	other, _, err := c.Request(ctx, op, time.Minute)
	if err != nil || other.Status != "pending" {
		t.Fatal("unrelated caller inherited parent authority", err, other)
	}
	op.Command = "block"
	child, ticket, err := delegated.Request(ctx, op, time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	stream, err := delegated.Start(ctx, child.ID, ticket, 0, 0)
	if err != nil {
		t.Fatal(err)
	}
	defer stream.Close()
	if _, err = c.Call(ctx, broker.Message{Action: "withdraw", ID: r.ID, Ticket: d.Ticket}); err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() { _, e := stream.Receive(); done <- e }()
	select {
	case err := <-done:
		if err == nil {
			t.Fatal("revoked stream returned success")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("parent revoke left a running SSH task alive")
	}
	if _, _, err = delegated.Request(ctx, op, time.Minute); err == nil {
		t.Fatal("revoked parent issued a new child")
	}
	pending, err := c.Call(ctx, broker.Message{Action: "status", ID: other.ID})
	if err != nil || pending.Request == nil || pending.Request.Status != "pending" {
		t.Fatal("revocation changed an unrelated request", err)
	}
	audit, err := os.ReadFile(filepath.Join(c.Dir, "audit.jsonl"))
	if err != nil || !bytes.Contains(audit, []byte(`"parentId"`)) || bytes.Contains(audit, data) {
		t.Fatal("audit omitted parent relationship or leaked client authorization", err)
	}
}

func TestDelegatedReadSessionDownloadUsesTransferPipelineAndCannotUpload(t *testing.T) {
	c, _ := setup(t)
	root := t.TempDir()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	r, d, err := c.RequestSession(ctx, "fixture", "read client", "read", time.Hour, 15*time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()
	approve(t, c, r)
	delegated := client.Client{Dir: c.Dir, Delegation: d}
	remote := filepath.Join(root, "download.txt")
	if err = os.WriteFile(remote, []byte("delegated file contents"), 0600); err != nil {
		t.Fatal(err)
	}
	m := transfer.NewWithClient(delegated)
	j, err := m.Add("fixture", "download", filepath.Join(c.Dir, "received.txt"), testserver.RemotePath(remote))
	if err != nil {
		t.Fatal(err)
	}
	if err = m.Run(ctx, j.ID, "read client"); err != nil {
		t.Fatal("delegated resumable download", err)
	}
	data, err := os.ReadFile(filepath.Join(c.Dir, "received.txt"))
	if err != nil || string(data) != "delegated file contents" {
		t.Fatal("download mismatch", err)
	}
	op := approval.Operation{Host: "fixture", Kind: "exec", Command: "printf unsafe", Client: "read client"}
	if _, _, err = delegated.Request(ctx, op, time.Minute); err == nil {
		t.Fatal("read scope ran arbitrary commands")
	}
	j, err = m.Add("fixture", "upload", filepath.Join(c.Dir, "received.txt"), testserver.RemotePath(filepath.Join(root, "forbidden.txt")))
	if err != nil {
		t.Fatal(err)
	}
	if err = m.Run(ctx, j.ID, "read client"); err == nil {
		t.Fatal("read scope uploaded a file")
	}
	if _, err = os.Stat(filepath.Join(root, "forbidden.txt")); !os.IsNotExist(err) {
		t.Fatal("denied upload touched remote target")
	}
}
