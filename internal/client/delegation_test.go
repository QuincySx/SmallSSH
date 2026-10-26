package client

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func fileDelegation(t *testing.T) *Delegation {
	t.Helper()
	pub, key, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	if err = os.Chmod(dir, 0700); err != nil {
		t.Fatal(err)
	}
	return &Delegation{ParentID: "generated-parent", Directory: dir, Key: key, Ticket: "generated-client-withdraw-ticket", Permission: "read", Host: "fixture", PublicKey: base64.RawURLEncoding.EncodeToString(pub)}
}

func TestDelegationFilePrivateRoundtripAndNoOverwrite(t *testing.T) {
	d := fileDelegation(t)
	defer d.Close()
	if err := d.Save("ai.json"); err != nil {
		t.Fatal(err)
	}
	if err := d.Save("ai.json"); err == nil {
		t.Fatal("overwrote another session")
	}
	got, err := ReadDelegation("ai.json", d.Directory)
	if err != nil {
		t.Fatal(err)
	}
	defer got.Close()
	if !ed25519.PrivateKey(got.Key).Equal(ed25519.PrivateKey(d.Key)) || got.ParentID != d.ParentID {
		t.Fatal("roundtrip changed client authority")
	}
	if err = got.Validate(t.TempDir()); err == nil {
		t.Fatal("client authority moved to another broker directory")
	}
	st, err := os.Stat(filepath.Join(d.Directory, "client-sessions", "ai.json"))
	if err != nil || runtime.GOOS != "windows" && st.Mode().Perm() != 0600 {
		t.Fatal("session file has unsafe permissions", err)
	}
	got.Close()
	if len(got.Key) != 0 || got.Ticket != "" {
		t.Fatal("closed client retained its authority")
	}
}

func TestDelegationFileRejectsPublicPathsSymlinksAndCorruptKey(t *testing.T) {
	d := fileDelegation(t)
	defer d.Close()
	for _, path := range []string{"", ".", "..", "../outside.json", filepath.Join(t.TempDir(), "outside.json")} {
		if err := d.Save(path); err == nil {
			t.Fatal("saved authority outside the private directory", path)
		}
	}
	if err := d.Save("original.json"); err != nil {
		t.Fatal(err)
	}
	if runtime.GOOS != "windows" {
		link := filepath.Join(d.Directory, "client-sessions", "link.json")
		if err := os.Symlink(filepath.Join(d.Directory, "client-sessions", "original.json"), link); err != nil {
			t.Fatal(err)
		}
		if _, err := ReadDelegation("link.json", d.Directory); err == nil {
			t.Fatal("followed a session-file symlink")
		}
		path := filepath.Join(d.Directory, "client-sessions", "original.json")
		if err := os.Chmod(path, 0644); err != nil {
			t.Fatal(err)
		}
		if _, err := ReadDelegation("original.json", d.Directory); err == nil {
			t.Fatal("accepted an exposed session file")
		}
	}
	d.Key[len(d.Key)-1] ^= 1
	if err := d.Validate(d.Directory); err == nil {
		t.Fatal("accepted a corrupt Ed25519 private/public pair")
	}
}
