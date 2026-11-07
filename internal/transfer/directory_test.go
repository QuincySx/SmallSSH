package transfer

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"sshclient/internal/broker"
	"sshclient/internal/testserver"
	"sshclient/internal/vault"
)

func TestDirectoryDownloadPreservesTreeResumesAndNeverOverwrites(t *testing.T) {
	var executions atomic.Int32
	fixture := testserver.NewWithExec(t, func(string) (string, string, int) { executions.Add(1); return "", "", 1 })
	dir, err := os.MkdirTemp("", "directory-transfer-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(dir) })
	owner, err := vault.New([]byte("synthetic directory download owner password"))
	if err != nil {
		t.Fatal(err)
	}
	profile := fixture.Profile()
	if err = owner.Put(profile); err != nil {
		t.Fatal(err)
	}
	srv, err := broker.OpenSession(dir, owner)
	if err != nil {
		owner.Close()
		t.Fatal(err)
	}
	t.Cleanup(srv.Close)
	m := New(dir)
	ctx := context.Background()
	source := t.TempDir()
	if err = os.MkdirAll(filepath.Join(source, "nested", "empty"), 0700); err != nil {
		t.Fatal(err)
	}
	for name, body := range map[string]string{"plain.txt": "one\n", "nested/中文.conf": "二\n"} {
		if err = os.WriteFile(filepath.Join(source, filepath.FromSlash(name)), []byte(body), 0600); err != nil {
			t.Fatal(err)
		}
	}
	endpoint := fmt.Sprintf("%s@%s", profile.User, fixture.Listener.Addr())
	destination := filepath.Join(t.TempDir(), "download")
	j, err := m.AddDownload(profile.ID, destination, testserver.RemotePath(source), endpoint, profile.Fingerprint, true)
	if err != nil {
		t.Fatal(err)
	}
	if err = m.RunWithOwner(ctx, j.ID, "directory test", owner); err != nil {
		t.Fatal(err)
	}
	jobs := m.List()
	if len(jobs) != 1 || jobs[0].State != "done" || jobs[0].FilesDone != 2 || jobs[0].FilesTotal != 2 || jobs[0].Bytes != jobs[0].Size {
		t.Fatal("group progress or child visibility incorrect", jobs)
	}
	if body, err := os.ReadFile(filepath.Join(destination, "nested", "中文.conf")); err != nil || string(body) != "二\n" {
		t.Fatal("nested file differs", string(body), err)
	}
	if st, err := os.Stat(filepath.Join(destination, "nested", "empty")); err != nil || !st.IsDir() {
		t.Fatal("empty directory lost", err)
	}
	if executions.Load() != 0 {
		t.Fatal("directory download executed a remote command")
	}
	// Simulate a stopped group after its child completed, including a missing
	// local file. Reload must use the persisted manifest and fresh read grants.
	j, _ = m.read(j.ID)
	j.State = "paused"
	os.WriteFile(filepath.Join(destination, ".smallssh-download-"+j.ID), []byte(j.ID), 0600)
	if err = m.save(j); err != nil {
		t.Fatal(err)
	}
	if err = os.Remove(filepath.Join(destination, "plain.txt")); err != nil {
		t.Fatal(err)
	}
	m = New(dir)
	if err = m.RunWithOwner(ctx, j.ID, "directory resumed", owner); err != nil {
		t.Fatal(err)
	}
	if body, err := os.ReadFile(filepath.Join(destination, "plain.txt")); err != nil || string(body) != "one\n" {
		t.Fatal(string(body), err)
	}
	j, _ = m.read(j.ID)
	j.State = "paused"
	os.WriteFile(filepath.Join(destination, ".smallssh-download-"+j.ID), []byte(j.ID), 0600)
	m.save(j)
	os.WriteFile(filepath.Join(destination, "plain.txt"), []byte("mine\n"), 0600)
	if err = m.RunWithOwner(ctx, j.ID, "do not overwrite", owner); err == nil {
		t.Fatal("overwrote a changed local file")
	}
	if body, _ := os.ReadFile(filepath.Join(destination, "plain.txt")); string(body) != "mine\n" {
		t.Fatal("changed file lost")
	}
	if _, err = m.AddDownload(profile.ID, destination, testserver.RemotePath(source), endpoint, profile.Fingerprint, true); err == nil {
		t.Fatal("accepted an existing destination")
	}
	// Endpoint snapshots apply to every scan and every child, not only to the
	// initial UI click, so editing a profile cannot redirect a queued group.
	changed, err := m.AddDownload(profile.ID, filepath.Join(t.TempDir(), "changed"), testserver.RemotePath(source), "someone@127.0.0.1:22", profile.Fingerprint, true)
	if err != nil {
		t.Fatal(err)
	}
	if err = m.RunWithOwner(ctx, changed.ID, "changed identity", owner); err == nil {
		t.Fatal("queued directory followed a changed endpoint")
	}
	// An ordinary CLI request is bound to the broker's current profile. The
	// transfer must compare that immutable response with its own snapshot
	// before waiting for approval, even when it is not an owner operation.
	for _, directory := range []bool{false, true} {
		drifted, err := m.AddDownload(profile.ID, filepath.Join(t.TempDir(), "drifted"), testserver.RemotePath(source), "someone@127.0.0.1:22", profile.Fingerprint, directory)
		if err != nil {
			t.Fatal(err)
		}
		bounded, cancel := context.WithTimeout(ctx, 2*time.Second)
		err = m.Run(bounded, drifted.ID, "CLI target drift")
		cancel()
		if err == nil || !strings.Contains(err.Error(), "连接目标已变化") {
			t.Fatal("CLI resume waited for or followed another target", err)
		}
	}
	if runtime.GOOS != "windows" {
		// Even a directory replaced by a local link during a paused transfer
		// cannot make a remote file name escape the opened destination root.
		outside := t.TempDir()
		if err = os.RemoveAll(filepath.Join(destination, "nested")); err != nil {
			t.Fatal(err)
		}
		if err = os.Symlink(outside, filepath.Join(destination, "nested")); err != nil {
			t.Fatal(err)
		}
		if err = m.RunWithOwner(ctx, j.ID, "contained resume", owner); err == nil {
			t.Fatal("followed a local link out of the destination")
		}
		if entries, err := os.ReadDir(outside); err != nil || len(entries) != 0 {
			t.Fatal("download escaped its destination", entries, err)
		}
		linkedSource := t.TempDir()
		if err = os.Symlink(source, filepath.Join(linkedSource, "linked")); err != nil {
			t.Fatal(err)
		}
		linked, err := m.AddDownload(profile.ID, filepath.Join(t.TempDir(), "linked"), testserver.RemotePath(linkedSource), endpoint, profile.Fingerprint, true)
		if err != nil {
			t.Fatal(err)
		}
		if err = m.RunWithOwner(ctx, linked.ID, "symlink rejection", owner); err == nil || !strings.Contains(err.Error(), "符号链接") {
			t.Fatal("followed a remote link", err)
		}
	}
}

func TestDirectoryScanCanPauseAndResumeWithFreshRequests(t *testing.T) {
	var accepting atomic.Bool
	started := make(chan struct{}, 1)
	fixture := testserver.NewWithSessionGate(t, func() bool {
		select {
		case started <- struct{}{}:
		default:
		}
		return accepting.Load()
	})
	dir, err := os.MkdirTemp("", "directory-pause-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(dir) })
	owner, err := vault.New([]byte("synthetic pausable directory owner password"))
	if err != nil {
		t.Fatal(err)
	}
	p := fixture.Profile()
	if err = owner.Put(p); err != nil {
		t.Fatal(err)
	}
	srv, err := broker.OpenSession(dir, owner)
	if err != nil {
		owner.Close()
		t.Fatal(err)
	}
	t.Cleanup(srv.Close)
	m := New(dir)
	destination := filepath.Join(t.TempDir(), "empty-folder")
	j, err := m.AddDownload(p.ID, destination, testserver.RemotePath(t.TempDir()), fmt.Sprintf("%s@%s", p.User, fixture.Listener.Addr()), p.Fingerprint, true)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- m.RunWithOwner(ctx, j.ID, "pause scan", owner) }()
	select {
	case <-started:
	case <-ctx.Done():
		t.Fatal("scan never reached the stalled SFTP channel")
	}
	New(dir).Pause(j.ID)
	select {
	case err := <-done:
		if err == nil {
			t.Fatal("paused scan completed")
		}
	case <-ctx.Done():
		t.Fatal("pause left SFTP scan blocked")
	}
	if jobs := m.List(); len(jobs) != 1 || jobs[0].State != "paused" {
		t.Fatal("scan was not persisted as paused", jobs)
	}
	accepting.Store(true)
	m = New(dir)
	if err = m.RunWithOwner(ctx, j.ID, "resume scan", owner); err != nil {
		t.Fatal(err)
	}
	if jobs := m.List(); len(jobs) != 1 || jobs[0].State != "done" || jobs[0].FilesTotal != 0 {
		t.Fatal("empty folder could not resume", jobs)
	}
	if st, err := os.Stat(destination); err != nil || !st.IsDir() {
		t.Fatal("empty root folder missing", err)
	}
}

func TestDirectoryManifestRejectsTraversalAliasesAndSpecialNames(t *testing.T) {
	for _, name := range []string{"../escape", "/absolute", "a/../../outside", "a\\b", "CON", "a/..", "trailing.", ".smallssh-download-other"} {
		manifest := directoryManifest{Version: 1, Files: []directoryFile{{Path: name, Size: 1}}}
		if validateManifest(manifest) == nil {
			t.Errorf("accepted %q", name)
		}
	}
	if validateManifest(directoryManifest{Version: 1, Folders: []string{"Config"}, Files: []directoryFile{{Path: "config", Size: 1}}}) == nil {
		t.Fatal("accepted a file/directory case collision")
	}
}
