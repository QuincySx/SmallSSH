package transfer

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"testing"

	"sshclient/internal/accesspolicy"
	"sshclient/internal/broker"
	"sshclient/internal/client"
	"sshclient/internal/localipc"
	"sshclient/internal/testserver"
	"sshclient/internal/vault"
)

// A protected host refuses resumed writes, because SFTP cannot prove an existing
// remote partial is not a hard link into a protected tree. An unprivileged upload
// that was interrupted must therefore fall back to one bounded first-time upload
// instead of staying refused forever.
func TestProtectedHostUploadFallsBackToABoundedRetry(t *testing.T) {
	fixture := testserver.New(t)
	dir, e := os.MkdirTemp("", "sw-fallback-")
	if e != nil {
		t.Fatal(e)
	}
	t.Cleanup(func() { os.RemoveAll(dir) })
	os.Chmod(dir, 0o700)
	pass := []byte("fallback long master password")
	localipc.Prepare(dir)
	v, _ := vault.New(pass)
	profile := fixture.Profile()
	profile.Policy = accesspolicy.Policy{ProtectedPaths: []string{"/etc"}}
	v.Put(profile)
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
	body := bytes.Repeat([]byte("bounded payload\n"), 50000)
	if e = os.WriteFile(source, body, 0600); e != nil {
		t.Fatal(e)
	}
	// The protected path guard rejects any symlinked ancestor, and macOS keeps
	// temporary directories behind /var -> /private/var, so resolve it first.
	target, e := filepath.EvalSymlinks(t.TempDir())
	if e != nil {
		t.Fatal(e)
	}
	remote := testserver.RemotePath(filepath.Join(target, "remote"))
	m := New(dir)
	j, e := m.Add("fixture", "upload", source, remote)
	if e != nil {
		t.Fatal(e)
	}
	// An earlier attempt was interrupted, so the job looks like a resumed write.
	j.State, j.Bytes = "running", 4096
	if e = m.save(j); e != nil {
		t.Fatal(e)
	}
	m = New(dir)
	done := make(chan error, 1)
	go func() { done <- m.Run(ctx, j.ID, "test") }()
	pending := waitJob(t, m, j.ID, "waiting")
	if e = cli.Decide(ctx, pending.RequestID, true, false, pass); e != nil {
		t.Fatal(e)
	}
	if e = <-done; e != nil {
		t.Fatalf("upload stayed refused instead of falling back: %v", e)
	}
	got, e := os.ReadFile(testserver.LocalPath(remote))
	if e != nil {
		t.Fatal(e)
	}
	if !bytes.Equal(got, body) {
		t.Fatal("fallback upload content differs from the source")
	}
	if job := m.List(); len(job) != 1 || job[0].State != "done" || job[0].Bytes != int64(len(body)) {
		t.Fatalf("job did not finish as a complete upload: %+v", job)
	}
}
