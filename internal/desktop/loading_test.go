package desktop

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"
	"sshclient/internal/sshconn"
	"sshclient/internal/testserver"
	"sshclient/internal/transfer"
	"sshclient/internal/vault"
)

// All editor loading tests use a fake native window and render its actual view.
func fakeEditorFactory(a *app) *[]*fakeToolsWindow {
	var windows []*fakeToolsWindow
	a.newEditorWindow = func(options mygo.WindowOptions, request, closed func()) toolsWindow {
		w := &fakeToolsWindow{options: options, onClosed: closed}
		windows = append(windows, w)
		return w
	}
	return &windows
}

// Visible tabs include connecting and failed attempts. Tests waiting for SSH
// must observe actual attached terminals rather than the number of tabs.
func readyTerminalCount(a *app) int {
	n := 0
	for _, s := range a.sessions {
		if s.term != nil && !s.closed && s.connectionState == "" {
			n++
		}
	}
	return n
}

func TestEditorWindowOpensBeforeReadingAndCompletionDoesNotStealFocus(t *testing.T) {
	updates := make(chan func(), 16)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	a := &app{ctx: ctx, dispatch: func(fn func()) { updates <- fn }}
	t.Cleanup(a.cancelEditorRead)
	windows := fakeEditorFactory(a)
	file := filepath.Join(t.TempDir(), "config.txt")
	if err := os.WriteFile(file, []byte("ready\n"), 0600); err != nil {
		t.Fatal(err)
	}
	a.readEditor(localEndpoint, file)
	if a.editShown || a.editPath != file || a.editorRead == nil || !a.editorRead.loading || a.edit.Buffer.String() != "" {
		t.Fatal("file tab waited for I/O")
	}
	select {
	case update := <-updates:
		update()
	case <-time.After(3 * time.Second):
		t.Fatal("editor window was not created")
	}
	if len(*windows) != 1 || (*windows)[0].shown != 1 {
		t.Fatal("editor did not open its standalone window")
	}
	tt := ui.NewTester(a.editorWindowView, 720, 480)
	if !tt.HasText("正在读取文件…") || !tt.HasText("取消读取") {
		t.Fatal("loading state missing", tt.Texts())
	}
	a.page, a.editShown = 4, false
	select {
	case update := <-updates:
		update()
	case <-time.After(3 * time.Second):
		t.Fatal("file read did not complete")
	}
	if a.page != 4 || a.editShown || a.edit.Buffer.String() != "ready\n" || a.editorRead != nil {
		t.Fatal("completion stole focus or lost content")
	}
	// A canceled/replaced document must never receive a late result.
	a.readEditor(localEndpoint, file)
	a.installEditor(localEndpoint, "/synthetic/new.txt", "keep this document", "")
	deadline := time.NewTimer(100 * time.Millisecond)
	defer deadline.Stop()
drain:
	for {
		select {
		case update := <-updates:
			update()
		case <-deadline.C:
			break drain
		}
	}
	if a.editPath != "/synthetic/new.txt" || a.edit.Buffer.String() != "keep this document" || a.editorRead != nil {
		t.Fatal("late read replaced another document")
	}
}

func TestLocalEditorReadRejectsGrowthPastSizeLimit(t *testing.T) {
	limit := bytes.Repeat([]byte("x"), 4<<20)
	if err := validateLocalEditorRead(limit, nil); err != nil {
		t.Fatalf("exactly 4 MiB should be accepted: %v", err)
	}
	oversized := append(limit, 'x')
	if err := validateLocalEditorRead(oversized, nil); err == nil || !strings.Contains(err.Error(), "4 MiB") {
		t.Fatalf("4 MiB plus one byte was not rejected: %v", err)
	}
	readErr := errors.New("synthetic read failure")
	if err := validateLocalEditorRead(oversized, readErr); !errors.Is(err, readErr) {
		t.Fatalf("read error was lost: %v", err)
	}
}

func TestUnsupportedFileShowsReasonAndCanRetryInItsExistingWindow(t *testing.T) {
	updates := make(chan func(), 16)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	a := &app{ctx: ctx, dispatch: func(fn func()) { updates <- fn }}
	t.Cleanup(a.cancelEditorRead)
	windows := fakeEditorFactory(a)
	file := filepath.Join(t.TempDir(), "binary.dat")
	os.WriteFile(file, []byte{'a', 0, 'b'}, 0600)
	a.readEditor(localEndpoint, file)
	select {
	case update := <-updates:
		update()
	case <-time.After(3 * time.Second):
		t.Fatal("editor window was not created")
	}
	if len(*windows) != 1 || (*windows)[0].shown != 1 {
		t.Fatal("editor did not open its standalone window")
	}
	tt := ui.NewTester(a.editorWindowView, 720, 480)
	for a.editorRead.loading {
		select {
		case update := <-updates:
			update()
			tt.Frame()
		case <-time.After(3 * time.Second):
			t.Fatal("binary read stalled")
		}
	}
	if !tt.HasText("文件暂未打开") || !tt.HasText("UTF-8") || !tt.HasText("重试读取") {
		t.Fatal("unsupported file looks frozen", tt.Texts())
	}
	os.WriteFile(file, []byte("now text"), 0600)
	if err := tt.Click("重试读取"); err != nil {
		t.Fatal(err)
	}
	for a.editorRead != nil {
		select {
		case update := <-updates:
			update()
		case <-time.After(3 * time.Second):
			t.Fatal("retry stalled")
		}
	}
	if a.edit.Buffer.String() != "now text" || a.editPath != file {
		t.Fatal("retry did not populate document")
	}
}

func TestServerAndRemoteFileExposeLoadingBeforeNetworkAndCloseCancels(t *testing.T) {
	h := newConnectionFlowHarness(t)
	stalled := testserver.NewWithSessionGate(t, func() bool { return false })
	p := stalled.Profile()
	if err := h.a.client.SaveConnection(h.ctx, vault.ProfileChange{Profile: p}, h.server.Vault); err != nil {
		t.Fatal(err)
	}
	h.a.connections = h.server.Vault.Summaries()
	h.a.selected, h.a.page = p.ID, 1
	h.a.toolsHidden = true
	h.ui = ui.NewTester(h.a.view, 1000, 760)
	h.a.openShellMode(true)
	h.ui.Frame()
	if len(h.a.sessions) != 1 || h.a.sessions[0].term != nil || !h.ui.HasText("正在连接服务器…") {
		t.Fatal("no immediate server tab", h.ui.Texts())
	}
	h.a.closeSessionAt(0)
	h.wait(func() bool { return len(h.a.connecting) == 0 })
	if len(h.a.sessions) != 0 {
		t.Fatal("canceled connection restored a tab")
	}
	windows := fakeEditorFactory(h.a)
	h.a.readEditor(p.ID, "/synthetic/stalled.txt")
	h.wait(func() bool { return len(*windows) == 1 })
	ed := ui.NewTester(h.a.editorWindowView, 720, 480)
	if h.a.editorRead == nil || !ed.HasText("正在读取文件…") || (*windows)[0].shown != 1 {
		t.Fatal("remote file read has no immediate window", ed.Texts())
	}
	if err := ed.Click("取消读取"); err != nil {
		t.Fatal(err)
	}
	if h.a.editorRead.loading || !strings.Contains(h.a.editorRead.error, "取消") {
		t.Fatal("cancel did not finish loading state")
	}
	if err := ed.Click("关闭"); err != nil {
		t.Fatal(err)
	}
	if h.a.editPath != "" || h.a.editorRead != nil || (*windows)[0].hidden != 1 {
		t.Fatal("closing a loading window retained the document")
	}
	// Cancellation completion may already be queued; it must not revive the document.
	select {
	case update := <-h.updates:
		update()
	case <-time.After(3 * time.Second):
		t.Fatal("canceled read did not report completion")
	}
	(*windows)[0].finishPendingUpdates()
	if h.a.editPath != "" || h.a.editorRead != nil || h.a.editorWin != nil {
		t.Fatal("late cancellation revived the closed editor")
	}
}

func TestRemoteFolderMenuDownloadsWithoutALocalPane(t *testing.T) {
	h := newConnectionFlowHarness(t)
	p := h.fixture.Profile()
	if err := h.a.client.SaveConnection(h.ctx, vault.ProfileChange{Profile: p}, h.server.Vault); err != nil {
		t.Fatal(err)
	}
	h.a.connections = h.server.Vault.Summaries()
	h.a.transfers = transfer.New(h.dir)
	t.Cleanup(h.a.transfers.PauseAll)
	source := t.TempDir()
	if err := os.Mkdir(filepath.Join(source, "folder"), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(source, "folder", "readme.txt"), []byte("downloaded\n"), 0600); err != nil {
		t.Fatal(err)
	}
	h.a.ensureFileLayout()
	pane := h.a.primaryFilePane
	pane.endpoint = p.ID
	b := pane.browser()
	h.a.applyDirectory(b, false, testserver.RemotePath(source), []sshconn.File{{Name: "folder", Directory: true, Mode: "drwx------"}}, b.seq, true)
	h.ui = ui.NewTester(func(c *ui.Context) { h.a.browserPane(c, pane) }, 720, 480)
	if len(h.a.fileLayout.panes()) != 1 {
		t.Fatal("fixture unexpectedly has another pane")
	}
	if err := h.ui.RightClick("folder"); err != nil {
		t.Fatal(err)
	}
	if err := h.ui.ChooseMenuItem("下载到本机…"); err != nil {
		t.Fatal(err, h.ui.Menu())
	}
	if !h.a.prompt.open || h.a.prompt.field != "本地目标文件夹（完整路径）" || !strings.Contains(h.a.prompt.message, "递归") {
		t.Fatal("folder menu did not explain the destination", h.a.prompt)
	}
	destination := t.TempDir()
	confirm := h.a.prompt.confirm
	h.a.prompt = promptState{}
	// Selection changes after the menu opened cannot redirect the download.
	pane.endpoint = "another-server"
	confirm(destination)
	h.wait(func() bool { return len(h.a.jobs) == 1 && h.a.jobs[0].State == "done" })
	if !h.a.transfersOpen || !h.a.jobs[0].Directory || h.a.jobs[0].Host != p.ID {
		t.Fatal("download did not expose its captured task", h.a.jobs)
	}
	body, err := os.ReadFile(filepath.Join(destination, "folder", "readme.txt"))
	if err != nil || string(body) != "downloaded\n" {
		t.Fatal(string(body), err)
	}
}
