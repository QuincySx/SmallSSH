package transfer

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"sshclient/internal/approval"
	"sshclient/internal/broker"
	"sshclient/internal/client"
	"sshclient/internal/localipc"
	"sshclient/internal/testserver"
	"sshclient/internal/vault"
	"testing"
	"time"
)

func waitJob(t *testing.T, m *Manager, id, state string) Job {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		for _, j := range m.List() {
			if j.ID == id && j.State == state {
				return j
			}
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatal("job did not reach", state, m.List())
	return Job{}
}
func TestPersistentResumeRequiresFreshGrantAndChecksIntegrity(t *testing.T) {
	fixture := testserver.New(t)
	dir, e := os.MkdirTemp("", "sw-transfer-")
	if e != nil {
		t.Fatal(e)
	}
	t.Cleanup(func() { os.RemoveAll(dir) })
	os.Chmod(dir, 0700)
	pass := []byte("transfer long master password")
	localipc.Prepare(dir)
	v, _ := vault.New(pass)
	v.Put(fixture.Profile())
	vault.Save(broker.VaultPath(dir), v, true)
	v.Close()
	srv, e := broker.Open(dir, pass)
	if e != nil {
		t.Fatal(e)
	}
	defer srv.Close()
	ctx := context.Background()
	cli := client.Client{Dir: dir, Owner: srv.Vault}
	source := filepath.Join(t.TempDir(), "source")
	body := bytes.Repeat([]byte("resumable payload\n"), 250000)
	os.WriteFile(source, body, 0600)
	remote := testserver.RemotePath(filepath.Join(t.TempDir(), "remote"))
	m := New(dir)
	j, e := m.Add("fixture", "upload", source, remote)
	if e != nil {
		t.Fatal(e)
	}
	// Simulate a stopped process with a real remote prefix already on disk.
	cut := int64(900007)
	part := filepath.Join(filepath.Dir(testserver.LocalPath(remote)), ".ssh-part-"+j.ID)
	os.WriteFile(part, body[:cut], 0600)
	j.State = "running"
	j.Bytes = cut
	if e = m.save(j); e != nil {
		t.Fatal(e)
	}
	m = New(dir)
	if got := m.List(); len(got) != 1 || got[0].State != "paused" {
		t.Fatal(got)
	}
	done := make(chan error, 1)
	go func() { done <- m.Run(ctx, j.ID, "test") }()
	waiting := waitJob(t, m, j.ID, "waiting")
	// Pausing a pending task also withdraws its old authorization ticket.
	New(dir).Pause(j.ID) // A different frontend/process can request a pause.
	if e = <-done; e == nil {
		t.Fatal("pause succeeded as completed")
	}
	r, _ := cli.Call(ctx, broker.Message{Action: "status", ID: waiting.RequestID})
	if r.Request == nil || r.Request.Status != "withdrawn" {
		t.Fatal(r)
	}
	m = New(dir)
	go func() { done <- m.Run(ctx, j.ID, "test") }()
	again := waitJob(t, m, j.ID, "waiting")
	if again.RequestID == waiting.RequestID {
		t.Fatal("grant reused")
	}
	if e = cli.Decide(ctx, again.RequestID, true, false, pass); e != nil {
		t.Fatal(e)
	}
	if e = <-done; e != nil {
		t.Fatal(e)
	}
	got, _ := os.ReadFile(testserver.LocalPath(remote))
	if !bytes.Equal(got, body) {
		t.Fatal("resumed upload differs")
	}
	// Download restart binds both the known full hash and local prefix.
	dest := filepath.Join(t.TempDir(), "download")
	d, e := m.Add("fixture", "download", dest, remote)
	if e != nil {
		t.Fatal(e)
	}
	h := sha256.Sum256(body)
	d.Hash = hex.EncodeToString(h[:])
	d.Size = int64(len(body))
	d.Bytes = cut
	d.State = "running"
	m.save(d)
	localPart := filepath.Join(filepath.Dir(dest), "."+filepath.Base(dest)+".ssh-part-"+d.ID)
	os.WriteFile(localPart, body[:cut], 0600)
	m = New(dir)
	go func() { done <- m.Run(ctx, d.ID, "test") }()
	waiting = waitJob(t, m, d.ID, "waiting")
	if e = cli.Decide(ctx, waiting.RequestID, true, false, pass); e != nil {
		t.Fatal(e)
	}
	if e = <-done; e != nil {
		t.Fatal(e)
	}
	got, _ = os.ReadFile(dest)
	if !bytes.Equal(got, body) {
		t.Fatal("resumed download differs")
	}
	// A crash between the final commit and state write must not require an
	// overwrite or another download when the complete local hash matches.
	d.State = "running"
	if e = m.save(d); e != nil {
		t.Fatal(e)
	}
	m = New(dir)
	if e = m.Run(ctx, d.ID, "crash recovery"); e != nil {
		t.Fatal(e)
	}
	state, _ := os.ReadFile(filepath.Join(m.jobDir(d.ID), "state.json"))
	var values map[string]any
	json.Unmarshal(state, &values)
	if _, ok := values["ticket"]; ok {
		t.Fatal("grant ticket persisted")
	}
	if bytes.Contains(state, []byte(fixture.Profile().Password)) {
		t.Fatal("credential persisted")
	}
	// Corruption refuses mixed files and leaves the final target absent.
	badDest := filepath.Join(t.TempDir(), "bad")
	bad, _ := m.Add("fixture", "download", badDest, remote)
	bad.Hash = d.Hash
	bad.Size = d.Size
	m.save(bad)
	os.WriteFile(filepath.Join(filepath.Dir(badDest), ".bad.ssh-part-"+bad.ID), []byte("corrupt"), 0600)
	go func() { done <- m.Run(ctx, bad.ID, "test") }()
	waiting = waitJob(t, m, bad.ID, "waiting")
	cli.Decide(ctx, waiting.RequestID, true, false, pass)
	if e = <-done; e == nil {
		t.Fatal("corrupted prefix accepted")
	}
	if _, e = os.Stat(badDest); !os.IsNotExist(e) {
		t.Fatal("bad download committed")
	}
}

func TestOwnerTransfersReuseSessionAndLeaveUnrelatedRequestPending(t *testing.T) {
	fixture := testserver.New(t)
	dir, err := os.MkdirTemp("", "owner-transfer-")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(dir)
	if err = localipc.Prepare(dir); err != nil {
		t.Fatal(err)
	}
	pass := []byte("isolated owner transfer password")
	v, err := vault.New(pass)
	if err != nil {
		t.Fatal(err)
	}
	if err = v.Put(fixture.Profile()); err != nil {
		t.Fatal(err)
	}
	if err = vault.Save(broker.VaultPath(dir), v, true); err != nil {
		t.Fatal(err)
	}
	v.Close()
	server, err := broker.Open(dir, pass)
	if err != nil {
		t.Fatal(err)
	}
	defer server.Close()
	ctx := context.Background()
	cli := client.Client{Dir: dir, Owner: server.Vault}
	request, _, err := cli.Request(ctx, approval.Operation{Host: "fixture", Kind: "exec", Command: "external", Client: "desktop spoof"}, time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	src := filepath.Join(t.TempDir(), "source")
	body := bytes.Repeat([]byte("owner transfer\n"), 1200)
	if err = os.WriteFile(src, body, 0600); err != nil {
		t.Fatal(err)
	}
	remote := testserver.RemotePath(filepath.Join(t.TempDir(), "upload.txt"))
	m := New(dir)
	up, err := m.Add("fixture", "upload", src, remote)
	if err != nil {
		t.Fatal(err)
	}
	if err = m.RunWithOwner(ctx, up.ID, "desktop", server.Vault); err != nil {
		t.Fatal(err)
	}
	dest := filepath.Join(t.TempDir(), "download.txt")
	down, err := m.Add("fixture", "download", dest, remote)
	if err != nil {
		t.Fatal(err)
	}
	if err = m.RunWithOwner(ctx, down.ID, "desktop", server.Vault); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(dest)
	if err != nil || !bytes.Equal(got, body) {
		t.Fatal("owner transfer content differs", err)
	}
	state, err := cli.Call(ctx, broker.Message{Action: "status", ID: request.ID})
	if err != nil || state.Request == nil || state.Request.Status != "pending" {
		t.Fatal("external request approved", err)
	}
	for _, job := range m.List() {
		if job.State != "done" {
			t.Fatal("job incomplete", job.State)
		}
	}
}
