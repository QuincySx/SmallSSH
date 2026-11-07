package desktop

import (
	"reflect"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"
	"sshclient/internal/broker"
	"sshclient/internal/snippet"
)

func queuedSnippetUpdate(t *testing.T, h *connectionFlowHarness) func() {
	t.Helper()
	select {
	case update := <-h.updates:
		return update
	case <-time.After(5 * time.Second):
		t.Fatal("snippet RPC did not finish")
		return nil
	}
}

func snippetWithID(commands []snippet.Command, id string) (snippet.Command, bool) {
	for _, command := range commands {
		if command.ID == id {
			return command, true
		}
	}
	return snippet.Command{}, false
}

func TestSnippetLoadReturnsStoredContentWithoutBusy(t *testing.T) {
	h := newConnectionFlowHarness(t)
	command := snippet.Command{ID: "load-marker-id", Name: "load-marker-name", Group: "acceptance", Body: "printf load-marker-body"}
	if err := h.server.Vault.SaveSnippet(broker.VaultPath(h.dir), command, false); err != nil {
		t.Fatal(err)
	}
	h.resetApp()
	if h.a.busy {
		t.Fatal("a fresh workspace is unexpectedly busy")
	}
	h.a.loadSnippets()
	if h.a.busy {
		t.Fatal("loading snippets marked the workspace busy")
	}
	update := queuedSnippetUpdate(t, h)
	if h.a.busy {
		t.Fatal("a queued snippet load marked the workspace busy")
	}
	update()
	if h.a.busy {
		t.Fatal("applying a snippet load marked the workspace busy")
	}
	got, ok := snippetWithID(h.a.snippets, command.ID)
	if !ok || got.Name != command.Name || got.Body != command.Body {
		t.Fatalf("snippet load did not return the broker content: %#v", h.a.snippets)
	}
}

func TestSnippetSaveAndDeleteRefreshRealBroker(t *testing.T) {
	h := newConnectionFlowHarness(t)
	a := h.a
	a.newSnippet()
	saved := a.snippetForm
	saved.Name, saved.Group, saved.Body = "broker-save-name", "acceptance", "printf broker-save-body"
	a.snippetForm = saved
	a.snippetForm.Args = []snippet.Arg{}
	a.saveSnippetForm()
	queuedSnippetUpdate(t, h)()
	if a.busy {
		t.Fatal("snippet save left the workspace busy")
	}
	if a.snippetOpen || !reflect.DeepEqual(a.snippetForm, snippet.Command{}) || a.snippetError != "" {
		t.Fatalf("snippet save did not close and clear the unchanged editor: open=%v form=%#v error=%q", a.snippetOpen, a.snippetForm, a.snippetError)
	}
	got, ok := snippetWithID(a.snippets, saved.ID)
	if !ok || got.Name != saved.Name || got.Body != saved.Body {
		t.Fatalf("snippet save did not refresh the list from the broker: %#v", a.snippets)
	}
	stored, ok := snippetWithID(h.server.Vault.Snippets(), saved.ID)
	if !ok || stored.Body != saved.Body {
		t.Fatalf("snippet save did not reach the broker vault: %#v", h.server.Vault.Snippets())
	}

	a.newSnippet()
	draft := a.snippetForm
	draft.Name, draft.Body = "draft survives background saves", "echo draft"
	a.snippetForm = draft

	toggled := saved
	toggled.SkipConfirm = true
	a.persistSnippet(toggled, false, nil)
	queuedSnippetUpdate(t, h)()
	if !a.snippetOpen || !reflect.DeepEqual(a.snippetForm, draft) {
		t.Fatal("confirmation toggle cleared an unrelated draft", a.snippetOpen, a.snippetForm)
	}
	if got, ok = snippetWithID(a.snippets, saved.ID); !ok || !got.SkipConfirm {
		t.Fatal("confirmation toggle did not refresh the list", a.snippets)
	}
	if stored, ok = snippetWithID(h.server.Vault.Snippets(), saved.ID); !ok || !stored.SkipConfirm {
		t.Fatal("confirmation toggle did not reach the broker vault", h.server.Vault.Snippets())
	}

	a.persistSnippet(saved, true, nil)
	queuedSnippetUpdate(t, h)()
	if !a.snippetOpen || !reflect.DeepEqual(a.snippetForm, draft) {
		t.Fatal("delete cleared an unrelated draft", a.snippetOpen, a.snippetForm)
	}
	if _, ok = snippetWithID(a.snippets, saved.ID); ok {
		t.Fatal("delete did not refresh the list", a.snippets)
	}
	if _, ok = snippetWithID(h.server.Vault.Snippets(), saved.ID); ok {
		t.Fatal("delete did not reach the broker vault", h.server.Vault.Snippets())
	}
}

func TestSnippetEditorSavesConfirmationBothDirections(t *testing.T) {
	h := newConnectionFlowHarness(t)
	a := h.a
	a.newSnippet()
	a.snippetForm.Name, a.snippetForm.Body = "confirmation editor", "uptime"
	id := a.snippetForm.ID
	for _, wantSkip := range []bool{false, true, false} {
		tt := ui.NewTester(a.snippetDialog, 700, 720)
		if a.snippetForm.SkipConfirm != wantSkip {
			if err := tt.Click("发送时询问"); err != nil {
				t.Fatal(err)
			}
		}
		if err := tt.Click("保存命令"); err != nil {
			t.Fatal(err)
		}
		queuedSnippetUpdate(t, h)()
		stored, ok := snippetWithID(h.server.Vault.Snippets(), id)
		if !ok || stored.SkipConfirm != wantSkip {
			t.Fatal("editor confirmation did not reach the broker", stored, wantSkip)
		}
		listed, ok := snippetWithID(a.snippets, id)
		if !ok || listed.SkipConfirm != wantSkip || a.snippetOpen {
			t.Fatal("editor save did not refresh and close", listed, wantSkip, a.snippetOpen)
		}
		a.editSnippet(listed)
	}
}

func TestQueuedSnippetSaveCompletionPreservesNewerDraft(t *testing.T) {
	cases := []struct {
		name   string
		change func(*app, snippet.Command) snippet.Command
	}{
		{
			name: "new draft",
			change: func(a *app, _ snippet.Command) snippet.Command {
				a.newSnippet()
				draft := a.snippetForm
				draft.Name, draft.Body = "draft B", "echo draft B"
				a.snippetForm = draft
				return a.snippetForm.Clone()
			},
		},
		{
			name: "reopen same id",
			change: func(a *app, saved snippet.Command) snippet.Command {
				a.editSnippet(saved)
				draft := a.snippetForm
				draft.Name, draft.Body = "reopened B", "echo reopened B"
				a.snippetForm = draft
				return a.snippetForm.Clone()
			},
		},
		{
			name: "continue editing the same form",
			change: func(a *app, _ snippet.Command) snippet.Command {
				a.snippetForm.Name = "edited A"
				a.snippetForm.Body = "echo edited A"
				return a.snippetForm.Clone()
			},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			h := newConnectionFlowHarness(t)
			a := h.a
			a.newSnippet()
			saved := a.snippetForm
			saved.Name, saved.Body = "saved A", "printf saved A"
			a.snippetForm = saved
			a.saveSnippetForm()
			update := queuedSnippetUpdate(t, h)
			if !a.busy || !a.snippetOpen || len(a.snippets) != 0 {
				t.Fatal("save completion was applied before the newer draft was entered", a.busy, a.snippetOpen, a.snippets)
			}
			expected := tc.change(a, saved)
			update()
			if a.busy {
				t.Fatal("save completion left the workspace busy")
			}
			if !a.snippetOpen {
				t.Fatal("a queued save completion closed the newer draft")
			}
			if !reflect.DeepEqual(a.snippetForm, expected) {
				t.Fatalf("a queued save completion changed the newer draft: %#v", a.snippetForm)
			}
			if got, ok := snippetWithID(a.snippets, saved.ID); !ok || got.Body != saved.Body {
				t.Fatalf("the saved command is missing from the refreshed list: %#v", a.snippets)
			}
			if stored, ok := snippetWithID(h.server.Vault.Snippets(), saved.ID); !ok || stored.Body != saved.Body {
				t.Fatalf("the saved command is missing from the broker vault: %#v", h.server.Vault.Snippets())
			}
		})
	}
}

func TestQueuedSnippetListCannotRepopulateALockedWorkspace(t *testing.T) {
	h := newConnectionFlowHarness(t)
	command := snippet.Command{ID: "stale-list", Name: "stale list", Body: "uptime"}
	if err := h.server.Vault.SaveSnippet(broker.VaultPath(h.dir), command, false); err != nil {
		t.Fatal(err)
	}
	h.resetApp()
	h.a.loadSnippets()

	var stale func()
	select {
	case stale = <-h.updates:
	case <-time.After(5 * time.Second):
		t.Fatal("snippet list did not finish")
	}

	h.a.lock()
	lockedStatus := h.a.status
	stale()
	if len(h.a.snippets) != 0 || h.a.status != lockedStatus {
		t.Fatal("a queued list result changed the locked workspace", h.a.snippets, h.a.status)
	}
}
