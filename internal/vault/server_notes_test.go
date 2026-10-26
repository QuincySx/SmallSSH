package vault

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestServerNotesEncryptedPrivateAndPreservedAcrossWrites(t *testing.T) {
	v, err := New(testPass)
	if err != nil {
		t.Fatal(err)
	}
	defer v.Close()
	p := profile()
	if err = v.Put(p); err != nil {
		t.Fatal(err)
	}
	file := filepath.Join(t.TempDir(), "vault.json")
	if err = Save(file, v, true); err != nil {
		t.Fatal(err)
	}
	note := ServerNote{Manual: "private reminder: staging database", Discovered: "private service summary", Details: "private service inventory", CheckedAt: time.Now().UTC(), Target: "fixture-target"}
	if err = v.SaveServerNote(file, p.ID, note); err != nil {
		t.Fatal(err)
	}
	p.Name = "renamed"
	if err = v.SaveProfile(file, p); err != nil {
		t.Fatal(err)
	}
	if err = v.SaveSnippet(file, Snippet{ID: "test", Name: "status", Body: "uptime"}, false); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(file)
	if err != nil {
		t.Fatal(err)
	}
	public, _ := json.Marshal(v.Summaries())
	for _, value := range []string{note.Manual, note.Discovered, note.Details} {
		if bytes.Contains(data, []byte(value)) || bytes.Contains(public, []byte(value)) {
			t.Fatal("notes leaked outside encrypted owner data")
		}
	}
	reopened, err := Read(file, testPass)
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	got, err := reopened.ServerNote(p.ID)
	if err != nil || got != note {
		t.Fatal("other writes lost annotations", got, err)
	}
	if err = reopened.DeleteProfile(file, p.ID); err != nil {
		t.Fatal(err)
	}
	if _, ok := reopened.notes[p.ID]; ok {
		t.Fatal("deleted connection left its annotation behind")
	}
	v.Close()
	if _, err = v.ServerNote(p.ID); err == nil {
		t.Fatal("locked owner could read notes")
	}
}

func TestServerNoteFailedSaveDoesNotPublishAndTargetTracksIdentity(t *testing.T) {
	v, _ := New(testPass)
	defer v.Close()
	p := profile()
	_ = v.Put(p)
	file := filepath.Join(t.TempDir(), "vault.json")
	if err := Save(file, v, true); err != nil {
		t.Fatal(err)
	}
	old := ServerNote{Manual: "keep this"}
	if err := v.SaveServerNote(file, p.ID, old); err != nil {
		t.Fatal(err)
	}
	occupied := filepath.Join(t.TempDir(), "file")
	_ = os.WriteFile(occupied, []byte("occupied"), 0600)
	if err := v.SaveServerNote(filepath.Join(occupied, "vault.json"), p.ID, ServerNote{Manual: "unsaved"}); err == nil {
		t.Fatal("expected write failure")
	}
	if got, _ := v.ServerNote(p.ID); got != old {
		t.Fatal("failed write published a new note")
	}
	if err := v.SaveServerNote(file, p.ID, ServerNote{Manual: strings.Repeat("x", 8193)}); err == nil {
		t.Fatal("oversized note accepted")
	}
	for _, details := range []string{strings.Repeat("x", 16385), "hidden\x1b", "invalid\xff"} {
		invalid := ServerNote{Discovered: "summary", Details: details, CheckedAt: time.Now().UTC(), Target: "fixture"}
		if err := v.SaveServerNote(file, p.ID, invalid); err == nil {
			t.Fatal("unbounded or unsafe evidence accepted")
		}
	}
	if err := v.SaveServerNote(file, p.ID, ServerNote{Details: "orphaned evidence"}); err == nil {
		t.Fatal("evidence without a bound summary was accepted")
	}
	if err := v.SaveServerNote(file, "unknown", old); err == nil {
		t.Fatal("note saved without a connection")
	}
	s := v.Summaries()[0]
	first := NoteTarget(s)
	s.Name = "rename"
	s.Group = "new group"
	if NoteTarget(s) != first {
		t.Fatal("cosmetic edit invalidated discovery")
	}
	s.Port++
	if NoteTarget(s) == first {
		t.Fatal("new destination reused old discovery binding")
	}
}
