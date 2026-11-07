package vault_test

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"sshclient/internal/snippet"
	"sshclient/internal/vault"
)

func TestParameterHistoryIsEncryptedScopedBoundedAndSurvivesReopen(t *testing.T) {
	pass := []byte("synthetic parameter history password")
	v, err := vault.New(pass)
	if err != nil {
		t.Fatal(err)
	}
	defer v.Close()
	file := filepath.Join(t.TempDir(), "vault.json")
	if err := vault.Save(file, v, true); err != nil {
		t.Fatal(err)
	}
	command := snippet.Command{ID: "container", Name: "container", Body: "pct config $1 && echo $2 $3",
		Variants: []snippet.Variant{{OS: snippet.OSDebian, Body: "pct config $1 && printf $2 $3"}},
		Args:     []snippet.Arg{{Name: "ContainerID", Type: snippet.TypeInt}, {Name: "Label"}, {Name: "Private", NoHistory: true}}}
	if err := v.SaveSnippet(file, command, false); err != nil {
		t.Fatal(err)
	}
	a, b := strings.Repeat("a", 64), strings.Repeat("b", 64)
	for i := 0; i < 12; i++ {
		if err := v.SaveParameterHistory(file, command, a, snippet.OSDefault, []string{fmt.Sprint(100 + i), "private-history-value", "never-record-this"}, -1); err != nil {
			t.Fatal(err)
		}
	}
	if err := v.SaveParameterHistory(file, command, a, snippet.OSDefault, []string{"105", "private-history-value", "never-record-this"}, -1); err != nil {
		t.Fatal(err)
	}
	got := v.ParameterHistory(command.ID, a, snippet.OSDefault, 0)
	if len(got) != snippet.MaxInputHistory || got[0] != "105" || got[1] != "111" {
		t.Fatal("history was not recent-first, bounded and deduplicated", got)
	}
	got[0] = "mutated"
	if v.ParameterHistory(command.ID, a, snippet.OSDefault, 0)[0] != "105" {
		t.Fatal("getter exposed mutable history")
	}
	metadata := command.Clone()
	metadata.Name = "renamed after the run dialog opened"
	if err := v.SaveSnippet(file, metadata, false); err != nil {
		t.Fatal(err)
	}
	if v.ParameterHistory(command.ID, a, snippet.OSDefault, 0)[0] != "105" {
		t.Fatal("an old editor snapshot erased current history")
	}
	if len(v.ParameterHistory(command.ID, a, snippet.OSDefault, 2)) != 0 {
		t.Fatal("opted-out parameter was recorded")
	}
	if len(v.ParameterHistory(command.ID, b, snippet.OSDefault, 0)) != 0 || len(v.ParameterHistory(command.ID, a, snippet.OSDebian, 0)) != 0 {
		t.Fatal("history crossed server or system scopes")
	}
	if err := v.SaveParameterHistory(file, command, b, snippet.OSDebian, []string{"200", "other-context", "private"}, -1); err != nil {
		t.Fatal(err)
	}
	profile := vault.Profile{ID: "test", Name: "test", Host: "127.0.0.1", Port: 22, User: "test", Auth: "password", Password: "synthetic", Fingerprint: "SHA256:test"}
	if err := v.SaveProfile(file, profile); err != nil {
		t.Fatal(err)
	}
	if err := v.SaveServerNote(file, profile.ID, vault.ServerNote{Manual: "synthetic note"}); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(file)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(data, []byte("private-history-value")) || bytes.Contains(data, []byte("other-context")) {
		t.Fatal("history leaked outside ciphertext")
	}
	reopened, err := vault.Read(file, pass)
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	if got := reopened.ParameterHistory(command.ID, b, snippet.OSDebian, 0); !reflect.DeepEqual(got, []string{"200"}) {
		t.Fatal("other vault writes lost history", got)
	}
	if reopened.Snippets()[0].History != nil {
		t.Fatal("command discovery exported all input histories")
	}
	if err := reopened.SaveParameterHistory(file, command, a, snippet.OSDefault, nil, 0); err != nil {
		t.Fatal(err)
	}
	if len(reopened.ParameterHistory(command.ID, a, snippet.OSDefault, 0)) != 0 || len(reopened.ParameterHistory(command.ID, a, snippet.OSDefault, 1)) != 1 || len(reopened.ParameterHistory(command.ID, b, snippet.OSDebian, 0)) != 1 {
		t.Fatal("clear affected the wrong parameter or context")
	}
	reopened.Close()
	if got := reopened.ParameterHistory(command.ID, a, snippet.OSDefault, 1); got != nil {
		t.Fatal("locked vault returned history")
	}
}

func TestParameterHistoryOptOutDefinitionChangesAndStaleWriters(t *testing.T) {
	v, err := vault.New([]byte("synthetic private history password"))
	if err != nil {
		t.Fatal(err)
	}
	defer v.Close()
	file := filepath.Join(t.TempDir(), "vault.json")
	if err := vault.Save(file, v, true); err != nil {
		t.Fatal(err)
	}
	command := snippet.Command{ID: "test", Name: "test", Body: "echo $1 $2", Args: []snippet.Arg{{Name: "first"}, {Name: "second"}}}
	target := strings.Repeat("c", 64)
	if err := v.SaveSnippet(file, command, false); err != nil {
		t.Fatal(err)
	}
	if err := v.SaveParameterHistory(file, command, target, snippet.OSDefault, []string{"remember-first", "remember-second"}, -1); err != nil {
		t.Fatal(err)
	}
	changed := command.Clone()
	changed.Name = "renamed"
	changed.Args[0].NoHistory = true
	if err := v.SaveSnippet(file, changed, false); err != nil {
		t.Fatal(err)
	}
	if got := v.ParameterHistory(command.ID, target, snippet.OSDefault, 1); len(got) != 1 {
		t.Fatal("unrelated parameter history was cleared", got)
	}
	if err := v.SaveParameterHistory(file, command, target, snippet.OSDefault, []string{"late", "late"}, -1); err == nil {
		t.Fatal("stale run restored opted-out history")
	}
	changed.Args[0].NoHistory = false
	if err := v.SaveSnippet(file, changed, false); err != nil {
		t.Fatal(err)
	}
	if len(v.ParameterHistory(command.ID, target, snippet.OSDefault, 0)) != 0 {
		t.Fatal("opting out merely hid retained history")
	}
	changed.Args[1].Name = "different meaning"
	if err := v.SaveSnippet(file, changed, false); err != nil {
		t.Fatal(err)
	}
	if len(v.ParameterHistory(command.ID, target, snippet.OSDefault, 1)) != 0 {
		t.Fatal("new parameter meaning inherited old input")
	}
	if err := v.SaveParameterHistory(file, changed, target, snippet.OSDefault, []string{strings.Repeat("x", 513), "line\nreturn"}, -1); err != nil {
		t.Fatal(err)
	}
	if len(v.ParameterHistory(command.ID, target, snippet.OSDefault, 0)) != 0 || len(v.ParameterHistory(command.ID, target, snippet.OSDefault, 1)) != 0 {
		t.Fatal("oversized or multiline history was retained")
	}
}
