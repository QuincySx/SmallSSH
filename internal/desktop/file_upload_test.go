package desktop

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"
	"sshclient/internal/connectcfg"
	"sshclient/internal/sshdata"
	"sshclient/internal/testserver"
	"sshclient/internal/transfer"
	"sshclient/internal/vault"
)

func uploadFixture(t *testing.T) (*app, *filePane, func()) {
	t.Helper()
	a := fileFollowDemo()
	a.transfers = transfer.New(t.TempDir())
	callbacks := make(chan func(), 2)
	a.dispatch = func(fn func()) { callbacks <- fn }
	return a, a.primaryFilePane, func() {
		t.Helper()
		select {
		case fn := <-callbacks:
			fn()
		case <-time.After(3 * time.Second):
			t.Fatal("upload result did not return to the UI")
		}
	}
}

func TestFileUploadConflictRequiresConfirmationAndPreservesPreviousFile(t *testing.T) {
	h := newConnectionFlowHarness(t)
	profile := h.fixture.Profile()
	if err := h.a.client.SaveConnection(h.ctx, vault.ProfileChange{Profile: profile}, h.server.Vault); err != nil {
		t.Fatal(err)
	}
	h.a.transfers = transfer.New(h.dir)
	h.ui = ui.NewTester(func(c *ui.Context) { h.a.promptForHost(c, dialogHostMain) }, 820, 420)
	source := writeUploadFixture(t, "existing.txt")
	destination := t.TempDir()
	remote := filepath.Join(destination, "existing.txt")
	writeRemote := func(content string) {
		t.Helper()
		if err := os.WriteFile(remote, []byte(content), 0600); err != nil {
			t.Fatal(err)
		}
	}
	writeRemote("previous file")
	h.a.queueUploadPaths(profile.ID, testserver.RemotePath(destination), connectcfg.RouteSelection{}, []string{source})
	waitForConflict := func() {
		t.Helper()
		h.wait(func() bool { return h.a.prompt.open || h.a.status != "" })
		if !h.ui.HasText("覆盖远程文件？") || len(h.a.jobs) != 1 {
			t.Fatal("did not show the upload collision", h.a.status, h.a.jobs)
		}
	}
	waitForConflict()
	assertMovedFile(t, remote, "previous file")
	h.click("跳过")
	if h.a.prompt.open {
		t.Fatal("skip did not close the confirmation")
	}
	assertMovedFile(t, remote, "previous file")
	id := h.a.jobs[0].ID
	h.a.runTransfer(id)
	waitForConflict()
	writeRemote("changed after confirmation opened")
	h.click("覆盖")
	h.wait(func() bool { return h.a.status != "" })
	assertMovedFile(t, remote, "changed after confirmation opened")
	if h.a.jobs[0].State != "error" {
		t.Fatal("a changed remote file was not rejected", h.a.jobs[0])
	}
	h.a.runTransfer(id)
	waitForConflict()
	h.click("覆盖")
	h.wait(func() bool { return h.a.jobs[0].State == "done" })
	assertMovedFile(t, remote, "synthetic upload")
	backup := testserver.LocalPath(h.a.jobs[0].Backup)
	assertMovedFile(t, backup, "changed after confirmation opened")
}

func writeUploadFixture(t *testing.T, name string) string {
	t.Helper()
	file := filepath.Join(t.TempDir(), name)
	if err := os.WriteFile(file, []byte("synthetic upload"), 0600); err != nil {
		t.Fatal(err)
	}
	return file
}

func TestFileUploadPickerKeepsExactPaneAndAcceptsMultipleSources(t *testing.T) {
	a, pane, finish := uploadFixture(t)
	route := connectcfg.RouteSelection{Mode: "saved", ID: "explicit"}
	pane.route = route
	pane.browser().dir = "/srv/initial"
	first, second := writeUploadFixture(t, "first.txt"), writeUploadFixture(t, "second.txt")
	result := make(chan []string, 1)
	a.selectPaneUpload(pane, func() ([]string, error) { return <-result, nil })
	a.chooseFileEndpoint(pane, "b")
	result <- []string{first, second}
	finish() // Native picker result.
	finish() // Queue creation.

	if a.prompt.open || len(a.jobs) != 2 {
		t.Fatalf("native selection opened a path prompt or lost a file: %d jobs", len(a.jobs))
	}
	for _, job := range a.jobs {
		wantLocal := map[string]string{"/srv/initial/first.txt": first, "/srv/initial/second.txt": second}
		if job.Host != "a" || job.Route != route || wantLocal[job.Remote] != job.Local {
			t.Fatalf("queued the wrong source, target, or route: %+v", job)
		}
	}
	a.selectPaneUpload(pane, func() ([]string, error) { return nil, nil })
	finish()
	if a.busy || len(a.jobs) != 2 {
		t.Fatal("dismissing the system picker made a job or left upload busy")
	}
}

func TestNativeDropUploadsToRemoteFolderAndEmptyDirectory(t *testing.T) {
	a, pane, finish := uploadFixture(t)
	pane.treeHidden = true
	route := connectcfg.RouteSelection{Mode: "direct"}
	pane.route = route
	browser := pane.browser()
	a.applyDirectory(browser, false, "/srv/a", []sshdata.File{{Name: "target", Directory: true}}, browser.seq, true)
	tt := ui.NewTester(func(c *ui.Context) { a.browserPane(c, pane) }, 950, 500)
	folder, ok := tt.Find("target")
	if !ok || !tt.DropFiles(folder.X+min(40, folder.W/2), folder.Y+folder.H/2, writeUploadFixture(t, "folder-drop.txt")) {
		t.Fatal("remote folder did not accept native file drag")
	}
	finish()
	if len(a.jobs) != 1 || a.jobs[0].Remote != "/srv/a/target/folder-drop.txt" || a.jobs[0].Route != route {
		t.Fatal("native drop did not retain the exact folder and its route", a.jobs)
	}
	a.applyDirectory(browser, false, "/srv/a/empty", nil, browser.seq, true)
	tt.Frame()
	list, ok := tt.Find("文件列表")
	if !ok || !tt.DropFiles(list.X+list.W/2, list.Y+list.H/2, writeUploadFixture(t, "empty-drop.txt")) {
		t.Fatal("empty remote directory did not accept native file drag")
	}
	finish()
	if len(a.jobs) != 2 || a.prompt.open {
		t.Fatal("native file drop did not start a transfer directly", a.jobs)
	}
	for _, job := range a.jobs {
		if job.Remote == "/srv/a/empty/empty-drop.txt" && job.Host == "a" && job.Route == route {
			return
		}
	}
	t.Fatal("drop in empty space missed the current remote directory", a.jobs)
}
