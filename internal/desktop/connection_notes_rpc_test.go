package desktop

import (
	"reflect"
	"testing"
	"time"

	"sshclient/internal/vault"
)

func takeConnectionNotesLoadUpdate(t *testing.T, h *connectionFlowHarness) func() {
	t.Helper()
	select {
	case update, ok := <-h.updates:
		if !ok {
			t.Fatal("connection notes update channel closed")
		}
		return update
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for connection notes load update")
		return nil
	}
}

func seedConnectionNotesRPC(t *testing.T, h *connectionFlowHarness) (vault.Profile, vault.ServerNote) {
	t.Helper()
	p := h.fixture.Profile()
	owner := h.server.Vault
	cli := h.a.client
	if err := cli.SaveConnection(h.ctx, vault.ProfileChange{Profile: p}, owner); err != nil {
		t.Fatalf("seed connection: %v", err)
	}
	summaries := h.server.Vault.Summaries()
	summaryIndex := -1
	for i := range summaries {
		if summaries[i].ID == p.ID {
			summaryIndex = i
			break
		}
	}
	if summaryIndex < 0 {
		t.Fatalf("server summary for profile %q not found", p.ID)
	}
	note := vault.ServerNote{
		Manual:     "stored manual marker",
		Discovered: "stored overview",
		Details:    "stored details",
		CheckedAt:  time.Date(2025, time.January, 2, 3, 4, 5, 0, time.UTC),
		Target:     vault.NoteTarget(summaries[summaryIndex]),
	}
	if err := cli.SaveServerNote(h.ctx, p.ID, note, owner); err != nil {
		t.Fatalf("seed server note: %v", err)
	}
	return p, note
}

func TestConnectionNotesLoadImmediatelyAndPreservesFormEdits(t *testing.T) {
	h := newConnectionFlowHarness(t)
	t.Cleanup(h.a.resetConnectionEditor)

	p, note := seedConnectionNotesRPC(t, h)

	authsBefore := h.fixture.Auths.Load()
	busyBefore := h.a.busy
	phaseBefore := h.a.connection.phase
	h.a.connections = h.server.Vault.Summaries()
	h.a.selected = p.ID
	h.a.page = 1
	h.a.editConnection()
	h.ui.Frame()

	load := h.a.connection.notes
	if load == nil || !load.loading {
		t.Fatal("connection notes did not start loading")
	}
	if h.a.page != 0 {
		t.Fatalf("page = %d, want 0", h.a.page)
	}
	if h.a.form.ID != p.ID ||
		h.a.form.Name != p.Name ||
		h.a.form.Host != p.Host ||
		h.a.form.Port != p.Port ||
		h.a.form.User != p.User ||
		h.a.form.Auth != p.Auth ||
		h.a.form.Fingerprint != p.Fingerprint {
		t.Fatal("form public fields differ from seeded profile")
	}
	if h.a.form.Password != "" ||
		h.a.form.PrivateKey != "" ||
		h.a.form.KeyPassphrase != "" ||
		h.a.form.Network.Proxy.Password != "" ||
		h.a.form.Hardware.PIN != "" ||
		h.a.keyFile != "" {
		t.Fatal("form unexpectedly loaded secrets")
	}
	if !reflect.ValueOf(h.a.formNote).IsZero() {
		t.Fatalf("note = %#v, want empty before load", h.a.formNote)
	}
	if h.a.busy != busyBefore {
		t.Fatalf("busy = %v, want unchanged %v", h.a.busy, busyBefore)
	}
	if h.a.connection.phase != phaseBefore {
		t.Fatalf("connection phase = %q, want unchanged %q", h.a.connection.phase, phaseBefore)
	}
	if !h.ui.HasText("正在读取服务器备注…") {
		t.Fatal("loading text not shown")
	}

	update := takeConnectionNotesLoadUpdate(t, h)
	h.input("名称", "new draft name")
	h.a.saveProfile()
	if h.a.page != 0 {
		t.Fatalf("save while notes load left page %d, want 0", h.a.page)
	}
	if h.a.connection.notes != load || !load.loading {
		t.Fatal("save while notes load changed the pending loader")
	}
	if stored := h.stored(p.ID); stored.Name != p.Name {
		t.Fatalf("stored name = %q, want unchanged %q", stored.Name, p.Name)
	}
	if h.a.form.Name != "new draft name" {
		t.Fatalf("draft name = %q, want new draft name", h.a.form.Name)
	}
	if h.fixture.Auths.Load() != authsBefore {
		t.Fatal("saving while notes load performed a real SSH login")
	}

	update()
	h.ui.Frame()
	if load.loading {
		t.Fatal("connection notes still loading after update")
	}
	if load.loadError != "" {
		t.Fatalf("connection notes load error = %q", load.loadError)
	}
	if !reflect.DeepEqual(h.a.formNoteOriginal, note) {
		t.Fatalf("original note = %#v, want %#v", h.a.formNoteOriginal, note)
	}
	if !reflect.DeepEqual(h.a.formNote, note) {
		t.Fatalf("loaded note = %#v, want %#v", h.a.formNote, note)
	}
	if h.a.form.Name != "new draft name" {
		t.Fatalf("loaded notes replaced draft name with %q", h.a.form.Name)
	}
	if h.a.connectionNotesBlocked() {
		t.Fatal("connection notes remained blocked after successful load")
	}
	if h.fixture.Auths.Load() != authsBefore {
		t.Fatal("loading connection notes performed a real SSH login")
	}

	h.input("服务器备注", "post-load note edit")
	if h.a.formNote.Manual != "post-load note edit" {
		t.Fatalf("note draft = %q, want post-load note edit", h.a.formNote.Manual)
	}
	if h.a.formNote.Discovered != note.Discovered || h.a.formNote.Details != note.Details {
		t.Fatalf("note edit replaced server fields: %#v", h.a.formNote)
	}
	if !h.a.formNote.CheckedAt.Equal(note.CheckedAt) {
		t.Fatalf("checked time = %v, want %v", h.a.formNote.CheckedAt, note.CheckedAt)
	}
	if !reflect.DeepEqual(h.a.formNote.Target, note.Target) {
		t.Fatalf("note target = %#v, want %#v", h.a.formNote.Target, note.Target)
	}
	if !h.a.connection.notes.draftEdited {
		t.Fatal("note edit did not mark the draft as edited")
	}
}

func TestConnectionNotesLoadFailureRetryPreservesManualDraft(t *testing.T) {
	h := newConnectionFlowHarness(t)
	t.Cleanup(h.a.resetConnectionEditor)

	p, note := seedConnectionNotesRPC(t, h)

	authsBefore := h.fixture.Auths.Load()
	originalDir := h.a.client.Dir
	h.a.client.Dir = t.TempDir()
	t.Cleanup(func() { h.a.client.Dir = originalDir })
	h.a.connections = h.server.Vault.Summaries()
	h.a.selected = p.ID
	h.a.page = 1
	h.a.editConnection()
	h.ui.Frame()

	h.wait(func() bool {
		load := h.a.connection.notes
		return load != nil && !load.loading
	})
	load := h.a.connection.notes
	if load.loadError == "" {
		t.Fatal("connection notes load unexpectedly succeeded")
	}
	if !h.ui.HasText("读取服务器备注失败：") {
		t.Fatal("load failure text not shown")
	}

	h.input("名称", "new name after error")
	h.input("服务器备注", "manual entered after failure")
	if h.a.form.Name != "new name after error" {
		t.Fatalf("draft name = %q, want new name after error", h.a.form.Name)
	}
	if h.a.formNote.Manual != "manual entered after failure" {
		t.Fatalf("note draft = %q, want manual entered after failure", h.a.formNote.Manual)
	}
	if !h.a.connectionNotesBlocked() {
		t.Fatal("failed connection notes load did not block saving")
	}

	h.a.saveProfile()
	if h.a.page != 0 {
		t.Fatalf("save after load failure left page %d, want 0", h.a.page)
	}
	if stored := h.stored(p.ID); stored.Name != p.Name {
		t.Fatalf("stored name = %q, want unchanged %q", stored.Name, p.Name)
	}
	if storedNote, err := h.server.Vault.ServerNote(p.ID); err != nil || !reflect.DeepEqual(storedNote, note) {
		t.Fatalf("stored note = %#v, want unchanged %#v: %v", storedNote, note, err)
	}
	if h.a.formNote.Manual != "manual entered after failure" {
		t.Fatalf("blocked save changed note draft to %q", h.a.formNote.Manual)
	}
	if h.fixture.Auths.Load() != authsBefore {
		t.Fatal("failed note load or blocked save performed a real SSH login")
	}

	h.a.client.Dir = originalDir
	content, ok := h.ui.Find("连接分类内容")
	if !ok {
		t.Fatal("missing connection content viewport")
	}
	h.ui.Scroll(content.X+content.W/2, content.Y+content.H/2, 0, 1000)
	h.ui.Frame()
	retry, ok := h.ui.Find("重试读取")
	if !ok || retry.Y < content.Y || retry.Y+retry.H > content.Y+content.H {
		t.Fatal("notes retry is outside its scroll viewport", retry, content)
	}
	h.click("重试读取")
	h.wait(func() bool {
		load := h.a.connection.notes
		if load == nil {
			return false
		}
		if !load.loading && load.loadError != "" {
			t.Fatalf("connection notes retry failed: %s", load.loadError)
		}
		return !load.loading && load.loadError == ""
	})

	if load.loading || load.loadError != "" {
		t.Fatalf("retry state: loading=%v error=%q", load.loading, load.loadError)
	}
	if h.a.formNote.Manual != "manual entered after failure" {
		t.Fatalf("retry replaced manual draft with %q", h.a.formNote.Manual)
	}
	if h.a.formNoteOriginal.Manual != note.Manual {
		t.Fatalf("server manual note = %q, want old value %q", h.a.formNoteOriginal.Manual, note.Manual)
	}
	if !reflect.DeepEqual(h.a.formNoteOriginal, note) {
		t.Fatalf("original note = %#v, want %#v", h.a.formNoteOriginal, note)
	}
	if h.a.formNote.Discovered != note.Discovered || h.a.formNote.Details != note.Details {
		t.Fatalf("retried note fields = %#v, want server values from %#v", h.a.formNote, note)
	}
	if !h.a.formNote.CheckedAt.Equal(note.CheckedAt) {
		t.Fatalf("checked time = %v, want %v", h.a.formNote.CheckedAt, note.CheckedAt)
	}
	if !reflect.DeepEqual(h.a.formNote.Target, note.Target) {
		t.Fatalf("note target = %#v, want %#v", h.a.formNote.Target, note.Target)
	}
	if h.a.form.Name != "new name after error" {
		t.Fatalf("retry replaced draft name with %q", h.a.form.Name)
	}
	if h.a.connectionNotesBlocked() {
		t.Fatal("connection notes remained blocked after successful retry")
	}
	if h.fixture.Auths.Load() != authsBefore {
		t.Fatal("retrying connection notes performed a real SSH login")
	}
}

func TestConnectionNotesStaleLoadCannotMutateNewEditor(t *testing.T) {
	t.Run("close", func(t *testing.T) {
		h := newConnectionFlowHarness(t)
		t.Cleanup(h.a.resetConnectionEditor)
		p, _ := seedConnectionNotesRPC(t, h)
		h.a.connections = h.server.Vault.Summaries()
		h.a.selected = p.ID
		h.a.page = 1
		h.a.editConnection()

		oldNotes := h.a.connection.notes
		if oldNotes == nil || !oldNotes.loading {
			t.Fatal("connection notes did not start loading")
		}
		stale := takeConnectionNotesLoadUpdate(t, h)

		h.a.cancelConnectionEditor()
		if oldNotes.ctx.Err() == nil {
			t.Fatal("closing the editor did not cancel the stale notes load")
		}

		page := h.a.page
		online := h.a.online
		selected := h.a.selected
		form := h.a.form
		formNote := h.a.formNote
		formNoteOriginal := h.a.formNoteOriginal
		notes := h.a.connection.notes
		phase := h.a.connection.phase
		busy := h.a.busy

		stale()

		if h.a.page != page {
			t.Fatalf("stale load changed page from %d to %d", page, h.a.page)
		}
		if h.a.online != online {
			t.Fatalf("stale load changed online from %v to %v", online, h.a.online)
		}
		if h.a.selected != selected {
			t.Fatalf("stale load changed selection from %q to %q", selected, h.a.selected)
		}
		if !reflect.DeepEqual(h.a.form, form) {
			t.Fatalf("stale load changed form: %#v", h.a.form)
		}
		if !reflect.DeepEqual(h.a.formNote, formNote) {
			t.Fatalf("stale load changed note: %#v", h.a.formNote)
		}
		if !reflect.DeepEqual(h.a.formNoteOriginal, formNoteOriginal) {
			t.Fatalf("stale load changed original note: %#v", h.a.formNoteOriginal)
		}
		if h.a.connection.notes != notes {
			t.Fatal("stale load replaced the closed editor notes state")
		}
		if h.a.connection.phase != phase {
			t.Fatalf("stale load changed connection phase from %q to %q", phase, h.a.connection.phase)
		}
		if h.a.busy != busy {
			t.Fatalf("stale load changed busy from %v to %v", busy, h.a.busy)
		}
	})

	t.Run("lock", func(t *testing.T) {
		h := newConnectionFlowHarness(t)
		t.Cleanup(h.a.resetConnectionEditor)
		p, _ := seedConnectionNotesRPC(t, h)
		h.a.connections = h.server.Vault.Summaries()
		h.a.selected = p.ID
		h.a.page = 1
		h.a.editConnection()

		oldNotes := h.a.connection.notes
		if oldNotes == nil || !oldNotes.loading {
			t.Fatal("connection notes did not start loading")
		}
		stale := takeConnectionNotesLoadUpdate(t, h)

		h.a.lock()
		if oldNotes.ctx.Err() == nil {
			t.Fatal("locking did not cancel the stale notes load")
		}

		page := h.a.page
		online := h.a.online
		selected := h.a.selected
		form := h.a.form
		formNote := h.a.formNote
		formNoteOriginal := h.a.formNoteOriginal
		notes := h.a.connection.notes
		phase := h.a.connection.phase
		busy := h.a.busy

		stale()

		if h.a.page != page {
			t.Fatalf("stale load changed page from %d to %d", page, h.a.page)
		}
		if h.a.online != online {
			t.Fatalf("stale load changed online from %v to %v", online, h.a.online)
		}
		if h.a.selected != selected {
			t.Fatalf("stale load changed selection from %q to %q", selected, h.a.selected)
		}
		if !reflect.DeepEqual(h.a.form, form) {
			t.Fatalf("stale load changed form: %#v", h.a.form)
		}
		if !reflect.DeepEqual(h.a.formNote, formNote) {
			t.Fatalf("stale load changed note: %#v", h.a.formNote)
		}
		if !reflect.DeepEqual(h.a.formNoteOriginal, formNoteOriginal) {
			t.Fatalf("stale load changed original note: %#v", h.a.formNoteOriginal)
		}
		if h.a.connection.notes != notes {
			t.Fatal("stale load replaced the locked editor notes state")
		}
		if h.a.connection.phase != phase {
			t.Fatalf("stale load changed connection phase from %q to %q", phase, h.a.connection.phase)
		}
		if h.a.busy != busy {
			t.Fatalf("stale load changed busy from %v to %v", busy, h.a.busy)
		}
	})

	t.Run("reopen", func(t *testing.T) {
		h := newConnectionFlowHarness(t)
		t.Cleanup(h.a.resetConnectionEditor)
		p, note := seedConnectionNotesRPC(t, h)
		h.a.connections = h.server.Vault.Summaries()
		h.a.selected = p.ID
		h.a.page = 1
		h.a.editConnection()

		oldNotes := h.a.connection.notes
		if oldNotes == nil || !oldNotes.loading {
			t.Fatal("connection notes did not start loading")
		}
		stale := takeConnectionNotesLoadUpdate(t, h)

		h.a.cancelConnectionEditor()
		if oldNotes.ctx.Err() == nil {
			t.Fatal("closing the editor did not cancel the stale notes load")
		}
		h.a.selected = p.ID
		h.a.page = 1
		h.a.editConnection()

		newNotes := h.a.connection.notes
		if newNotes == nil || newNotes == oldNotes || !newNotes.loading {
			t.Fatal("reopening did not start a new connection notes load")
		}
		newSequence := newNotes.loadSequence
		if !reflect.ValueOf(h.a.formNote).IsZero() {
			t.Fatalf("reopened note = %#v, want empty before load", h.a.formNote)
		}

		stale()

		if h.a.connection.notes != newNotes || !newNotes.loading {
			t.Fatal("stale load changed the reopened notes state")
		}
		if newNotes.loadSequence != newSequence {
			t.Fatalf("stale load changed reopened sequence from %d to %d", newSequence, newNotes.loadSequence)
		}
		if newNotes.ctx.Err() != nil {
			t.Fatalf("stale load canceled the reopened context: %v", newNotes.ctx.Err())
		}
		if !reflect.ValueOf(h.a.formNote).IsZero() {
			t.Fatalf("stale load filled reopened note before its own load: %#v", h.a.formNote)
		}

		fresh := takeConnectionNotesLoadUpdate(t, h)
		fresh()
		h.ui.Frame()

		if h.a.connection.notes != newNotes || newNotes.loading || newNotes.loadError != "" {
			t.Fatalf("reopened load state: loading=%v error=%q", newNotes.loading, newNotes.loadError)
		}
		if !reflect.DeepEqual(h.a.formNoteOriginal, note) {
			t.Fatalf("reopened original note = %#v, want %#v", h.a.formNoteOriginal, note)
		}
		if !reflect.DeepEqual(h.a.formNote, note) {
			t.Fatalf("reopened note = %#v, want %#v", h.a.formNote, note)
		}
		if h.a.connectionNotesBlocked() {
			t.Fatal("reopened connection notes remained blocked after successful load")
		}
	})
}

func TestConnectionNotesLoadSurvivesUnrelatedEditorOperation(t *testing.T) {
	h := newConnectionFlowHarness(t)
	t.Cleanup(h.a.resetConnectionEditor)
	p, note := seedConnectionNotesRPC(t, h)
	h.a.connections = h.server.Vault.Summaries()
	h.a.selected = p.ID
	h.a.page = 1
	h.a.editConnection()

	load := h.a.connection.notes
	if load == nil || !load.loading {
		t.Fatal("connection notes did not start loading")
	}
	loadUpdate := takeConnectionNotesLoadUpdate(t, h)
	sequence := h.a.connection.sequence

	ctx, _, _ := h.a.editorOperation("agent")
	if h.a.connection.sequence == sequence {
		t.Fatal("unrelated editor operation did not change the operation sequence")
	}
	if load.ctx.Err() != nil {
		t.Fatalf("unrelated editor operation canceled the notes context: %v", load.ctx.Err())
	}
	if ctx.Err() != nil {
		t.Fatalf("unrelated editor operation context was already canceled: %v", ctx.Err())
	}

	loadUpdate()
	h.ui.Frame()

	if h.a.connection.notes != load {
		t.Fatal("notes load was replaced by an unrelated editor operation")
	}
	if load.ctx.Err() != nil {
		t.Fatalf("notes context was canceled by an unrelated editor operation: %v", load.ctx.Err())
	}
	if load.loading || load.loadError != "" {
		t.Fatalf("notes load state: loading=%v error=%q", load.loading, load.loadError)
	}
	if !reflect.DeepEqual(h.a.formNoteOriginal, note) {
		t.Fatalf("original note = %#v, want %#v", h.a.formNoteOriginal, note)
	}
	if !reflect.DeepEqual(h.a.formNote, note) {
		t.Fatalf("loaded note = %#v, want %#v", h.a.formNote, note)
	}
	if h.a.connectionNotesBlocked() {
		t.Fatal("connection notes remained blocked after successful load")
	}

	h.a.cancelConnectionEditor()
	if ctx.Err() == nil {
		t.Fatal("canceling the simulated editor operation did not cancel its context")
	}
	h.a.connection.cancel = nil
	h.a.connection.phase = ""
	h.ui.Frame()
}
