package vault

import (
	"fmt"
	"path/filepath"
	"testing"

	"sshclient/internal/snippet"
)

func TestWorkspaceWritesPreserveAuditAnchor(t *testing.T) {
	const (
		updatedProfileName = "renamed by audit anchor test"
		updatedSnippetName = "updated audit anchor command"
		insertedSnippetID  = "audit-anchor-inserted"
		savedNoteText      = "replacement audit anchor note"
	)
	historyTarget := fmt.Sprintf("%064x", 0xfeed)

	newFixture := func(t *testing.T, anchor string) (*Session, string, Profile, snippet.Command) {
		t.Helper()
		v, err := New(testPass)
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(v.Close)
		file := filepath.Join(t.TempDir(), "vault.json")
		p := profile()
		if err = v.Put(p); err != nil {
			t.Fatal(err)
		}
		command := snippet.Command{
			ID:   "audit-anchor",
			Name: "audit anchor command",
			Body: "printf '%s' $1",
			Args: []snippet.Arg{{Name: "value"}},
		}
		if err = Save(file, v, true); err != nil {
			t.Fatal(err)
		}
		if err = v.SaveSnippet(file, command, false); err != nil {
			t.Fatal(err)
		}
		if err = v.SaveServerNote(file, p.ID, ServerNote{Manual: "synthetic audit anchor note"}); err != nil {
			t.Fatal(err)
		}
		v.SetAuditHead(anchor)
		if err = Save(file, v, false); err != nil {
			t.Fatal(err)
		}
		return v, file, p, command
	}

	findSnippet := func(t *testing.T, v *Session, id string) (snippet.Command, bool) {
		t.Helper()
		for _, command := range v.Snippets() {
			if command.ID == id {
				return command, true
			}
		}
		return snippet.Command{}, false
	}

	tests := []struct {
		name   string
		mutate func(*Session, string, Profile, snippet.Command) error
		verify func(*testing.T, *Session, Profile, snippet.Command)
	}{
		{
			name: "SaveProfile",
			mutate: func(v *Session, file string, p Profile, _ snippet.Command) error {
				p.Name = updatedProfileName
				return v.SaveProfile(file, p)
			},
			verify: func(t *testing.T, v *Session, p Profile, _ snippet.Command) {
				t.Helper()
				got, err := v.Profile(p.ID)
				if err != nil || got.Name != updatedProfileName {
					t.Fatalf("profile mutation was not persisted: %v %#v", err, got)
				}
			},
		},
		{
			name: "SaveSnippetInsert",
			mutate: func(v *Session, file string, _ Profile, _ snippet.Command) error {
				return v.SaveSnippet(file, snippet.Command{
					ID:   insertedSnippetID,
					Name: "inserted audit anchor command",
					Body: "printf '%s' $1",
					Args: []snippet.Arg{{Name: "value"}},
				}, false)
			},
			verify: func(t *testing.T, v *Session, _ Profile, _ snippet.Command) {
				t.Helper()
				got, ok := findSnippet(t, v, insertedSnippetID)
				if !ok || got.Name != "inserted audit anchor command" {
					t.Fatalf("inserted snippet was not persisted: %#v", got)
				}
			},
		},
		{
			name: "SaveSnippetUpdate",
			mutate: func(v *Session, file string, _ Profile, command snippet.Command) error {
				command.Name = updatedSnippetName
				return v.SaveSnippet(file, command, false)
			},
			verify: func(t *testing.T, v *Session, _ Profile, command snippet.Command) {
				t.Helper()
				got, ok := findSnippet(t, v, command.ID)
				if !ok || got.Name != updatedSnippetName {
					t.Fatalf("updated snippet was not persisted: %#v", got)
				}
			},
		},
		{
			name: "SaveSnippetDelete",
			mutate: func(v *Session, file string, _ Profile, command snippet.Command) error {
				return v.SaveSnippet(file, snippet.Command{ID: command.ID}, true)
			},
			verify: func(t *testing.T, v *Session, _ Profile, command snippet.Command) {
				t.Helper()
				if got, ok := findSnippet(t, v, command.ID); ok {
					t.Fatalf("deleted snippet remained: %#v", got)
				}
			},
		},
		{
			name: "SaveServerNoteSave",
			mutate: func(v *Session, file string, p Profile, _ snippet.Command) error {
				return v.SaveServerNote(file, p.ID, ServerNote{Manual: savedNoteText})
			},
			verify: func(t *testing.T, v *Session, p Profile, _ snippet.Command) {
				t.Helper()
				got, err := v.ServerNote(p.ID)
				if err != nil || got.Manual != savedNoteText {
					t.Fatalf("saved note was not persisted: %v %#v", err, got)
				}
			},
		},
		{
			name: "SaveServerNoteClear",
			mutate: func(v *Session, file string, p Profile, _ snippet.Command) error {
				return v.SaveServerNote(file, p.ID, ServerNote{})
			},
			verify: func(t *testing.T, v *Session, p Profile, _ snippet.Command) {
				t.Helper()
				got, err := v.ServerNote(p.ID)
				if err != nil || got != (ServerNote{}) {
					t.Fatalf("cleared note was not persisted: %v %#v", err, got)
				}
			},
		},
		{
			name: "SaveParameterHistoryControl",
			mutate: func(v *Session, file string, _ Profile, command snippet.Command) error {
				return v.SaveParameterHistory(file, command, historyTarget, snippet.OSDefault, []string{"42"}, -1)
			},
			verify: func(t *testing.T, v *Session, _ Profile, command snippet.Command) {
				t.Helper()
				got := v.ParameterHistory(command.ID, historyTarget, snippet.OSDefault, 0)
				if len(got) != 1 || got[0] != "42" {
					t.Fatalf("parameter history was not persisted: %#v", got)
				}
			},
		},
		{
			name: "DeleteProfile",
			mutate: func(v *Session, file string, p Profile, _ snippet.Command) error {
				return v.DeleteProfile(file, p.ID)
			},
			verify: func(t *testing.T, v *Session, p Profile, _ snippet.Command) {
				t.Helper()
				if got, err := v.Profile(p.ID); err == nil {
					t.Fatalf("deleted profile remained: %#v", got)
				}
			},
		},
	}

	for i, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			anchor := fmt.Sprintf("%064x", i+1)
			v, file, p, command := newFixture(t, anchor)
			if err := test.mutate(v, file, p, command); err != nil {
				t.Fatalf("mutation failed: %v", err)
			}
			reopened, err := Read(file, testPass)
			if err != nil {
				t.Fatalf("reopen after mutation: %v", err)
			}
			defer reopened.Close()
			if got := reopened.AuditHead(); got != anchor {
				t.Fatalf("stored audit anchor = %q, want %q", got, anchor)
			}
			if got := v.AuditHead(); got != anchor {
				t.Fatalf("session audit anchor = %q, want %q", got, anchor)
			}
			test.verify(t, reopened, p, command)
		})
	}
}
