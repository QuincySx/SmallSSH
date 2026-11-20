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

func saveTaskFixture(t *testing.T, a *app, name, state string, bytes int64) transfer.Job {
	t.Helper()
	j, err := a.transfers.Add("fixture", "download", filepath.Join(t.TempDir(), name), "/srv/"+name)
	if err != nil {
		t.Fatal(err)
	}
	j.State, j.Size, j.Bytes = state, 100, bytes
	if state == "error" {
		j.Error = "连接已断开，可重试"
	}
	b, _ := json.Marshal(j)
	if err := os.WriteFile(filepath.Join(a.dir, "transfers", j.ID, "state.json"), b, 0600); err != nil {
		t.Fatal(err)
	}
	return j
}

func TestTaskCenterRowsCancelRemoveAndClearFailed(t *testing.T) {
	a := statusDemo()
	a.dir = t.TempDir()
	a.transfers = transfer.New(a.dir)
	updates := make(chan func(), 8)
	a.dispatch = func(fn func()) { updates <- fn }
	paused := saveTaskFixture(t, a, "archive.zip", "paused", 40)
	partial := filepath.Join(filepath.Dir(paused.Local), ".archive.zip.ssh-part-"+paused.ID)
	if err := os.WriteFile(partial, make([]byte, 40), 0600); err != nil {
		t.Fatal(err)
	}
	failed := saveTaskFixture(t, a, "config.yaml", "error", 0)
	older := saveTaskFixture(t, a, "old.log", "error", 0)
	a.jobs = a.transfers.List()
	wait := func(what string) {
		t.Helper()
		select {
		case update := <-updates:
			update()
		case <-time.After(5 * time.Second):
			t.Fatal(what, "stalled")
		}
	}

	tt := ui.NewTester(a.view, 1280, 900)
	if err := tt.Click("任务中心"); err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"1 项已暂停 · 2 项失败", "继续", "重试", "清除失败", "取消传输 " + paused.ID, "移除失败任务 " + failed.ID} {
		if !tt.HasText(want) {
			t.Fatal("task center lacks", want, tt.Texts())
		}
	}
	if slices.Contains(tt.Texts(), "0 B / 100 B") || !slices.Contains(tt.Texts(), "40 B / 100 B") {
		t.Fatal("a failed task that never moved still shows an empty progress line", tt.Texts())
	}

	// A paused download can be cancelled: its partial file and record go.
	if err := tt.Click("取消传输 " + paused.ID); err != nil {
		t.Fatal(err)
	}
	wait("cancel")
	tt.Frame()
	if _, err := os.Lstat(partial); !os.IsNotExist(err) {
		t.Fatal("cancel kept the partial file", err)
	}
	if tt.HasText("archive.zip") || len(transfer.New(a.dir).List()) != 2 {
		t.Fatal("cancelled task remained", a.jobs)
	}

	// One failed task can be removed, and the rest cleared at once.
	if err := tt.Click("移除失败任务 " + failed.ID); err != nil {
		t.Fatal(err)
	}
	wait("remove")
	tt.Frame()
	if tt.HasText("config.yaml") || !tt.HasText("old.log") {
		t.Fatal("remove took the wrong failed task", tt.Texts())
	}
	if err := tt.Click("清除失败"); err != nil {
		t.Fatal(err)
	}
	wait("clear failed")
	tt.Frame()
	if tt.HasText(filepath.Base(older.Remote)) || tt.HasText("清除失败") || len(transfer.New(a.dir).List()) != 0 || !tt.HasText("暂无传输任务") {
		t.Fatal("failed tasks were not cleared", tt.Texts())
	}
}

func TestFailedServerOperationCanBeRemovedAndRunningOneStopped(t *testing.T) {
	a := statusDemo()
	stopped := false
	a.dockerJobs = []*dockerJob{
		{title: "更新 web", host: "fixture", status: "running", cancel: func() { stopped = true }},
		{title: "更新 db", host: "fixture", status: "error", error: "健康检查失败"},
	}
	tt := ui.NewTester(a.view, 1280, 900)
	if err := tt.Click("任务中心"); err != nil {
		t.Fatal(err)
	}
	if err := tt.Click("服务器操作"); err != nil {
		t.Fatal(err)
	}
	if err := tt.Click("停止等待 更新 web"); err != nil || !stopped {
		t.Fatal("running operation cannot be stopped from its row", err)
	}
	if err := tt.Click("移除记录 更新 db"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if len(a.dockerJobs) != 1 || a.dockerJobs[0].title != "更新 web" {
		t.Fatal("failed operation was not removed", len(a.dockerJobs))
	}
}
