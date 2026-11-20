package transfer

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"testing"
	"time"

	"sshclient/internal/broker"
	"sshclient/internal/client"
	"sshclient/internal/localipc"
	"sshclient/internal/testserver"
	"sshclient/internal/vault"
)

func writePartial(t *testing.T, path string, mode os.FileMode) {
	t.Helper()
	if err := os.WriteFile(path, []byte("partial bytes"), mode); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(path, mode); err != nil {
		t.Fatal(err)
	}
}

func TestDiscardFailedDownloadDeletesOnlyItsPartial(t *testing.T) {
	m := New(t.TempDir())
	j := historyJob(t, m, "error")
	part := filepath.Join(filepath.Dir(j.Local), partialName(filepath.Base(j.Local), j.ID))
	other := filepath.Join(filepath.Dir(j.Local), partialName(filepath.Base(j.Local), "someone-else"))
	writePartial(t, part, 0600)
	writePartial(t, other, 0600)
	if err := m.Discard(j.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Lstat(part); !os.IsNotExist(err) {
		t.Fatal("partial kept", err)
	}
	if _, err := os.Lstat(other); err != nil {
		t.Fatal("deleted another job's partial", err)
	}
	if jobs := New(m.dir).List(); len(jobs) != 0 {
		t.Fatal("failed record returned", jobs)
	}
	if err := m.Discard(j.ID); err != nil {
		t.Fatal("repeated discard", err)
	}
}

func TestDiscardKeepsUnexpectedPartialAndRecord(t *testing.T) {
	m := New(t.TempDir())
	j := historyJob(t, m, "paused")
	part := filepath.Join(filepath.Dir(j.Local), partialName(filepath.Base(j.Local), j.ID))
	writePartial(t, part, 0644)
	if err := m.Discard(j.ID); err == nil {
		t.Fatal("deleted a partial that is not private")
	}
	if _, err := os.Lstat(part); err != nil {
		t.Fatal("unexpected partial was touched", err)
	}
	if len(m.List()) != 1 {
		t.Fatal("record dropped although cleanup failed")
	}
}

func TestDiscardRefusesARunningJobAndCancelStopsThenDiscards(t *testing.T) {
	m := New(t.TempDir())
	j := historyJob(t, m, "running")
	release, err := localipc.Acquire(m.jobDir(j.ID))
	if err != nil {
		t.Fatal(err)
	}
	if err := m.Discard(j.ID); err == nil {
		t.Fatal("discarded a job another run holds")
	}
	// Cancel asks the run to stop; the run releases its lock when it ends.
	stopped := make(chan struct{})
	m.mu.Lock()
	m.active[j.ID] = func() { close(stopped) }
	m.mu.Unlock()
	go func() {
		<-stopped
		time.Sleep(50 * time.Millisecond)
		m.mu.Lock()
		delete(m.active, j.ID)
		m.mu.Unlock()
		release()
	}()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := m.Cancel(ctx, j.ID); err != nil {
		t.Fatal(err)
	}
	if len(m.List()) != 0 {
		t.Fatal("cancelled job still listed")
	}
	if err := m.Cancel(ctx, j.ID); err != nil {
		t.Fatal("cancelling a removed job", err)
	}
}

func TestCancelTimesOutWhileTheRunKeepsTheJob(t *testing.T) {
	m := New(t.TempDir())
	j := historyJob(t, m, "running")
	release, err := localipc.Acquire(m.jobDir(j.ID))
	if err != nil {
		t.Fatal(err)
	}
	defer release()
	ctx, cancel := context.WithTimeout(context.Background(), 300*time.Millisecond)
	defer cancel()
	if err := m.Cancel(ctx, j.ID); err == nil {
		t.Fatal("cancel claimed success while the job still ran")
	}
	if len(m.List()) != 1 {
		t.Fatal("record removed under a live run")
	}
}

func TestDiscardDirectoryRemovesUnfinishedPartialsKeepsFinishedFiles(t *testing.T) {
	m := New(t.TempDir())
	target := filepath.Join(t.TempDir(), "site")
	j, err := m.Add("fixture", "download", target, "/remote/site")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(target, "logs"), 0700); err != nil {
		t.Fatal(err)
	}
	j.Directory, j.State = true, "paused"
	if err := m.save(j); err != nil {
		t.Fatal(err)
	}
	marker := filepath.Join(target, ".smallssh-download-"+j.ID)
	if err := os.WriteFile(marker, []byte(j.ID), 0600); err != nil {
		t.Fatal(err)
	}
	// Same layout the folder download uses for its per-file state.
	children := New(t.TempDir())
	children.dir = filepath.Join(m.jobDir(j.ID), "children")
	finished := Job{ID: "aaaaaaaaaaaaaaaaaaaaaaaa", ParentID: j.ID, Direction: "download", Local: filepath.Join(target, "index.html"), State: "done"}
	pending := Job{ID: "bbbbbbbbbbbbbbbbbbbbbbbb", ParentID: j.ID, Direction: "download", Local: filepath.Join(target, "logs", "a.log"), State: "paused"}
	for _, child := range []Job{finished, pending} {
		if err := children.save(child); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(finished.Local, []byte("done"), 0600); err != nil {
		t.Fatal(err)
	}
	pendingPart := filepath.Join(target, "logs", partialName("a.log", pending.ID))
	writePartial(t, pendingPart, 0600)
	if err := m.Discard(j.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Lstat(pendingPart); !os.IsNotExist(err) {
		t.Fatal("unfinished child partial kept", err)
	}
	if _, err := os.Lstat(marker); !os.IsNotExist(err) {
		t.Fatal("ownership marker kept", err)
	}
	if b, err := os.ReadFile(finished.Local); err != nil || string(b) != "done" {
		t.Fatal("finished file changed", err)
	}
	if len(m.List()) != 0 {
		t.Fatal("folder record kept")
	}
}

func TestClearFailedLeavesOtherStates(t *testing.T) {
	m := New(t.TempDir())
	for _, state := range []string{"error", "error", "paused", "done", "queued"} {
		historyJob(t, m, state)
	}
	if err := m.ClearFailed(); err != nil {
		t.Fatal(err)
	}
	for _, j := range m.List() {
		if j.State == "error" {
			t.Fatal("failed job kept")
		}
	}
	if len(m.List()) != 3 {
		t.Fatal(m.List())
	}
}

// A live run in another frontend is stopped through the shared pause marker,
// its authorization is withdrawn, and only then are the partial and record removed.
func TestCancelStopsALiveDownloadAndDeletesItsPartial(t *testing.T) {
	fixture := testserver.New(t)
	dir, e := os.MkdirTemp("", "sw-cancel-")
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
	cli := client.Client{Dir: dir, Owner: srv.Vault}
	body := bytes.Repeat([]byte("cancellable payload\n"), 50000)
	remote := testserver.RemotePath(filepath.Join(t.TempDir(), "remote"))
	if err := os.WriteFile(testserver.LocalPath(remote), body, 0600); err != nil {
		t.Fatal(err)
	}
	dest := filepath.Join(t.TempDir(), "download")
	m := New(dir)
	d, e := m.Add("fixture", "download", dest, remote)
	if e != nil {
		t.Fatal(e)
	}
	h := sha256.Sum256(body)
	d.Hash, d.Size, d.Bytes = hex.EncodeToString(h[:]), int64(len(body)), 4096
	if e = m.save(d); e != nil {
		t.Fatal(e)
	}
	part := filepath.Join(filepath.Dir(dest), partialName(filepath.Base(dest), d.ID))
	writePartial(t, part, 0600)
	done := make(chan error, 1)
	go func() { done <- m.Run(context.Background(), d.ID, "test") }()
	waiting := waitJob(t, m, d.ID, "waiting")
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := New(dir).Cancel(ctx, d.ID); err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-done:
		if err == nil {
			t.Fatal("cancelled run reported success")
		}
	case <-time.After(5 * time.Second):
		t.Fatal("run did not stop")
	}
	if _, err := os.Lstat(part); !os.IsNotExist(err) {
		t.Fatal("partial kept after cancel", err)
	}
	if _, err := os.Lstat(dest); !os.IsNotExist(err) {
		t.Fatal("cancel created the target", err)
	}
	if jobs := New(dir).List(); len(jobs) != 0 {
		t.Fatal("cancelled job still listed", jobs)
	}
	r, err := cli.Call(context.Background(), broker.Message{Action: "status", ID: waiting.RequestID})
	if err != nil || r.Request == nil || r.Request.Status == "pending" || r.Request.Status == "approved" {
		t.Fatal("cancelled request kept its authority", r.Request, err)
	}
}
