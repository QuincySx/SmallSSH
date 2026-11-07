package desktop

import (
	"context"
	"fmt"
	"net"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"
	"sshclient/internal/servernotes"
	"sshclient/internal/testserver"
	"sshclient/internal/vault"
)

func newServerNotesConnection(t *testing.T, execute func(string) (string, string, int)) *connectionFlowHarness {
	t.Helper()
	h := newConnectionFlowHarness(t)
	if execute != nil {
		h.fixture = testserver.NewWithExec(t, execute)
	}
	if err := h.a.client.SaveConnection(h.ctx, vault.ProfileChange{Profile: h.fixture.Profile()}, h.server.Vault); err != nil {
		t.Fatal(err)
	}
	h.a.page = 1
	h.a.connections = h.server.Vault.Summaries()
	h.a.selected = h.fixture.Profile().ID
	t.Cleanup(func() {
		if h.a.serverNotes != nil {
			h.a.closeServerNotes()
		}
	})
	return h
}

func waitServerNotesLoaded(t *testing.T, h *connectionFlowHarness, s *serverNotesState) {
	t.Helper()
	h.wait(func() bool {
		return h.a.notesCurrent(s) && !s.loading && s.loadError == ""
	})
}

func openServerNotesAndWait(t *testing.T, h *connectionFlowHarness) *serverNotesState {
	t.Helper()
	h.a.openServerNotes(h.a.selected)
	s := h.a.serverNotes
	if s == nil {
		t.Fatal("note dialog did not open", h.a.status)
	}
	h.ui = ui.NewTester(h.a.serverNotesDialog, 760, 650)
	waitServerNotesLoaded(t, h, s)
	return s
}

func prepareServerNotes(t *testing.T, execute func(string) (string, string, int)) *connectionFlowHarness {
	t.Helper()
	h := newServerNotesConnection(t, execute)
	openServerNotesAndWait(t, h)
	return h
}

func nextServerNotesUpdate(t *testing.T, h *connectionFlowHarness) func() {
	t.Helper()
	select {
	case update := <-h.updates:
		return update
	case <-time.After(5 * time.Second):
		t.Fatal("server notes RPC did not finish")
		return nil
	}
}

func seedServerNote(t *testing.T, h *connectionFlowHarness, manual string) {
	t.Helper()
	owner, err := h.a.unlockedOwner()
	if err != nil {
		t.Fatal(err)
	}
	if err := h.a.client.SaveServerNote(h.ctx, h.a.selected, vault.ServerNote{Manual: manual}, owner); err != nil {
		t.Fatal(err)
	}
}

func TestServerNotesSaveWithoutConnectingAndDiscardRequiresChoice(t *testing.T) {
	h := prepareServerNotes(t, nil)
	before := h.fixture.Auths.Load()
	h.click("服务器备注输入")
	h.ui.Type("用途：静态网站、Sub-Store。\n维护：留意证书续期。")
	h.click("保存备注")
	h.wait(func() bool { return h.a.serverNotes == nil })
	note, err := h.server.Vault.ServerNote(h.a.selected)
	if err != nil || !strings.Contains(note.Manual, "Sub-Store") {
		t.Fatal("manual note did not persist", note, err)
	}
	if h.fixture.Auths.Load() != before {
		t.Fatal("manual notes unexpectedly connected to the server")
	}
	s := openServerNotesAndWait(t, h)
	if s.draft.Manual != note.Manual {
		t.Fatal("saved notes not restored")
	}
	s.draft.Manual = "unsaved edit"
	h.ui.Frame()
	h.a.requestCloseNotes(s)
	h.ui.Frame()
	if !s.discard {
		t.Fatal("closing discarded an edit without a choice")
	}
	h.click("继续编辑")
	if s.draft.Manual != "unsaved edit" || s.discard {
		t.Fatal("continuing lost the draft")
	}
	h.a.requestCloseNotes(s)
	h.ui.Frame()
	h.click("放弃更改")
	if h.a.serverNotes != nil || s.ctx.Err() == nil || s.draft.Manual != "" {
		t.Fatal("closing retained note contents or work context")
	}
	s = openServerNotesAndWait(t, h)
	h.a.lock()
	if h.a.serverNotes != nil || s.ctx.Err() == nil || s.draft.Manual != "" {
		t.Fatal("locking retained private notes")
	}
}

func TestServerNotesReadableSummaryAndLegacyRenderingDoNotConnectOrDirtyDraft(t *testing.T) {
	h := prepareServerNotes(t, nil)
	s := h.a.serverNotes
	legacy := "系统：Debian\n\n运行中的服务（systemd）：\nsubstore.service、frps.service、dbus.service\n\n监听地址（不代表公网已放行）：TCP *:9080"
	s.draft = vault.ServerNote{Manual: "keep my words", Discovered: legacy, CheckedAt: time.Now().UTC(), Target: vault.NoteTarget(s.target)}
	s.original = s.draft
	before := h.fixture.Auths.Load()
	h.ui.Frame()
	summary, details := s.snapshotText()
	if !strings.Contains(summary, "服务：Sub-Store、FRP 服务端") || !strings.Contains(summary, "\n域名：") || strings.Contains(summary, "\\n") || !strings.Contains(details, "\n\n") {
		t.Fatal("summary was flattened or still raw inventory", summary, details)
	}
	if _, visible := h.ui.Find("服务器检测详情"); visible {
		t.Fatal("technical evidence was expanded by default")
	}
	if h.fixture.Auths.Load() != before || s.draft != s.original || s.draft.Details != "" {
		t.Fatal("reading an existing note probed the server or changed its stored draft")
	}
	if got := notesDisplayText("first\nsecond\x1b\u202e"); got != "first\nsecond\\x1b\\u202e" {
		t.Fatal("multiline display lost control-character protection", got)
	}
}

func TestServerNotesScanThroughLoopbackSSHAndKeepsManualDraft(t *testing.T) {
	var commands atomic.Int32
	h := prepareServerNotes(t, func(command string) (string, string, int) {
		if command != servernotes.Command {
			return "", "unexpected command", 1
		}
		commands.Add(1)
		return "@@OS\nDebian GNU/Linux 13\n@@SERVICE_MANAGER\nsystemd\n@@SERVICE_FILES\nnginx.service enabled\n@@SERVICE_UNITS\nnginx.service loaded active running Web\n@@INTERFACES\neth0 up 0 - 1500\n@@ADDRESSES\n2: eth0 inet 192.0.2.10/24 scope global eth0\n@@DOCKER_STATUS\nready\n@@DOCKER_CONTAINERS\n{\"ID\":\"fixture\",\"Names\":\"sub-store\",\"Image\":\"example/sub-store:latest\",\"State\":\"running\"}\n@@NOTE_PORT\ttcp\t0.0.0.0:443\n@@NOTE_DOMAIN\t/etc/nginx/conf.d/site.conf\tweb.example.test\n@@NOTE_DONE\n", "", 0
	})
	h.a.notesDNS = func(_ context.Context, host string) ([]net.IPAddr, error) {
		if host != "web.example.test" {
			t.Errorf("unexpected DNS query %q", host)
		}
		return []net.IPAddr{{IP: net.ParseIP("192.0.2.10")}}, nil
	}
	s := h.a.serverNotes
	s.draft.Manual = "my handwritten reminder"
	h.ui.Frame()
	h.click("检测服务和域名")
	h.wait(func() bool { return !s.scanning })
	if s.error != "" || commands.Load() != 1 {
		t.Fatal("scan failed or repeated", s.error, commands.Load())
	}
	if s.draft.Manual != "my handwritten reminder" {
		t.Fatal("discovery overwrote a handwritten note")
	}
	for _, text := range []string{"nginx.service", "sub-store", "web.example.test", "匹配已知地址", "/etc/nginx/conf.d/site.conf", "TCP 0.0.0.0:443"} {
		if !strings.Contains(s.draft.Details, text) {
			t.Fatal("missing discovered metadata", text, s.draft.Details)
		}
	}
	if !strings.Contains(s.draft.Discovered, "服务：Sub-Store、Nginx") || s.expanded || strings.Contains(s.draft.Discovered, "TCP 0.0.0.0") {
		t.Fatal("scan did not show concise purpose with collapsed technical details", s.draft)
	}
	if stored, _ := h.server.Vault.ServerNote(s.target.ID); stored.Manual != "" || stored.Discovered != "" {
		t.Fatal("scan silently saved an unfinished manual draft")
	}
	h.click("保存备注")
	h.wait(func() bool { return h.a.serverNotes == nil })
	stored, _ := h.server.Vault.ServerNote(s.target.ID)
	if stored.Manual != "my handwritten reminder" || stored.Discovered == "" || stored.Details == "" || stored.Target != vault.NoteTarget(s.target) || stored.CheckedAt.IsZero() {
		t.Fatal("note and scan did not persist together", stored)
	}
}

func TestServerNotesIncompleteScanPreservesPriorSnapshot(t *testing.T) {
	h := prepareServerNotes(t, func(string) (string, string, int) { return "@@NOTE_DOMAIN\t/source\tpartial.example.test\n", "", 0 })
	s := h.a.serverNotes
	s.draft = vault.ServerNote{Manual: "manual", Discovered: "prior working snapshot", CheckedAt: time.Now().UTC(), Target: vault.NoteTarget(s.target)}
	old := s.draft
	h.a.scanServerNotes(s)
	h.wait(func() bool { return !s.scanning })
	if s.error == "" || s.draft != old {
		t.Fatal("partial scan replaced useful notes", s.error, s.draft)
	}
}

func TestServerNotesCloseShortcutAndNavigationPreserveUnsavedDraft(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	s := &serverNotesState{ctx: ctx, cancel: cancel, draft: vault.ServerNote{Manual: "unsaved"}}
	a := &app{online: true, page: 1, serverNotes: s}
	ran := false
	a.runWindowMenuAction(menuAction{shortcut: "Cmd+W", run: func() { ran = true }}, false, 0)
	if ran || !s.discard || s.draft.Manual != "unsaved" {
		t.Fatal("close shortcut closed another workspace item")
	}
	s.discard = false
	a.navigate(func() { ran = true })
	if ran || !s.discard {
		t.Fatal("navigation silently discarded a note")
	}
	s.discard = false
	s.draft = s.original
	a.navigate(func() { ran = true })
	if !ran || a.serverNotes != nil || ctx.Err() == nil {
		t.Fatal("clean note prevented normal navigation")
	}
}

func TestServerNotesLateScanCannotOverwriteNewerScan(t *testing.T) {
	var round atomic.Int32
	h := prepareServerNotes(t, func(string) (string, string, int) {
		return fmt.Sprintf("@@OS\nround %d\n@@NOTE_DONE\n", round.Add(1)), "", 0
	})
	s := h.a.serverNotes
	s.draft.Manual = "keep this draft"
	getUpdate := func() func() {
		t.Helper()
		select {
		case fn := <-h.updates:
			return fn
		case <-time.After(5 * time.Second):
			t.Fatal("scan did not finish")
			return nil
		}
	}
	h.a.scanServerNotes(s)
	oldUpdate := getUpdate()
	h.a.requestCloseNotes(s)
	if !s.discard || !h.a.notesCurrent(s) {
		t.Fatal("cancelled scan lost the editable note")
	}
	s.discard = false
	// Advance only this test's cooldown; the delayed callback still belongs
	// to the cancelled scan and must not affect this newer one.
	h.a.notesScannedAt[vault.NoteTarget(s.target)] = time.Now().Add(-notesScanInterval)
	h.a.scanServerNotes(s)
	newUpdate := getUpdate()
	oldUpdate()
	if !s.scanning || s.draft.Discovered != "" || s.draft.Details != "" {
		t.Fatal("late cancelled result affected a newer scan")
	}
	newUpdate()
	if s.scanning || !strings.Contains(s.draft.Details, "round 2") || s.draft.Manual != "keep this draft" {
		t.Fatal("newer scan result was lost", s.error, s.draft)
	}
}

func TestServerNotesCooldownSurvivesReopeningAndRepeatedClicks(t *testing.T) {
	var commands atomic.Int32
	h := prepareServerNotes(t, func(string) (string, string, int) {
		commands.Add(1)
		return "@@NOTE_DONE\n", "", 0
	})
	s := h.a.serverNotes
	h.a.scanServerNotes(s)
	for range 20 {
		h.a.scanServerNotes(s)
	}
	h.wait(func() bool { return !s.scanning })
	if s.error != "" || commands.Load() != 1 {
		t.Fatal("repeat clicks multiplied the scan", s.error, commands.Load())
	}
	h.a.closeServerNotes()
	s = openServerNotesAndWait(t, h)
	h.a.scanServerNotes(s)
	if s.scanning || h.a.notesScanWait(s.target) <= 0 || commands.Load() != 1 {
		t.Fatal("reopening bypassed scan cooldown", commands.Load())
	}
	h.a.notesScannedAt[vault.NoteTarget(s.target)] = time.Now().Add(-notesScanInterval)
	h.a.scanServerNotes(s)
	h.wait(func() bool { return !s.scanning })
	if commands.Load() != 2 || s.error != "" {
		t.Fatal("expired cooldown did not permit a new explicit scan", commands.Load(), s.error)
	}
}

func TestServerNotesCancelIsQuietAndKeepsPreviousSnapshot(t *testing.T) {
	h := prepareServerNotes(t, func(string) (string, string, int) {
		return "@@NOTE_DOMAIN\t/fixture\twait.example.test\n@@NOTE_DONE\n", "", 0
	})
	started, stopped := make(chan struct{}), make(chan struct{})
	h.a.notesDNS = func(ctx context.Context, _ string) ([]net.IPAddr, error) {
		close(started)
		<-ctx.Done()
		close(stopped)
		return nil, ctx.Err()
	}
	s := h.a.serverNotes
	s.draft = vault.ServerNote{Manual: "keep", Discovered: "previous snapshot", CheckedAt: time.Now().UTC(), Target: vault.NoteTarget(s.target)}
	before := s.draft
	h.a.scanServerNotes(s)
	select {
	case <-started:
	case <-time.After(5 * time.Second):
		t.Fatal("scan did not reach DNS verification")
	}
	h.ui.Frame()
	h.click("取消检测")
	select {
	case <-stopped:
	case <-time.After(time.Second):
		t.Fatal("cancellation did not stop DNS verification")
	}
	if s.scanning || s.error != "" || s.draft != before {
		t.Fatal("normal cancellation showed an error or replaced notes", s.error, s.draft)
	}
}

func TestServerNotesLoadShowsLoadingWithoutStartingDiscovery(t *testing.T) {
	const marker = "marker: metadata load only"
	h := newServerNotesConnection(t, nil)
	seedServerNote(t, h, marker)
	beforeBusy := h.a.busy
	beforeAuths := h.fixture.Auths.Load()

	h.a.openServerNotes(h.a.selected)
	s := h.a.serverNotes
	if s == nil || !s.loading {
		t.Fatal("opening notes did not enter the loading state", h.a.status, s)
	}
	if s.original.Manual != "" || s.draft.Manual != "" {
		t.Fatal("notes were populated before the real load closure was applied", s.original, s.draft)
	}
	if h.a.busy != beforeBusy {
		t.Fatal("opening notes changed global busy state")
	}
	h.ui = ui.NewTester(h.a.serverNotesDialog, 760, 650)
	h.ui.Frame()
	h.a.saveServerNotes(s)
	h.a.scanServerNotes(s)
	if s.saving || s.scanning || h.a.notesScanWait(s.target) > 0 {
		t.Fatal("loading notes accepted save or discovery actions", s.saving, s.scanning)
	}

	waitServerNotesLoaded(t, h, s)
	if s.original.Manual != marker || s.draft != s.original {
		t.Fatal("real metadata load did not apply the broker note exactly", s.original, s.draft)
	}
	if h.a.busy != beforeBusy {
		t.Fatal("loaded notes changed global busy state")
	}
	if h.fixture.Auths.Load() != beforeAuths {
		t.Fatal("loading note metadata started an SSH discovery scan")
	}
}

func TestServerNotesStaleLoadCannotRestoreClosedOrReopenedWindow(t *testing.T) {
	const marker = "marker: stale load must not revive"

	t.Run("close", func(t *testing.T) {
		h := newServerNotesConnection(t, nil)
		seedServerNote(t, h, marker)
		h.a.openServerNotes(h.a.selected)
		s := h.a.serverNotes
		if s == nil {
			t.Fatal("note dialog did not open")
		}
		h.ui = ui.NewTester(h.a.serverNotesDialog, 760, 650)
		oldUpdate := nextServerNotesUpdate(t, h)
		oldCtx := s.ctx
		h.a.closeServerNotes()
		oldUpdate()
		if h.a.serverNotes != nil || oldCtx.Err() == nil || s.original.Manual != "" || s.draft.Manual != "" {
			t.Fatal("stale load restored a closed note window", h.a.serverNotes, s.original, s.draft)
		}
	})

	t.Run("lock", func(t *testing.T) {
		h := newServerNotesConnection(t, nil)
		seedServerNote(t, h, marker)
		h.a.openServerNotes(h.a.selected)
		s := h.a.serverNotes
		if s == nil {
			t.Fatal("note dialog did not open")
		}
		h.ui = ui.NewTester(h.a.serverNotesDialog, 760, 650)
		oldUpdate := nextServerNotesUpdate(t, h)
		oldCtx := s.ctx
		h.a.lock()
		oldUpdate()
		if h.a.serverNotes != nil || oldCtx.Err() == nil || s.original.Manual != "" || s.draft.Manual != "" {
			t.Fatal("stale load restored notes after locking", h.a.serverNotes, s.original, s.draft)
		}
	})

	t.Run("close and reopen", func(t *testing.T) {
		h := newServerNotesConnection(t, nil)
		seedServerNote(t, h, marker)
		h.a.openServerNotes(h.a.selected)
		oldState := h.a.serverNotes
		if oldState == nil {
			t.Fatal("note dialog did not open")
		}
		h.ui = ui.NewTester(h.a.serverNotesDialog, 760, 650)
		oldUpdate := nextServerNotesUpdate(t, h)
		oldCtx := oldState.ctx
		h.a.closeServerNotes()
		h.a.openServerNotes(h.a.selected)
		s := h.a.serverNotes
		if s == nil || s == oldState || !s.loading {
			t.Fatal("reopened notes did not create a fresh loading state", s)
		}
		h.ui = ui.NewTester(h.a.serverNotesDialog, 760, 650)
		oldUpdate()
		h.ui.Frame()
		if h.a.serverNotes != s || !s.loading || s.original.Manual != "" || s.draft.Manual != "" {
			t.Fatal("stale load changed the reopened window", h.a.serverNotes, s.original, s.draft)
		}
		if oldCtx.Err() == nil {
			t.Fatal("closing the old note window did not cancel its context")
		}

		waitServerNotesLoaded(t, h, s)
		if s.original.Manual != marker || s.draft != s.original {
			t.Fatal("fresh load did not load the broker marker", s.original, s.draft)
		}
	})
}

func TestServerNotesFailedSaveKeepsDialogThenSucceedsAfterRecovery(t *testing.T) {
	const marker = "marker: original broker note"
	h := newServerNotesConnection(t, nil)
	seedServerNote(t, h, marker)
	s := openServerNotesAndWait(t, h)
	if s.original.Manual != marker || s.draft != s.original {
		t.Fatal("broker marker did not load", s.original, s.draft)
	}

	s.draft.Manual = "edited note kept after failure"
	edited := s.draft
	goodDir := h.a.client.Dir
	h.a.client.Dir = t.TempDir()
	h.a.saveServerNotes(s)
	if !s.saving {
		t.Fatal("save did not enter the saving state")
	}
	h.wait(func() bool { return !s.saving })
	if h.a.serverNotes != s || s.saving || s.error == "" || s.draft != edited || s.original.Manual != marker {
		t.Fatal("failed save did not preserve the open dialog and draft", h.a.serverNotes, s.error, s.original, s.draft)
	}
	if _, visible := h.ui.Find(reviewText(s.error)); !visible {
		t.Fatal("save failure was not visible", s.error)
	}
	stored, err := h.server.Vault.ServerNote(s.target.ID)
	if err != nil || stored.Manual != marker {
		t.Fatal("failed save changed broker storage", stored, err)
	}

	h.a.client.Dir = goodDir
	h.a.saveServerNotes(s)
	if !s.saving {
		t.Fatal("recovered save did not start")
	}
	h.wait(func() bool { return h.a.serverNotes == nil })
	stored, err = h.server.Vault.ServerNote(s.target.ID)
	if err != nil || stored.Manual != edited.Manual {
		t.Fatal("recovered save did not persist and close", stored, err)
	}
}

func TestServerNotesLoadFailureBlocksActionsAndRetryLoadsMarker(t *testing.T) {
	const marker = "marker: retry after metadata failure"
	h := newServerNotesConnection(t, nil)
	seedServerNote(t, h, marker)
	beforeAuths := h.fixture.Auths.Load()
	goodDir := h.a.client.Dir
	h.a.client.Dir = t.TempDir()

	h.a.openServerNotes(h.a.selected)
	s := h.a.serverNotes
	if s == nil || !s.loading {
		t.Fatal("notes did not start loading")
	}
	h.ui = ui.NewTester(h.a.serverNotesDialog, 760, 650)
	h.wait(func() bool { return !s.loading })
	if s.loadError == "" || s.original.Manual != "" || s.draft.Manual != "" {
		t.Fatal("unreachable client did not produce a clean load error", s.loadError, s.original, s.draft)
	}
	if _, visible := h.ui.Find("重试读取"); !visible {
		t.Fatal("load failure did not expose retry")
	}

	h.a.saveServerNotes(s)
	h.a.scanServerNotes(s)
	if s.saving || s.scanning || s.error != "" || s.original.Manual != "" || s.draft.Manual != "" || h.a.notesScanWait(s.target) > 0 {
		t.Fatal("load error accepted save or discovery actions", s.saving, s.scanning, s.error, s.draft)
	}
	if h.fixture.Auths.Load() != beforeAuths {
		t.Fatal("failed metadata load started an SSH discovery scan")
	}
	if stored, err := h.server.Vault.ServerNote(s.target.ID); err != nil || stored.Manual != marker {
		t.Fatal("blocked actions changed the broker note", stored, err)
	}

	h.a.client.Dir = goodDir
	h.click("重试读取")
	h.wait(func() bool {
		return h.a.notesCurrent(s) && !s.loading && s.loadError == ""
	})
	if s.original.Manual != marker || s.draft != s.original {
		t.Fatal("retry did not load the real broker marker", s.original, s.draft)
	}
	if h.fixture.Auths.Load() != beforeAuths {
		t.Fatal("retrying metadata load started an SSH discovery scan")
	}
}
