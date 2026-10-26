package desktop

import (
	"os"
	"path/filepath"
	"slices"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"
	"sshclient/internal/transfer"
)

func TestTransferActivityIsExplicitAndPreservesCurrentTool(t *testing.T) {
	a := statusDemo()
	a.jobs = []transfer.Job{{ID: "a", Remote: "/srv/index.html", State: "running", Size: 100, Bytes: 35}, {ID: "b", Remote: "/srv/config", State: "queued"}, {ID: "c", Remote: "/srv/log", State: "error"}, {ID: "d", State: "done"}, {ID: "e", State: "paused"}}
	active, failed := transferActivityCounts(a.jobs)
	if active != 2 || failed != 1 {
		t.Fatal("activity badge counted history as active", active, failed)
	}
	tt := ui.NewTester(a.view, 1280, 900)
	term, _ := tt.Find("终端区域")
	if tt.HasText("传输任务面板") || slices.Contains(tt.Texts(), "传输") || a.transfersOpen {
		t.Fatal("queue opened itself or remained a tool tab", tt.Texts())
	}
	if err := tt.Click("传输任务"); err != nil {
		t.Fatal(err)
	}
	if !a.transfersOpen || !tt.HasText("index.html") || a.dock != toolDiagnostics {
		t.Fatal("queue replaced the active tool", tt.Texts())
	}
	a.jobs[0].Bytes = 60
	tt.Frame()
	after, _ := tt.Find("终端区域")
	if after != term || !a.transfersOpen || a.dock != toolDiagnostics {
		t.Fatal("progress changed workspace layout")
	}
	tt.Key(0, ui.KeyEscape)
	if a.transfersOpen || a.dock != toolDiagnostics {
		t.Fatal("Escape changed the tool or did not close the queue")
	}
	a.jobs[0].State = "done"
	tt.Frame()
	if a.transfersOpen {
		t.Fatal("completion reopened the popup")
	}
	a.toolsHidden = true
	a.showTool(toolTransfers)
	tt.Frame()
	if !a.transfersOpen || !a.toolsHidden || a.dock != toolDiagnostics || !tt.HasText("传输任务面板") {
		t.Fatal("menu entry exposed or selected a tool panel")
	}
	a.sessions = nil // The offline status fixture has no native terminal to close.
	a.lock()
	tt.Frame()
	if a.transfersOpen || tt.HasText("index.html") || tt.HasText("传输任务面板") {
		t.Fatal("locked app retained the queue UI")
	}
}

func TestOldTransferTabPreferenceRestoresFilesWithoutPopup(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "layout.json"), []byte(`{"version":1,"tab":1,"terminalShare":68}`), 0600); err != nil {
		t.Fatal(err)
	}
	a := &app{dir: dir}
	a.loadLayout()
	if a.dock != toolFiles || a.transfersOpen || a.terminalShare != 68 {
		t.Fatal("old transfer tab broke restoration or opened activity")
	}
}

func TestEnqueueTransferPreservesOpenConnectionDraft(t *testing.T) {
	a := statusDemo()
	a.dir = t.TempDir()
	a.transfers = transfer.New(a.dir)
	updates := make(chan func(), 4)
	a.dispatch = func(fn func()) { updates <- fn }
	a.newConnection()
	a.form.Name, a.form.Password = "未保存连接", "synthetic draft secret"
	id, dock := a.form.ID, a.dock
	file := filepath.Join(a.dir, "example.txt")
	if err := os.WriteFile(file, []byte("offline fixture"), 0600); err != nil {
		t.Fatal(err)
	}
	a.addTransfer("upload", file, "/tmp/example.txt")
	select {
	case update := <-updates:
		update()
	case <-time.After(3 * time.Second):
		t.Fatal("enqueue did not finish")
	}
	// This fixture has no unlocked owner or SSH endpoint. The file is queued,
	// while running it is refused locally without issuing any network request.
	if len(a.jobs) != 1 || a.page != 0 || a.form.ID != id || a.form.Password == "" || a.dock != dock || a.transfersOpen {
		t.Fatal("enqueue hijacked the workspace or lost a connection draft")
	}
}
