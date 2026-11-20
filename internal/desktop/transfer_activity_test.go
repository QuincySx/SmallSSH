package desktop

import (
	"encoding/json"
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
	if tt.HasText("任务中心面板") || slices.Contains(tt.Texts(), "传输") || a.transfersOpen {
		t.Fatal("queue opened itself or remained a tool tab", tt.Texts())
	}
	if err := tt.Click("任务中心"); err != nil {
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
	if !a.transfersOpen || !a.toolsHidden || a.dock != toolDiagnostics || !tt.HasText("任务中心面板") {
		t.Fatal("menu entry exposed or selected a tool panel")
	}
	a.sessions = nil // The offline status fixture has no native terminal to close.
	a.lock()
	tt.Frame()
	if a.transfersOpen || tt.HasText("index.html") || tt.HasText("任务中心面板") {
		t.Fatal("locked app retained the queue UI")
	}
}

func TestCompletedTransferHistoryIsHiddenAndCanBeRemoved(t *testing.T) {
	a := statusDemo()
	a.dir = t.TempDir()
	a.transfers = transfer.New(a.dir)
	updates := make(chan func(), 8)
	a.dispatch = func(fn func()) { updates <- fn }
	for _, state := range []string{"done", "done", "paused", "error"} {
		j, err := a.transfers.Add("fixture", "download", filepath.Join(t.TempDir(), state), "/srv/"+state)
		if err != nil {
			t.Fatal(err)
		}
		j.State = state
		b, _ := json.Marshal(j)
		if err := os.WriteFile(filepath.Join(a.dir, "transfers", j.ID, "state.json"), b, 0600); err != nil {
			t.Fatal(err)
		}
		if state == "done" {
			if err := os.WriteFile(j.Local, []byte("retained file"), 0600); err != nil {
				t.Fatal(err)
			}
		}
	}
	a.jobs = a.transfers.List()
	all := slices.Clone(a.jobs)
	tt := ui.NewTester(a.view, 1280, 900)
	if err := tt.Click("任务中心"); err != nil {
		t.Fatal(err)
	}
	if tt.HasText("done") || !tt.HasText("paused") || !tt.HasText("error") {
		t.Fatal("history is visible or resumable work hidden", tt.Texts())
	}
	if err := tt.Click("历史记录"); err != nil {
		t.Fatal(err)
	}
	if !tt.HasText("done") || tt.HasText("paused") || tt.HasText("error") {
		t.Fatal("history filter", tt.Texts())
	}
	id := a.visibleTransferJobs(true)[0].ID
	if err := tt.Click("移除记录 " + id); err != nil {
		t.Fatal(err)
	}
	select {
	case update := <-updates:
		update()
	case <-time.After(3 * time.Second):
		t.Fatal("remove stalled")
	}
	tt.Frame()
	if len(a.visibleTransferJobs(true)) != 1 || len(a.jobs) != 3 {
		t.Fatal("single remove", a.jobs)
	}
	if err := tt.Click("清空记录"); err != nil {
		t.Fatal(err)
	}
	select {
	case update := <-updates:
		update()
	case <-time.After(3 * time.Second):
		t.Fatal("clear stalled")
	}
	tt.Frame()
	if !tt.HasText("暂无已完成记录") || len(a.jobs) != 2 || len(transfer.New(a.dir).List()) != 2 {
		t.Fatal("clear did not persist", a.jobs)
	}
	for _, j := range all {
		if j.State == "done" {
			if b, err := os.ReadFile(j.Local); err != nil || string(b) != "retained file" {
				t.Fatal("cleared a downloaded file", err)
			}
		}
	}
	tt.Key(0, ui.KeyEscape)
	if err := tt.Click("任务中心"); err != nil {
		t.Fatal(err)
	}
	if a.activityHistory[0] != 0 || !tt.HasText("paused") {
		t.Fatal("reopen did not default to current work")
	}
}

func TestServerTaskHistoryClearingKeepsCurrentOperations(t *testing.T) {
	a := statusDemo()
	a.dockerJobs = []*dockerJob{{title: "complete", status: "done"}, {title: "running", status: "running"}, {title: "failed", status: "error"}}
	tt := ui.NewTester(a.view, 1280, 900)
	if err := tt.Click("任务中心"); err != nil {
		t.Fatal(err)
	}
	if err := tt.Click("服务器操作"); err != nil {
		t.Fatal(err)
	}
	if tt.HasText("complete") || !tt.HasText("running") || !tt.HasText("failed") {
		t.Fatal(tt.Texts())
	}
	if err := tt.Click("历史记录"); err != nil {
		t.Fatal(err)
	}
	if err := tt.Click("移除记录 complete"); err != nil {
		t.Fatal(err)
	}
	if len(a.dockerJobs) != 2 || !tt.HasText("暂无已完成记录") {
		t.Fatal("server history removal changed current operations")
	}
	a.dockerJobs = append(a.dockerJobs, &dockerJob{title: "another", status: "done"})
	tt.Frame()
	if err := tt.Click("清空记录"); err != nil {
		t.Fatal(err)
	}
	if len(a.dockerJobs) != 2 {
		t.Fatal("clear affected running or failed operation")
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
