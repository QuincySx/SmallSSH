package transfer

import (
	"os"
	"path/filepath"
	"testing"

	"sshclient/internal/localipc"
)

func historyJob(t *testing.T, m *Manager, state string) Job {
	t.Helper()
	j, err := m.Add("fixture", "download", filepath.Join(t.TempDir(), "download.txt"), "/remote/file")
	if err != nil {
		t.Fatal(err)
	}
	j.State = state
	if err := m.save(j); err != nil {
		t.Fatal(err)
	}
	return j
}

func TestCompletedHistoryRemovalPersistsAndPreservesFiles(t *testing.T) {
	dir := t.TempDir()
	m := New(dir)
	j := historyJob(t, m, "done")
	if err := os.WriteFile(j.Local, []byte("downloaded content"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := m.RemoveCompleted(j.ID); err != nil {
		t.Fatal(err)
	}
	if err := m.RemoveCompleted(j.ID); err != nil {
		t.Fatal("repeated removal", err)
	}
	if jobs := New(dir).List(); len(jobs) != 0 {
		t.Fatal("history returned after restart", jobs)
	}
	if b, err := os.ReadFile(j.Local); err != nil || string(b) != "downloaded content" {
		t.Fatal("download changed", err)
	}
}

func TestClearCompletedRetainsUnfinishedAndBusyJobs(t *testing.T) {
	m := New(t.TempDir())
	for _, state := range []string{"queued", "paused", "error"} {
		j := historyJob(t, m, state)
		if err := m.RemoveCompleted(j.ID); err == nil {
			t.Fatal("removed unfinished job", state)
		}
	}
	busy := historyJob(t, m, "done")
	free := historyJob(t, m, "done")
	release, err := localipc.Acquire(m.jobDir(busy.ID))
	if err != nil {
		t.Fatal(err)
	}
	if err := m.ClearCompleted(); err == nil {
		t.Fatal("ignored a busy job")
	}
	release()
	if _, err := m.read(free.ID); !os.IsNotExist(err) {
		t.Fatal("did not clear the available completed job", err)
	}
	if len(m.List()) != 4 {
		t.Fatal("cleared unfinished or busy jobs", m.List())
	}
	if err := m.ClearCompleted(); err != nil {
		t.Fatal(err)
	}
	if len(m.List()) != 3 {
		t.Fatal(m.List())
	}
}

func TestDirectoryHistoryClearsNestedMetadataWithoutFollowingLinks(t *testing.T) {
	m := New(t.TempDir())
	j := historyJob(t, m, "done")
	j.Directory = true
	if err := m.save(j); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(j.Local, 0700); err != nil {
		t.Fatal(err)
	}
	file := filepath.Join(j.Local, "keep.txt")
	if err := os.WriteFile(file, []byte("keep"), 0600); err != nil {
		t.Fatal(err)
	}
	children := filepath.Join(m.jobDir(j.ID), "children")
	if err := os.MkdirAll(children, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(children, "state.json"), []byte("child metadata"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(j.Local, filepath.Join(children, "outside")); err != nil {
		t.Skip("symlinks unavailable", err)
	}
	if err := os.WriteFile(filepath.Join(m.jobDir(j.ID), "manifest.json"), []byte("manifest"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := m.RemoveCompleted(j.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(children); !os.IsNotExist(err) {
		t.Fatal("nested metadata remains", err)
	}
	if b, err := os.ReadFile(file); err != nil || string(b) != "keep" {
		t.Fatal("followed a metadata symlink", err)
	}
}

func TestHistoryRemovalRejectsInvalidIDsAndLinkedRecords(t *testing.T) {
	m := New(t.TempDir())
	j := historyJob(t, m, "done")
	if err := m.RemoveCompleted("../" + j.ID); err == nil {
		t.Fatal("accepted path traversal")
	}
	alias := "abcdefghijklmnopqrstuvwx"
	if err := os.Symlink(m.jobDir(j.ID), m.jobDir(alias)); err != nil {
		t.Skip("symlinks unavailable", err)
	}
	if err := m.RemoveCompleted(alias); err == nil {
		t.Fatal("followed a linked job directory")
	}
	if _, err := m.read(j.ID); err != nil {
		t.Fatal("changed original record", err)
	}
}
