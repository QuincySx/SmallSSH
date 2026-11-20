package transfer

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"testing"
	"time"

	"sshclient/internal/localipc"
)

func TestClearedTransferMetadataDoesNotGrowWithJobHistory(t *testing.T) {
	m := New(t.TempDir())
	for n := 0; n < 64; n++ {
		j := historyJob(t, m, "done")
		if err := m.RemoveCompleted(j.ID); err != nil {
			t.Fatal(err)
		}
		if _, err := os.Lstat(m.jobDir(j.ID)); !errors.Is(err, os.ErrNotExist) {
			t.Fatal("cleared job directory remains", err)
		}
	}
	entries, err := os.ReadDir(m.dir)
	if err != nil || len(entries) != 1 || entries[0].Name() != ".metadata.lock" {
		t.Fatal("metadata grew with cleared jobs", entries, err)
	}
	if jobs := New(filepath.Dir(m.dir)).List(); len(jobs) != 0 {
		t.Fatal("cleared history reappeared", jobs)
	}
}

func TestListingReclaimsLegacyLockOnlyDirectories(t *testing.T) {
	m := New(t.TempDir())
	for n := 0; n < 16; n++ {
		id := "abcdefghijklmnopqrstuv" + strconv.FormatInt(int64(10+n), 10)
		release, err := localipc.Acquire(m.jobDir(id))
		if err != nil {
			t.Fatal(err)
		}
		release()
	}
	busy := "abcdefghijklmnopqrstuvwx"
	release, err := localipc.Acquire(m.jobDir(busy))
	if err != nil {
		t.Fatal(err)
	}
	defer release()
	unknown := "zyxwvutsrqponmlkjihgfedc"
	if err := localipc.Prepare(m.jobDir(unknown)); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(m.jobDir(unknown), "unknown-data"), []byte("keep"), 0600); err != nil {
		t.Fatal(err)
	}
	if jobs := m.List(); len(jobs) != 0 {
		t.Fatal(jobs)
	}
	entries, err := os.ReadDir(m.dir)
	if err != nil || len(entries) != 3 { // one global lock + busy + unknown
		t.Fatal("legacy tombstones were not reclaimed", entries, err)
	}
	if _, err := os.Stat(m.jobDir(busy)); err != nil {
		t.Fatal("removed a held legacy job lock", err)
	}
	if data, err := os.ReadFile(filepath.Join(m.jobDir(unknown), "unknown-data")); err != nil || string(data) != "keep" {
		t.Fatal("removed unrecognised metadata", err)
	}
}

func TestStaleTransferCallsCannotRecreateRemovedJobDirectories(t *testing.T) {
	m := New(t.TempDir())
	j := historyJob(t, m, "done")
	if err := m.RemoveCompleted(j.ID); err != nil {
		t.Fatal(err)
	}
	if err := m.Run(context.Background(), j.ID, "stale frontend"); err == nil {
		t.Fatal("ran a removed job")
	}
	if err := m.Cancel(context.Background(), j.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Lstat(m.jobDir(j.ID)); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("stale call recreated a removed job", err)
	}
}

func TestJobAcquisitionWaitsForMetadataRemoval(t *testing.T) {
	m := New(t.TempDir())
	j := historyJob(t, m, "done")
	release, err := m.lockMetadata(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	done := make(chan error, 1)
	go func() {
		unlock, err := New(filepath.Dir(m.dir)).lockJob(ctx, j.ID)
		if err == nil {
			unlock()
		}
		done <- err
	}()
	if err := os.RemoveAll(m.jobDir(j.ID)); err != nil {
		release()
		t.Fatal(err)
	}
	release()
	if err := <-done; !errors.Is(err, os.ErrNotExist) {
		t.Fatal("acquisition did not observe completed removal", err)
	}
	if _, err := os.Lstat(m.jobDir(j.ID)); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("job lock namespace was recreated", err)
	}
}

func TestTransferMetadataLockIsSharedAcrossProcesses(t *testing.T) {
	const childDirectory = "SSH_CLIENT_TEST_TRANSFER_LOCK_DIR"
	if directory := os.Getenv(childDirectory); directory != "" {
		m := New(directory)
		ctx, cancel := context.WithTimeout(context.Background(), 150*time.Millisecond)
		defer cancel()
		if release, err := m.lockMetadata(ctx); !errors.Is(err, context.DeadlineExceeded) {
			if release != nil {
				release()
			}
			t.Fatal("another process's metadata lock was ignored", err)
		}
		return
	}
	directory := t.TempDir()
	m := New(directory)
	release, err := m.lockMetadata(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	defer release()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	command := exec.CommandContext(ctx, os.Args[0], "-test.run=^TestTransferMetadataLockIsSharedAcrossProcesses$")
	command.Env = append(os.Environ(), childDirectory+"="+directory)
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("cross-process metadata lock: %v\n%s", err, output)
	}
}
