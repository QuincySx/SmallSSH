package fileattrs

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestDarwinUnreadableFilePinIsRemovedOnClose(t *testing.T) {
	dir := t.TempDir()
	name := filepath.Join(dir, "unreadable")
	if err := os.WriteFile(name, []byte("content"), 0); err != nil {
		t.Fatal(err)
	}
	f, err := openAttributeHandle(name)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	pin, ok := f.(*darwinAttributeHandle)
	if !ok || pin.pin == "" {
		t.Fatal("unreadable regular file did not receive an inode pin")
	}
	if err := f.Chmod(0600); err != nil {
		t.Fatal(err)
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}
	if err := f.Close(); err != nil {
		t.Fatal("attribute handle cleanup was not idempotent", err)
	}
	entries, err := os.ReadDir(dir)
	if err != nil || len(entries) != 1 || entries[0].Name() != "unreadable" {
		t.Fatal("private inode pin was left behind", entries, err)
	}
}

func TestDarwinUnreadableDirectoryFallbackRejectsReplacement(t *testing.T) {
	dir := t.TempDir()
	name, held, victim := filepath.Join(dir, "original"), filepath.Join(dir, "held"), filepath.Join(dir, "victim")
	if err := os.Mkdir(name, 0); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(victim, 0700); err != nil {
		t.Fatal(err)
	}
	f, err := openAttributeHandle(name)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	if err := os.Rename(name, held); err != nil {
		t.Fatal(err)
	}
	defer os.Chmod(held, 0700)
	if err := os.Mkdir(name, 0); err != nil {
		t.Fatal(err)
	}
	if err := f.Chmod(0777); err == nil {
		t.Fatal("changed a replacement directory")
	}
	if err := os.Remove(name); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(victim, name); err != nil {
		t.Fatal(err)
	}
	if err := f.Chmod(0777); err == nil {
		t.Fatal("changed a replacement symlink")
	}
	st, err := os.Stat(victim)
	if err != nil || st.Mode().Perm() != 0700 {
		t.Fatal("symlink victim was modified", err)
	}
}

func TestDarwinUnreadableFileCanBeRepairedInReadOnlyParent(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root bypasses the parent directory write restriction")
	}
	dir := t.TempDir()
	name := filepath.Join(dir, "unreadable")
	if err := os.WriteFile(name, []byte("content"), 0); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(dir, 0500); err != nil {
		t.Fatal(err)
	}
	defer os.Chmod(dir, 0700)
	before, err := LocalInfo(context.Background(), name)
	if err != nil {
		t.Fatal(err)
	}
	after, err := SetLocalAttributes(context.Background(), name, Change{Expected: before.Snapshot, SetMode: true, Mode: 0600})
	if err != nil || after.Mode != 0600 {
		t.Fatal("permission recovery unnecessarily required parent write access", after, err)
	}
}
