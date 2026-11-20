package desktop

import (
	"context"
	"testing"

	"github.com/egoist/mygo/ui"
	"sshclient/internal/approval"
	"sshclient/internal/sshconn"
)

// Every in-progress state offers a way out, and every finished failure can be
// dismissed. These cover the states the review found without either.

func TestSlowFolderCanBeCancelledOrLeft(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	a := &app{ctx: ctx, dispatch: func(func()) {}, directorySlots: make(chan struct{}, 1)}
	a.directorySlots <- struct{}{} // Hold the listing so it stays in progress.
	a.ensureFileLayout()
	p := a.localFilePane
	p.treeHidden = true
	b := p.browser()
	a.applyDirectory(b, true, "/synthetic", []sshconn.File{{Name: "next", Directory: true}}, b.seq, true)
	a.openEntries(p, b.dir, []sshconn.File{{Name: "next", Directory: true}})
	load := b.loading["/synthetic/next"]
	tt := ui.NewTester(func(c *ui.Context) { a.browserPane(c, p) }, 800, 400)
	if load == nil || !tt.HasText("正在读取文件夹…") {
		t.Fatal("folder did not start loading", tt.Texts())
	}
	if err := tt.Click("取消读取"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if load.ctx.Err() == nil || b.loading["/synthetic/next"] != nil || !tt.HasText("已取消读取") {
		t.Fatal("cancel did not stop the load and land on the retry view", tt.Texts())
	}
	if _, ok := tt.Find("重试"); !ok {
		t.Fatal("cancelled folder lost its retry")
	}
	a.openEntries(p, "/synthetic", []sshconn.File{{Name: "next", Directory: true}})
	tt.Frame()
	if err := tt.Click("上一级"); err != nil {
		t.Fatal(err)
	}
	if b.dir != "/synthetic" {
		t.Fatal("parent button did not leave the slow folder", b.dir)
	}
	cancelPaneLoads(p)
}

func TestConnectingTabSaysCancelAndFailedTabSaysClose(t *testing.T) {
	s := &session{host: "a", name: "连接 A", connectionState: "connecting", target: approval.Operation{Host: "a", Kind: "shell"}}
	a := &app{ctx: context.Background(), online: true, page: 1, sessions: []*session{s}}
	a.ensureTerminalGroups()
	tt := ui.NewTester(func(c *ui.Context) { a.pendingTerminalView(c, s) }, 640, 400)
	if !tt.HasText("取消连接") || tt.HasText("关闭此标签") {
		t.Fatal("a connecting tab does not offer cancel", tt.Texts())
	}
	s.connectionState, s.connectionError = "error", "refused"
	tt.Frame()
	if !tt.HasText("关闭此标签") || !tt.HasText("重试连接") {
		t.Fatal("a failed tab lost close or retry", tt.Texts())
	}
}

func TestStoppedAndFailedTunnelsCanBeRemoved(t *testing.T) {
	stopped := false
	a := &app{ctx: context.Background(), online: true}
	a.tunnels = tunnelManager{open: true, showingServices: true, host: "a", kind: "local", listen: "127.0.0.1:0", target: "127.0.0.1:80", active: []*tunnelRun{
		{host: "a", kind: "local", address: "127.0.0.1:5000", status: "运行中", running: true, ready: true, cancel: func() { stopped = true }},
		{host: "a", kind: "local", address: "127.0.0.1:6000", status: "监听失败", failed: true},
	}}
	tt := ui.NewTester(a.tunnelsView, 760, 680)
	if _, ok := tt.Find("移除隧道记录 127.0.0.1:5000"); ok {
		t.Fatal("a running tunnel can be removed without stopping")
	}
	if err := tt.Click("停止"); err != nil || !stopped {
		t.Fatal("running tunnel cannot stop", err)
	}
	if err := tt.Click("移除隧道记录 127.0.0.1:6000"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if len(a.tunnels.active) != 1 || a.tunnels.active[0].address != "127.0.0.1:5000" {
		t.Fatal("failed tunnel stayed listed", len(a.tunnels.active))
	}
}

func TestRunningServiceActionCanStopWaiting(t *testing.T) {
	a := statusDemo()
	sess := a.sessions[0]
	if sess.management == nil {
		sess.management = newManagementUI()
	}
	stopped := false
	sess.management.busy, sess.management.message = true, "正在重启 nginx…"
	sess.management.stop = func() { stopped = true }
	a.active = 0
	tt := ui.NewTester(func(c *ui.Context) { a.managementFeedback(c) }, 640, 120)
	if err := tt.Click("停止等待"); err != nil || !stopped {
		t.Fatal("a running service action cannot stop waiting", err)
	}
}

func TestConnectionFormOperationsStopWithoutClosingTheForm(t *testing.T) {
	for _, phase := range []string{"route-measure", "hardware"} {
		a := fileFollowDemo()
		a.newConnection()
		a.form.Fingerprint, a.form.Hardware.Module = "SHA256:example", "/example/pkcs11.dylib"
		cancelled := false
		a.connection.phase, a.connection.cancel = phase, func() { cancelled = true }
		seq := a.connection.sequence
		view := a.routeMeasurementsView
		stop := "停止测量"
		if phase == "hardware" {
			view, stop = a.hardwareForm, "停止读取"
		}
		tt := ui.NewTester(func(c *ui.Context) { ui.Column(c).Fill().Children(func() { view(c) }) }, 720, 600)
		if err := tt.Click(stop); err != nil {
			t.Fatal(phase, err)
		}
		if !cancelled || a.connection.phase != "" || a.connection.sequence == seq || a.page != 0 {
			t.Fatal("stop did not abandon the operation or closed the form", phase, a.connection.phase)
		}
	}
}
