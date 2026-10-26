package sshconn_test

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"io"
	"os"
	"path/filepath"
	"sshclient/internal/sshconn"
	"sshclient/internal/testserver"
	"testing"
	"time"
)

func TestPasswordKeyExecAndWrongHost(t *testing.T) {
	s := testserver.New(t)
	p := s.Profile()
	p.Fingerprint = "SHA256:wrong"
	if c, e := sshconn.Dial(context.Background(), p); e == nil {
		c.Close()
		t.Fatal("accepted wrong server")
	}
	if s.Auths.Load() != 0 {
		t.Fatal("password offered before host verification")
	}
	for _, auth := range []string{"password", "key"} {
		p = s.Profile()
		p.Auth = auth
		p.PrivateKey = s.PrivateKey
		c, e := sshconn.Dial(context.Background(), p)
		if e != nil {
			t.Fatal(e)
		}
		r, e := c.Exec(context.Background(), "exit7")
		c.Close()
		if e != nil || r.ExitCode != 7 || r.Stdout != "fixture stdout: exit7" || r.Stderr != "fixture stderr" {
			t.Fatal(r, e)
		}
	}
}
func TestSFTPIntegrityNoOverwriteAndDownload(t *testing.T) {
	s := testserver.New(t)
	c, e := sshconn.Dial(context.Background(), s.Profile())
	if e != nil {
		t.Fatal(e)
	}
	defer c.Close()
	dir := t.TempDir()
	p := testserver.RemotePath(filepath.Join(dir, "new.txt"))
	content := []byte("hello file\n")
	h := sha256.Sum256(content)
	hash := hex.EncodeToString(h[:])
	if e = c.Upload(context.Background(), p, bytes.NewReader(content), "incorrect"); e == nil {
		t.Fatal("accepted changed content")
	}
	if _, e = os.Stat(testserver.LocalPath(p)); !os.IsNotExist(e) {
		t.Fatal("failed upload committed")
	}
	if e = c.Upload(context.Background(), p, bytes.NewReader(content), hash); e != nil {
		t.Fatal(e)
	}
	if e = c.Upload(context.Background(), p, bytes.NewReader(content), hash); e == nil {
		t.Fatal("overwrote existing target")
	}
	var out bytes.Buffer
	if e = c.Download(context.Background(), p, &out); e != nil || !bytes.Equal(out.Bytes(), content) {
		t.Fatal(e, out.String())
	}
	list, e := c.List(context.Background(), testserver.RemotePath(dir))
	if e != nil || len(list) != 1 || list[0].Name != "new.txt" {
		t.Fatal(list, e)
	}
	if e = c.Download(context.Background(), "/tmp/../escape", io.Discard); e == nil {
		t.Fatal("non-canonical path accepted")
	}
}
func TestShellResizeAndCancellation(t *testing.T) {
	s := testserver.New(t)
	ctx, cancel := context.WithCancel(context.Background())
	c, e := sshconn.Dial(ctx, s.Profile())
	if e != nil {
		t.Fatal(e)
	}
	defer c.Close()
	sh, e := c.Shell(ctx, 80, 24)
	if e != nil {
		t.Fatal(e)
	}
	defer sh.Close()
	if v := <-s.Sizes; v != [2]uint32{80, 24} {
		t.Fatal(v)
	}
	if e = sh.Resize(100, 40); e != nil {
		t.Fatal(e)
	}
	if v := <-s.Sizes; v != [2]uint32{100, 40} {
		t.Fatal(v)
	}
	if _, e = sh.Write([]byte("echo")); e != nil {
		t.Fatal(e)
	}
	b := make([]byte, 4)
	if _, e = io.ReadFull(sh, b); e != nil || string(b) != "echo" {
		t.Fatal(string(b), e)
	}
	cancel()
	done := make(chan struct{})
	go func() { sh.Read(b); close(done) }()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("shell survived cancellation")
	}
}

func TestEditorConflictDetectionAndIndependentBackup(t *testing.T) {
	s := testserver.New(t)
	c, e := sshconn.Dial(context.Background(), s.Profile())
	if e != nil {
		t.Fatal(e)
	}
	defer c.Close()
	dir := t.TempDir()
	p := testserver.RemotePath(filepath.Join(dir, "config.txt"))
	old := []byte("old config\n")
	updated := []byte("new config\n")
	os.WriteFile(testserver.LocalPath(p), old, 0640)
	h := sha256.Sum256(old)
	oldHash := hex.EncodeToString(h[:])
	h = sha256.Sum256(updated)
	newHash := hex.EncodeToString(h[:])
	if _, e = c.Replace(context.Background(), p, bytes.NewReader(updated), newHash, "wrong original hash"); e == nil {
		t.Fatal("ignored conflict")
	}
	got, _ := os.ReadFile(testserver.LocalPath(p))
	if !bytes.Equal(got, old) {
		t.Fatal("conflict changed target")
	}
	backup, e := c.Replace(context.Background(), p, bytes.NewReader(updated), newHash, oldHash)
	if e != nil {
		t.Fatal(e)
	}
	got, _ = os.ReadFile(testserver.LocalPath(p))
	if !bytes.Equal(got, updated) {
		t.Fatal("not saved")
	}
	got, _ = os.ReadFile(testserver.LocalPath(backup))
	if !bytes.Equal(got, old) {
		t.Fatal("backup lost original")
	}
	os.WriteFile(testserver.LocalPath(p), []byte("external later write"), 0640)
	got, _ = os.ReadFile(testserver.LocalPath(backup))
	if !bytes.Equal(got, old) {
		t.Fatal("backup shared inode")
	}
	link := testserver.RemotePath(filepath.Join(dir, "link"))
	if e = os.Symlink(testserver.LocalPath(p), testserver.LocalPath(link)); e == nil {
		if _, e = c.Replace(context.Background(), link, bytes.NewReader(updated), newHash, oldHash); e == nil {
			t.Fatal("overwrote symlink")
		}
	}
}

func TestDownloadRejectsDirectoryAndInitialTerminalSizeIsBounded(t *testing.T) {
	fixture := testserver.New(t)
	ctx := context.Background()
	c, err := sshconn.Dial(ctx, fixture.Profile())
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	if err = c.Download(ctx, testserver.RemotePath(t.TempDir()), io.Discard); err == nil {
		t.Fatal("directory accepted as download")
	}
	for _, size := range [][2]int{{1001, 24}, {80, 1001}, {-1, 24}} {
		if sh, err := c.Shell(ctx, size[0], size[1]); err == nil {
			sh.Close()
			t.Fatal("invalid terminal size accepted", size)
		}
	}
}
