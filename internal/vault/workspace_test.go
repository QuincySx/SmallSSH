package vault_test

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"

	"sshclient/internal/snippet"
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
	command := snippet.Command{ID: "s1", Name: "status", Group: "Ops", Body: "printf secret-command-content $1",
		Variants: []snippet.Variant{{OS: snippet.OSRedHat, Body: "printf redhat-command-content"}},
		Args:     []snippet.Arg{{Type: snippet.TypeInt, Preset: "42"}}}
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
	stored := reopened.Snippets()
	if len(stored) != 1 || len(stored[0].Variants) != 1 || stored[0].Variants[0].OS != snippet.OSRedHat || len(stored[0].Args) != 1 || stored[0].Args[0].Preset != "42" {
		t.Fatal("profile save lost commands or their variants", stored)
	}
	if e = reopened.SaveSnippet(file, snippet.Command{ID: "s2", Name: "bad", Body: "echo $1", Args: []snippet.Arg{{Type: snippet.TypeInt, Preset: "many"}}}, false); e == nil {
		t.Fatal("invalid parameter preset was stored")
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

func TestSnippetParametersAndVariantsDoNotAliasTheVault(t *testing.T) {
	pass := []byte("synthetic snippet isolation password")
	v, err := vault.New(pass)
	if err != nil {
		t.Fatal(err)
	}
	defer v.Close()
	file := filepath.Join(t.TempDir(), "vault.json")
	if err := vault.Save(file, v, true); err != nil {
		t.Fatal(err)
	}
	for _, action := range []string{"insert", "replace"} {
		t.Run(action, func(t *testing.T) {
			command := snippet.Command{ID: "snippet", Name: "snippet", Body: "echo $1",
				Args:     []snippet.Arg{{Preset: "original"}},
				Variants: []snippet.Variant{{OS: snippet.OSDebian, Body: "echo debian"}}}
			if err := v.SaveSnippet(file, command, false); err != nil {
				t.Fatal(err)
			}
			command.Args[0].Preset = "changed after save"
			command.Variants[0].Body = "changed after save"
			snapshot := v.Snippets()
			if snapshot[0].Args[0].Preset != "original" || snapshot[0].Variants[0].Body != "echo debian" {
				t.Fatal("caller-owned slices changed a saved command", snapshot)
			}
			snapshot[0].Args[0].Preset = "changed through getter"
			snapshot[0].Variants[0].Body = "changed through getter"
			got := v.Snippets()[0]
			if got.Args[0].Preset != "original" || got.Variants[0].Body != "echo debian" {
				t.Fatal("returned slices changed the stored command", got)
			}
			reopened, err := vault.Read(file, pass)
			if err != nil {
				t.Fatal(err)
			}
			defer reopened.Close()
			stored := reopened.Snippets()[0]
			if stored.Args[0] != got.Args[0] || stored.Variants[0] != got.Variants[0] {
				t.Fatal("memory and disk disagree after caller edits", stored, got)
			}
		})
	}
}
