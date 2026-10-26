package desktop

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"
	"sshclient/internal/approval"
	"sshclient/internal/broker"
	"sshclient/internal/sshconn"
	"sshclient/internal/systemauth"
	"sshclient/internal/testserver"
	"sshclient/internal/vault"
)

func TestOwnerFileActionsDoNotApproveExternalRequests(t *testing.T) {
	h := newConnectionFlowHarness(t)
	p := h.fixture.Profile()
	if err := h.a.client.SaveConnection(h.ctx, vault.ProfileChange{Profile: p}, h.server.Vault); err != nil {
		t.Fatal(err)
	}
	request, _, err := h.a.client.Request(h.ctx, approval.Operation{Host: p.ID, Kind: "exec", Command: "external", Client: "desktop spoof"}, time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	name := filepath.Join(dir, "notes.txt")
	if err = os.WriteFile(name, []byte("before"), 0600); err != nil {
		t.Fatal(err)
	}
	h.a.page = 1
	h.a.ensurePanes()
	h.a.primaryFilePane = newFilePane(p.ID, false)
	b := h.a.primaryFilePane.browser()
	b.dir = testserver.RemotePath(dir)
	b.address = b.dir
	h.a.loadDirectory(h.a.primaryFilePane, b.dir, true)
	h.wait(func() bool { return len(b.loading) == 0 })
	if len(b.cache[b.dir]) != 1 {
		t.Fatal("directory was not loaded without extra approval", b.loadError)
	}
	h.a.readEditor(p.ID, testserver.RemotePath(name))
	h.wait(func() bool { return h.a.editPath != "" })
	h.a.edit.Insert("changed ")
	h.a.saveEditor()
	h.wait(func() bool { return !h.a.edit.Buffer.Modified() })
	data, err := os.ReadFile(name)
	if err != nil || !strings.Contains(string(data), "changed") {
		t.Fatal("editor save failed", err, string(data))
	}
	status, err := h.a.client.Call(h.ctx, broker.Message{Action: "status", ID: request.ID})
	if err != nil || status.Request == nil || status.Request.Status != "pending" {
		t.Fatal("external operation inherited owner authority", err)
	}
	if h.a.ownerPass != "" || h.a.pass != "" || h.a.approvalWin != nil {
		t.Fatal("file action required retained password or popup")
	}
}

func TestPromptValidationKeepsInputAndCreatesOnlyAfterCorrection(t *testing.T) {
	updates := make(chan func(), 16)
	a := &app{ctx: context.Background(), online: true, dispatch: func(fn func()) { updates <- fn }}
	a.ensurePanes()
	a.primaryFilePane = newFilePane(localEndpoint, false)
	b := a.primaryFilePane.browser()
	b.dir = t.TempDir()
	b.address = b.dir
	b.cache[b.dir] = []sshconn.File{}
	a.fileOperationAt(a.primaryFilePane, b.dir, nil, "new")
	tt := ui.NewTester(a.view, 1000, 760)
	if err := tt.Click("文件名"); err != nil {
		t.Fatal(err)
	}
	tt.Type("../bad")
	tt.Key(0, ui.KeyEnter)
	if !a.prompt.open || a.prompt.error == "" || a.prompt.value != "../bad" {
		t.Fatal("invalid value dismissed or lost", a.prompt)
	}
	a.prompt.value = "good.txt"
	tt.Frame()
	if err := tt.Click("确认"); err != nil {
		t.Fatal(err)
	}
	select {
	case fn := <-updates:
		fn()
	case <-time.After(3 * time.Second):
		t.Fatal("file creation did not complete")
	}
	if _, err := os.Stat(filepath.Join(b.dir, "good.txt")); err != nil || a.prompt.open {
		t.Fatal("corrected creation failed", err)
	}
}

func TestUnsavedConnectionNavigationRequiresExplicitDiscard(t *testing.T) {
	a := &app{ctx: context.Background(), online: true}
	a.newConnection()
	a.form.Name = "draft"
	a.form.Password = "draft password"
	called := false
	a.navigate(func() { called = true; a.page = 4 })
	if called || !a.prompt.open || a.form.Password == "" {
		t.Fatal("navigation silently discarded input")
	}
	a.prompt = promptState{}
	if a.form.Name != "draft" {
		t.Fatal("cancelled discard lost input")
	}
	a.navigate(func() { called = true; a.page = 4 })
	confirm := a.prompt.confirm
	a.prompt = promptState{}
	confirm("")
	if !called || a.page != 4 || a.form.Password != "" {
		t.Fatal("explicit discard did not clear input")
	}
}

func TestOldWorkCannotOverwriteNewUnlockState(t *testing.T) {
	updates := make(chan func(), 2)
	a := &app{ctx: context.Background(), online: true, dispatch: func(fn func()) { updates <- fn }}
	release := make(chan struct{})
	a.work(func() (func(), error) { <-release; return nil, errors.New("obsolete failure") })
	a.lock()
	a.status = "current state"
	a.busy = true
	close(release)
	select {
	case fn := <-updates:
		fn()
	case <-time.After(time.Second):
		t.Fatal("work callback missing")
	}
	if a.status != "current state" || !a.busy || a.unlockError != "" {
		t.Fatal("obsolete work changed current state")
	}
}

func TestLocalEditorRejectsDirectoriesLinksAndOversizedFiles(t *testing.T) {
	dir := t.TempDir()
	if _, err := readLocalText(dir); err == nil {
		t.Fatal("directory accepted")
	}
	name := filepath.Join(dir, "file")
	if err := os.WriteFile(name, []byte("safe"), 0600); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(dir, "link")
	if err := os.Symlink(name, link); err == nil {
		if _, err = readLocalText(link); err == nil {
			t.Fatal("symbolic link accepted for editing")
		}
	}
	file, err := os.OpenFile(name, os.O_WRONLY, 0600)
	if err != nil {
		t.Fatal(err)
	}
	err = file.Truncate((4 << 20) + 1)
	file.Close()
	if err != nil {
		t.Fatal(err)
	}
	if _, err = readLocalText(name); err == nil {
		t.Fatal("oversized file accepted")
	}
	if got := permissionValue(sshconn.File{Mode: "drwx------", Directory: true}); got != "700" {
		t.Fatal("permissions reset", got)
	}
	if got := permissionValue(sshconn.File{Mode: "-rwSr--r--"}); got != "644" {
		t.Fatal("uppercase setuid incorrectly enabled execution", got)
	}
}

func TestRequestReviewShowsRelevantScopeAndEscapesInvisibleText(t *testing.T) {
	r := approval.Request{ID: "test", Status: "pending", Expires: time.Now().Add(time.Minute), Operation: approval.Operation{Kind: "exec", Endpoint: "root@example:22", Client: "AI\nverified\u202e", Command: "printf safe\nrm dangerous", ContentHash: "example-hash"}}
	tt := ui.NewTester(func(c *ui.Context) { ui.Column(c).Fill().Children(func() { requestReview(c, r) }) }, 700, 600)
	if !tt.HasText("printf safe\\nrm dangerous") || !tt.HasText("AI\\nverified\\u202e") || tt.HasText("example-hash") {
		t.Fatal("review hid controls or exposed irrelevant detail", tt.Texts())
	}
	if err := tt.Click("技术详情"); err != nil {
		t.Fatal(err)
	}
	if !tt.HasText("example-hash") {
		t.Fatal("technical information unavailable")
	}
	if strings.Contains(reviewText("ok\x1b[2J"), "\x1b") {
		t.Fatal("control character not escaped")
	}
}

func TestAuxiliaryScreensAndFileFooterFitSmallWindow(t *testing.T) {
	a := &app{ctx: context.Background(), online: true, page: 4, settingsSection: settingsSecurity, sidebarWidth: 190, groups: map[string]*bool{}}
	a.authStatus = systemauth.Status{Available: true, Enabled: true, Name: "Touch ID"}
	tt := ui.NewTester(a.view, 1000, 760)
	for _, label := range []string{"工作台", "设置", "主密码确认设置", "停用 Touch ID"} {
		r, ok := tt.Find(label)
		if !ok || r.Y < 0 || r.Y+r.H > 760 {
			t.Fatal("security control clipped", label, r)
		}
	}
	a.page = 1
	a.selected = "demo"
	a.toolsHidden = false
	a.ensurePanes()
	b := a.primaryFilePane.browser()
	b.dir = "/fixture"
	b.address = b.dir
	b.cache[b.dir] = []sshconn.File{{Name: "a"}, {Name: "b"}, {Name: "c"}, {Name: "d"}}
	b.reveal(false, b.dir)
	b.indexDirectory(false, b.dir)
	tt.Frame()
	r, ok := tt.Find("4 项")
	if !ok || r.Y < 0 || r.Y+r.H > 760 {
		t.Fatal("file footer clipped", r)
	}
}

func TestEditorSaveShortcutWritesOnlyFocusedDocument(t *testing.T) {
	dir := t.TempDir()
	name := filepath.Join(dir, "notes.txt")
	original := "before"
	if err := os.WriteFile(name, []byte(original), 0600); err != nil {
		t.Fatal(err)
	}
	updates := make(chan func(), 8)
	a := &app{ctx: context.Background(), online: true, page: 1, toolsHidden: true, sidebarWidth: 190, dispatch: func(fn func()) { updates <- fn }}
	a.installEditor(localEndpoint, name, original, textHash([]byte(original)))
	tt := ui.NewTester(a.view, 1000, 760)
	if err := tt.Click("文件编辑器"); err != nil {
		t.Fatal(err)
	}
	tt.Type(" edited")
	tt.Key(ui.Cmd, ui.KeyS)
	select {
	case fn := <-updates:
		fn()
	case <-time.After(3 * time.Second):
		t.Fatal("save shortcut did not execute")
	}
	got, err := os.ReadFile(name)
	if err != nil || !strings.Contains(string(got), "edited") || a.edit.Buffer.Modified() {
		t.Fatal("save shortcut did not persist current document", err, string(got))
	}
}

func TestDeletingBackgroundConnectionPreservesActiveSessionAndNewDraft(t *testing.T) {
	h := newConnectionFlowHarness(t)
	p := h.fixture.Profile()
	if err := h.a.client.SaveConnection(h.ctx, vault.ProfileChange{Profile: p}, h.server.Vault); err != nil {
		t.Fatal(err)
	}
	other := p
	other.ID = "other"
	other.Name = "other"
	if err := h.a.client.SaveConnection(h.ctx, vault.ProfileChange{Profile: other}, h.server.Vault); err != nil {
		t.Fatal(err)
	}
	h.a.connections = h.server.Vault.Summaries()
	h.a.page = 1
	h.a.selected = other.ID
	keep := &session{host: other.ID, name: other.Name}
	h.a.sessions = []*session{{host: p.ID, name: p.Name}, keep}
	h.a.active = 1
	var summary vault.Summary
	for _, d := range h.a.connections {
		if d.ID == p.ID {
			summary = d
		}
	}
	h.a.deleteConnection(summary)
	confirm := h.a.prompt.confirm
	h.a.prompt = promptState{}
	confirm("")
	h.a.newConnection()
	h.a.form.Name = "new draft"
	h.a.form.Password = "synthetic draft only"
	h.wait(func() bool { return !h.a.busy })
	if len(h.a.sessions) != 1 || h.a.sessions[h.a.active] != keep || h.a.selected != other.ID || h.a.page != 0 || h.a.form.Name != "new draft" || h.a.form.Password == "" {
		t.Fatal("deleting a background connection changed active work")
	}
}
