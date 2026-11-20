//go:build darwin || linux

package fileattrs

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"golang.org/x/sys/unix"
)

func TestOwnerCanRestoreUnreadableLocalPermissions(t *testing.T) {
	for _, directory := range []bool{false, true} {
		name := filepath.Join(t.TempDir(), "unreadable")
		mode := os.FileMode(0600)
		if directory {
			mode = 0700
			if err := os.Mkdir(name, mode); err != nil {
				t.Fatal(err)
			}
		} else if err := os.WriteFile(name, []byte("private data"), mode); err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = os.Chmod(name, mode) })
		if err := os.Chmod(name, 0); err != nil {
			t.Fatal(err)
		}
		before, err := LocalInfo(context.Background(), name)
		if err != nil {
			t.Fatal(err)
		}
		after, err := SetLocalAttributes(context.Background(), name, Change{Expected: before.Snapshot, SetMode: true, Mode: uint32(mode)})
		if err != nil || after.Mode&0777 != uint32(mode) {
			t.Fatalf("restore directory=%v: mode=%o, error=%v", directory, after.Mode, err)
		}
	}
}

func TestAttributeHandleChangesHeldInodeAfterPathReplacement(t *testing.T) {
	dir := t.TempDir()
	name, held, victim := filepath.Join(dir, "original"), filepath.Join(dir, "held"), filepath.Join(dir, "victim")
	for _, file := range []string{name, victim} {
		if err := os.WriteFile(file, []byte("content"), 0600); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.Chmod(name, 0); err != nil {
		t.Fatal(err)
	}
	f, err := openAttributeHandle(name)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	before, err := f.Stat()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(name, held); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(victim, name); err != nil {
		t.Fatal(err)
	}
	if _, err := Apply(f, Change{Expected: FromStat(before), SetMode: true, Mode: 0640}); err != nil {
		t.Fatal(err)
	}
	for path, want := range map[string]os.FileMode{held: 0640, victim: 0600} {
		info, err := os.Stat(path)
		if err != nil || info.Mode().Perm() != want {
			t.Fatalf("%s: wanted %o, info=%v, err=%v", path, want, info, err)
		}
	}
}

func TestAttributeHandleDoesNotBlockOnFIFOOrModifySymlink(t *testing.T) {
	dir := t.TempDir()
	fifo := filepath.Join(dir, "fifo")
	if err := unix.Mkfifo(fifo, 0600); err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() {
		f, err := openAttributeHandle(fifo)
		if err == nil {
			defer f.Close()
			st, statErr := f.Stat()
			if statErr != nil {
				err = statErr
			} else {
				_, err = Apply(f, Change{Expected: FromStat(st), SetMode: true, Mode: 0644})
			}
		}
		done <- err
	}()
	select {
	case err := <-done:
		if err == nil {
			t.Fatal("modified a FIFO")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("opening an attribute handle blocked on a FIFO")
	}
	victim, link := filepath.Join(dir, "victim"), filepath.Join(dir, "link")
	if err := os.WriteFile(victim, []byte("private"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(victim, link); err != nil {
		t.Fatal(err)
	}
	f, err := openAttributeHandle(link)
	if err == nil {
		defer f.Close()
		st, err := f.Stat()
		if err != nil {
			t.Fatal(err)
		}
		if _, err := Apply(f, Change{Expected: FromStat(st), SetMode: true, Mode: 0777}); err == nil {
			t.Fatal("modified a symlink through an attribute handle")
		}
	}
	st, err := os.Stat(victim)
	if err != nil || st.Mode().Perm() != 0600 {
		t.Fatal("symlink victim permissions changed", err)
	}
}
