package broker_test

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"sshclient/internal/approval"
	"sshclient/internal/broker"
	"sshclient/internal/client"
	"sshclient/internal/localipc"
	"sshclient/internal/testserver"
	"sshclient/internal/vault"
)

var pass = []byte("broker test long independent password")

func setup(t *testing.T) (client.Client, *testserver.Server) {
	t.Helper()
	fixture := testserver.New(t)
	dir, e := os.MkdirTemp("", "ssh-client-test-")
	if e != nil {
		t.Fatal(e)
	}
	t.Cleanup(func() { os.RemoveAll(dir) })
	if e = localipc.Prepare(dir); e != nil {
		t.Fatal(e)
	}
	v, e := vault.New(pass)
	if e != nil {
		t.Fatal(e)
	}
	v.Put(fixture.Profile())
	if e = vault.Save(broker.VaultPath(dir), v, true); e != nil {
		t.Fatal(e)
	}
	v.Close()
	s, e := broker.Open(dir, pass)
	if e != nil {
		t.Fatal(e)
	}
	t.Cleanup(s.Close)
	return client.Client{Dir: dir}, fixture
}
func request(t *testing.T, c client.Client, op approval.Operation) (approval.Request, string) {
	t.Helper()
	r, ticket, e := c.Request(context.Background(), op, time.Minute)
	if e != nil {
		t.Fatal(e)
	}
	return r, ticket
}
func approve(t *testing.T, c client.Client, r approval.Request) {
	t.Helper()
	if e := c.Decide(context.Background(), r.ID, true, false, pass); e != nil {
		t.Fatal(e)
	}
}
func TestBrokerEndToEndApprovalNoSecretsAndReplay(t *testing.T) {
	c, fixture := setup(t)
	ctx := context.Background()
	h, e := c.Call(ctx, broker.Message{Action: "hosts"})
	if e != nil {
		t.Fatal(e)
	}
	b, _ := json.Marshal(h)
	if bytes.Contains(b, []byte(fixture.Profile().Password)) || bytes.Contains(b, []byte("PRIVATE KEY")) {
		t.Fatal("credentials exposed")
	}
	r, ticket := request(t, c, approval.Operation{Host: "fixture", Kind: "exec", Command: "exit7", Client: "AI test"})
	if _, e = c.Start(ctx, r.ID, ticket, 0, 0); e == nil {
		t.Fatal("unapproved command executed")
	}
	if _, e = c.Call(ctx, broker.Message{Action: "decide", ID: r.ID, Allow: true, Proof: []byte("forgery")}); e == nil {
		t.Fatal("forged approval")
	}
	if e = c.Decide(ctx, r.ID, true, false, []byte("wrong password")); e == nil {
		t.Fatal("wrong owner password")
	}
	approve(t, c, r)
	s, e := c.Start(ctx, r.ID, ticket, 0, 0)
	if e != nil {
		t.Fatal(e)
	}
	var out, stderr bytes.Buffer
	code, _, e := s.Copy(&out, &stderr)
	s.Close()
	if e != nil || code != 7 || out.String() != "fixture stdout: exit7" || stderr.String() != "fixture stderr" {
		t.Fatal(code, e, out.String(), stderr.String())
	}
	if _, e = c.Start(ctx, r.ID, ticket, 0, 0); e == nil {
		t.Fatal("ticket replay")
	}
}
func TestBrokerRevocationDisconnectAndExclusiveLock(t *testing.T) {
	c, fixture := setup(t)
	if release, e := localipc.Acquire(c.Dir); e == nil {
		release()
		t.Fatal("second authority acquired vault")
	}
	r, ticket := request(t, c, approval.Operation{Host: "fixture", Kind: "exec", Command: "block", Client: "AI test"})
	approve(t, c, r)
	s, e := c.Start(context.Background(), r.ID, ticket, 0, 0)
	if e != nil {
		t.Fatal(e)
	}
	defer s.Close()
	<-fixture.Started
	done := make(chan error, 1)
	go func() { _, _, e := s.Copy(io.Discard, io.Discard); done <- e }()
	if e = c.Decide(context.Background(), r.ID, false, true, pass); e != nil {
		t.Fatal(e)
	}
	select {
	case e := <-done:
		if e == nil {
			t.Fatal("revoked command succeeded")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("revoked stream still open")
	}
}
func TestBrokerFileStreamingAndHashBinding(t *testing.T) {
	c, _ := setup(t)
	ctx := context.Background()
	p := testserver.RemotePath(filepath.Join(t.TempDir(), "large.bin"))
	body := bytes.Repeat([]byte("upload-and-download\n"), 80000)
	h := sha256.Sum256(body)
	r, ticket := request(t, c, approval.Operation{Host: "fixture", Kind: "put", Path: p, ContentHash: hex.EncodeToString(h[:]), Client: "AI test"})
	approve(t, c, r)
	s, e := c.Start(ctx, r.ID, ticket, 0, 0)
	if e != nil {
		t.Fatal(e)
	}
	if _, e = s.Write(body); e != nil {
		t.Fatal(e)
	}
	s.End()
	if _, _, e = s.Copy(io.Discard, io.Discard); e != nil {
		t.Fatal(e)
	}
	s.Close()
	r, ticket = request(t, c, approval.Operation{Host: "fixture", Kind: "get", Path: p, Client: "AI test"})
	approve(t, c, r)
	s, e = c.Start(ctx, r.ID, ticket, 0, 0)
	if e != nil {
		t.Fatal(e)
	}
	defer s.Close()
	var out bytes.Buffer
	if _, _, e = s.Copy(&out, io.Discard); e != nil || !bytes.Equal(out.Bytes(), body) {
		t.Fatal("stream roundtrip failed", e)
	}
}
func TestOwnerProfileChallengeOneUseAndOldGrantsInvalidated(t *testing.T) {
	c, fixture := setup(t)
	ctx := context.Background()
	r, ticket := request(t, c, approval.Operation{Host: "fixture", Kind: "exec", Command: "old", Client: "AI test"})
	approve(t, c, r)
	p := fixture.Profile()
	p.Name = "renamed"
	challenge, e := c.Call(ctx, broker.Message{Action: "profileChallenge"})
	if e != nil {
		t.Fatal(e)
	}
	v, e := vault.Read(broker.VaultPath(c.Dir), pass)
	if e != nil {
		t.Fatal(e)
	}
	defer v.Close()
	proof, _ := broker.ProfileProof(challenge.ID, p, v.Proof)
	q := broker.Message{Action: "profilePut", ID: challenge.ID, Profile: &p, Proof: proof}
	if _, e = c.Call(ctx, q); e != nil {
		t.Fatal(e)
	}
	if _, e = c.Call(ctx, q); e == nil {
		t.Fatal("owner challenge replay")
	}
	if _, e = c.Start(ctx, r.ID, ticket, 0, 0); e == nil {
		t.Fatal("grant survived profile update")
	}
	got, e := vault.Read(broker.VaultPath(c.Dir), pass)
	if e != nil {
		t.Fatal(e)
	}
	defer got.Close()
	saved, e := got.Profile(p.ID)
	if e != nil || saved.Name != "renamed" {
		t.Fatal("profile update not persisted", e)
	}
}
func TestApprovalDisplayEscapesTerminalControlSequences(t *testing.T) {
	s := client.Describe(approval.Request{Operation: approval.Operation{Client: "\x1b[2J", Command: "echo\rhide"}})
	if strings.ContainsRune(s, '\x1b') || strings.ContainsRune(s, '\r') {
		t.Fatal("terminal control sequence in approval prompt")
	}
}

func TestGroupedDetailsPreserveCredentialsAndCancelGrants(t *testing.T) {
	c, f := setup(t)
	ctx := context.Background()
	r, _ := request(t, c, approval.Operation{Host: "fixture", Kind: "shell", Client: "test"})
	owner, e := vault.Read(broker.VaultPath(c.Dir), pass)
	if e != nil {
		t.Fatal(e)
	}
	defer owner.Close()
	h, e := c.Call(ctx, broker.Message{Action: "hosts"})
	if e != nil {
		t.Fatal(e)
	}
	d := h.Connections[0]
	d.Group = "生产 / 个人"
	d.Name = "renamed"
	if e = c.PutDetails(ctx, d, owner); e != nil {
		t.Fatal(e)
	}
	h, e = c.Call(ctx, broker.Message{Action: "hosts"})
	if e != nil || h.Connections[0].Group != d.Group {
		t.Fatal(h, e)
	}
	reopened, e := vault.Read(broker.VaultPath(c.Dir), pass)
	if e != nil {
		t.Fatal(e)
	}
	defer reopened.Close()
	p, e := reopened.Profile("fixture")
	if e != nil || p.Password != f.Profile().Password {
		t.Fatal("credentials lost", e)
	}
	if _, e = c.Call(ctx, broker.Message{Action: "status", ID: r.ID}); e == nil {
		t.Fatal("old grant survived edit")
	}
}
