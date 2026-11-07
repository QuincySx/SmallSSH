package desktop

import (
	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"
	"os"
	"path/filepath"
	"sshclient/internal/approval"
	"sshclient/internal/testserver"
	"sshclient/internal/vault"
	"testing"
	"time"
)

func TestRemoteSaveAsProtectsNativeCloseAndPreservesLaterEdits(t *testing.T) {
	h := newConnectionFlowHarness(t)
	profile := h.fixture.Profile()
	if err := h.a.client.SaveConnection(h.ctx, vault.ProfileChange{Profile: profile}, h.server.Vault); err != nil {
		t.Fatal(err)
	}
	var requestClose func()
	var window *fakeToolsWindow
	h.a.newEditorWindow = func(o mygo.WindowOptions, request, closed func()) toolsWindow {
		requestClose = request
		window = &fakeToolsWindow{options: o, onClosed: closed}
		return window
	}
	directory := t.TempDir()
	h.a.page = 1
	h.a.installEditor(profile.ID, testserver.RemotePath(filepath.Join(directory, "original.conf")), "one\r\n", "")
	h.wait(func() bool { return window != nil })
	document := h.a.edit
	h.a.saveAs = testserver.RemotePath(filepath.Join(directory, "saved.conf"))
	h.a.upload(true)
	requestClose()
	if h.a.edit != document || h.a.editorSaving != document || h.a.editPath == "" || window.hidden != 0 {
		t.Fatal("native close discarded an in-flight Save As")
	}
	h.a.edit.Insert("later ")
	h.wait(func() bool { return h.a.editorSaving == nil })
	saved, err := os.ReadFile(filepath.Join(directory, "saved.conf"))
	if err != nil || string(saved) != "one\r\n" || !h.a.edit.Buffer.Modified() || window.title != "saved.conf" {
		t.Fatal("Save As lost captured bytes, later edits or title", string(saved), err, window.title)
	}
}

func TestSettingsApprovalEscapeDisposesAuthenticationAndDoesNotRecreate(t *testing.T) {
	a, r := reviewFixture("full")
	r.Expires = time.Now().Add(time.Minute)
	a.online, a.settingsOpen = true, true
	a.page, a.settingsSection = 1, settingsApprovals
	a.requests = []approval.Request{r}
	a.selectedRequest = r.ID
	tt := ui.NewTester(a.settingsWindowView, 1080, 760)
	state := a.inlineApproval
	if state == nil {
		t.Fatal("settings approval was not rendered")
	}
	state.password = "synthetic password"
	canceled := false
	state.cancel = func() { canceled = true }
	tt.Key(0, ui.KeyEscape)
	if a.settingsOpen || a.inlineApproval != nil || !canceled || state.password != "" || !state.closed {
		t.Fatalf("Escape did not dispose settings authentication: open=%v inline=%v canceled=%v passwordEmpty=%v closed=%v busy=%v texts=%v", a.settingsOpen, a.inlineApproval != nil, canceled, state.password == "", state.closed, state.busy, tt.Texts())
	}
	// The main workbench does not rebuild the dismissed independent review.
	ui.NewTester(a.view, 1080, 760)
	if a.inlineApproval != nil {
		t.Fatal("dismissed settings review was rebuilt")
	}
}
