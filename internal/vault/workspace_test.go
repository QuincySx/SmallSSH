package vault_test

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"

	"sshclient/internal/vault"
)

func TestCommandsEncryptedAndPreservedAcrossProfileWrites(t *testing.T) {
	pass := []byte("independent test master password")
	v, e := vault.New(pass)
	if e != nil {
		t.Fatal(e)
	}
	defer v.Close()
	file := filepath.Join(t.TempDir(), "vault.json")
	if e = vault.Save(file, v, true); e != nil {
		t.Fatal(e)
	}
	command := vault.Snippet{ID: "s1", Name: "status", Group: "Ops", Body: "printf secret-command-content"}
	if e = v.SaveSnippet(file, command, false); e != nil {
		t.Fatal(e)
	}
	p := vault.Profile{ID: "a", Name: "A", Host: "localhost", User: "test", Port: 22, Auth: "password", Password: "SSH test password", Fingerprint: "SHA256:test"}
	if e = v.SaveProfile(file, p); e != nil {
		t.Fatal(e)
	}
	data, _ := os.ReadFile(file)
	if bytes.Contains(data, []byte(command.Body)) || bytes.Contains(data, []byte(p.Password)) {
		t.Fatal("plaintext leaked")
	}
	reopened, e := vault.Read(file, pass)
	if e != nil {
		t.Fatal(e)
	}
	defer reopened.Close()
	if len(reopened.Snippets()) != 1 {
		t.Fatal("profile save lost commands")
	}
	if e = reopened.DeleteProfile(file, "a"); e != nil {
		t.Fatal(e)
	}
	if _, e = reopened.Profile("a"); e == nil {
		t.Fatal("profile remained")
	}
	if len(reopened.Snippets()) != 1 {
		t.Fatal("profile deletion lost commands")
	}
	if e = reopened.SaveSnippet(file, command, true); e != nil {
		t.Fatal(e)
	}
	if len(reopened.Snippets()) != 0 {
		t.Fatal("command remained")
	}
}
