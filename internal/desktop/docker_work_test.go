//go:build terminal_integration

package desktop

import (
	"context"
	"fmt"
	"io"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"
	"sshclient/internal/approval"
	"sshclient/internal/broker"
	"sshclient/internal/client"
	"sshclient/internal/monitor"
	"sshclient/internal/serverops"
	"sshclient/internal/testdocker"
	"sshclient/internal/testserver"
	"sshclient/internal/vault"
)

func dockerUIFixture(t *testing.T) (*app, *testdocker.Fixture, chan func()) {
	t.Helper()
	f := testdocker.New(t)
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Second)
	t.Cleanup(cancel)
	ssh := testserver.NewWithStreamExec(t, func(sessionCtx context.Context, command string, in io.Reader, out, stderr io.Writer) int {
		if command == monitor.Command {
			io.WriteString(out, "@@CPU\ncpu 10 0 10 80\n@@MEM\nMemTotal: 524288 kB\nMemAvailable: 300000 kB\n")
			return 0
		}
		if command == monitor.InventoryCommand {
			io.WriteString(out, "@@SERVICE_MANAGER\nsystemd\n@@DOCKER_STATUS\nready\n@@DOCKER_INFO\n{\"ID\":\"fixture-engine\"}\n@@DOCKER_SOCKET\n/var/run/docker.sock\n")
			return 0
		}
		if command == monitor.DockerStatsCommand {
			return 0
		}
		if strings.Contains(command, "@@MANAGEMENT_OK") && strings.Contains(command, "compose_local stop") {
			script, _, _ := strings.Cut(command, "\nmanagement_result=$?")
			if code := f.Stream(sessionCtx, script, in, out, stderr); code != 0 {
				return code
			}
			io.WriteString(out, "\n@@MANAGEMENT_OK\n@@SERVICE_MANAGER\nsystemd\n@@DOCKER_STATUS\nready\n@@DOCKER_INFO\n{\"ID\":\"fixture-engine\"}\n@@DOCKER_SOCKET\n/var/run/docker.sock\n")
			return 0
		}
		if mode, _ := os.ReadFile(f.State); string(mode) == "hold-pull" && strings.Contains(command, "compose_local pull") {
			io.WriteString(out, "Downloading test image…\n")
			<-sessionCtx.Done()
			return 1
		}
		if strings.Contains(command, "container logs") {
			io.WriteString(out, "\x1b[32m2026-10-21T10:00:00Z fixture log\x1b[0m\n")
			io.WriteString(stderr, "fixture stderr log\n")
			<-sessionCtx.Done()
			return 0
		}
		return f.Stream(sessionCtx, command, in, out, stderr)
	})
	dir, err := os.MkdirTemp("", "dui-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(dir) })
	pass := []byte("Docker UI isolated test password")
	v, err := vault.New(pass)
	if err != nil {
		t.Fatal(err)
	}
	v.Put(ssh.Profile())
	if err = vault.Save(broker.VaultPath(dir), v, true); err != nil {
		t.Fatal(err)
	}
	v.Close()
	server, err := broker.Open(dir, pass)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(server.Close)
	updates := make(chan func(), 256)
	a := managementApp()
	a.ctx, a.dir, a.server, a.client = ctx, dir, server, client.Client{Dir: dir}
	a.dispatch = func(fn func()) { updates <- fn }
	a.dock, a.diagnosticTab = toolDiagnostics, 4
	p := ssh.Profile()
	a.sessions[0].target = approval.Operation{Endpoint: fmt.Sprintf("%s@%s:%d", p.User, p.Host, p.Port), HostFingerprint: p.Fingerprint}
	d := a.sessions[0].stats.Inventory.Docker
	row := &d.Containers[0]
	row.Project, row.Service, row.ComposeDir, row.ComposeFiles = "sample", "web", f.Dir, f.Target.Files
	t.Cleanup(func() {
		a.cancelDockerWork()
		for _, s := range a.sessions {
			if s.term != nil {
				s.term.Close()
			}
		}
	})
	return a, f, updates
}

func pumpDocker(t *testing.T, updates <-chan func(), done func() bool) {
	t.Helper()
	timer := time.NewTimer(20 * time.Second)
	defer timer.Stop()
	for !done() {
		select {
		case fn := <-updates:
			fn()
		case <-timer.C:
			t.Fatal("Docker UI operation did not finish")
		}
	}
}

func TestDockerLogMenuSearchPauseAndCloseCancelOnlyOriginalStream(t *testing.T) {
	a, _, updates := dockerUIFixture(t)
	tt := ui.NewTester(a.view, 1150, 860)
	terminalBefore, _ := tt.Find("终端区域")
	if err := tt.Click("容器操作 web"); err != nil {
		t.Fatal(err)
	}
	if err := tt.ChooseMenuItem("日志与详情"); err != nil {
		t.Fatal(err)
	}
	s := a.sessions[0].management.containerDetails
	if s == nil {
		t.Fatal("no container details")
	}
	pumpDocker(t, updates, func() bool { return s.log.buffer.Version() > 0 || s.error != "" })
	tt.Frame()
	if s.error != "" || !tt.HasText("fixture stderr log") {
		t.Fatal(s.error, tt.Texts())
	}
	if err := tt.Click("搜索日志"); err != nil {
		t.Fatal(err)
	}
	tt.Type("stderr")
	if rows := logRows(s.log); len(rows) != 1 || rows[0] != "fixture stderr log" {
		t.Fatal(rows)
	}
	if err := tt.Click("复制筛选日志"); err != nil {
		t.Fatal(err)
	}
	ctx := s.ctx
	if err := tt.Click("暂停接收"); err != nil {
		t.Fatal(err)
	}
	if ctx.Err() == nil || s.streaming {
		t.Fatal("pause did not cancel log receiver")
	}
	a.active = 1
	if s.session != a.sessions[0] || s.target.ID != strings.Repeat("a", 64) {
		t.Fatal("log details followed a different server")
	}
	tt.Frame()
	if tt.HasText("容器日志详情") {
		t.Fatal("log panel leaked into another server")
	}
	a.active = 0
	tt.Frame()
	if err := tt.Click("继续接收"); err != nil {
		t.Fatal(err)
	}
	ctx = s.ctx
	if err := tt.Click("返回容器列表"); err != nil {
		t.Fatal(err)
	}
	if a.prompt.open || ctx.Err() == nil || a.sessions[0].management.containerDetails != nil {
		t.Fatal("details close retained receiver")
	}
	terminalAfter, _ := tt.Find("终端区域")
	if terminalBefore != terminalAfter {
		t.Fatal("log inspector resized the terminal", terminalBefore, terminalAfter)
	}
}

func TestComposeUIPlanBackgroundJobAndTaskDetails(t *testing.T) {
	a, f, updates := dockerUIFixture(t)
	tt := ui.NewTester(a.view, 1150, 860)
	if err := tt.Click("Compose"); err != nil {
		t.Fatal(err)
	}
	if err := tt.Click("Compose 项目 sample"); err != nil {
		t.Fatal(err)
	}
	if err := chooseComposeProjectMenu(tt, "sample", "更新项目…"); err != nil {
		t.Fatal(err)
	}
	if err := tt.Click("预览更新"); err != nil {
		t.Fatal(err)
	}
	s := a.prompt.docker
	a.active = 1
	pumpDocker(t, updates, func() bool { return !s.loading })
	tt.Frame()
	if s.plan == nil || s.error != "" || s.session != a.sessions[0] || s.plan.Images[0] != "example/web:1" {
		t.Fatal(s.error, s.plan)
	}
	if err := tt.Click("开始更新"); err != nil {
		t.Fatal(err)
	}
	if a.prompt.open || len(a.dockerJobs) != 1 || a.transfersOpen || a.active != 1 || a.dock != toolDiagnostics {
		t.Fatal("update hijacked current workspace")
	}
	job := a.dockerJobs[0]
	pumpDocker(t, updates, func() bool { return job.status != "running" })
	tt.Frame()
	if job.status != "done" || job.host != "fixture" || a.sessions[1].management != nil && (a.sessions[1].management.busy || a.sessions[1].management.message != "") {
		t.Fatal(job.status, job.error)
	}
	calls, _ := os.ReadFile(f.Calls)
	if !strings.Contains(string(calls), "up up") {
		t.Fatal("UI did not reach deployment", string(calls))
	}
	if err := tt.Click("任务中心"); err != nil {
		t.Fatal(err)
	}
	if err := tt.Click("任务详情 更新 sample"); err != nil {
		t.Fatal(err)
	}
	if !a.prompt.open || a.prompt.docker.job != job || !tt.HasText("更新完成") {
		t.Fatal("task results not accessible")
	}
	a.lock()
	tt.Frame()
	if len(a.dockerJobs) != 0 || a.prompt.open {
		t.Fatal("locked app retained task data")
	}
}

func TestComposeLifecycleUIKeepsReviewedServerAndReportsFailure(t *testing.T) {
	for _, fail := range []bool{false, true} {
		t.Run(fmt.Sprint(fail), func(t *testing.T) {
			a, f, updates := dockerUIFixture(t)
			if fail {
				os.WriteFile(f.State, []byte("lifecycle-fail"), 0600)
			}
			tt := ui.NewTester(a.view, 1280, 1000)
			tt.Click("Compose")
			tt.Click("Compose 项目 sample")
			if err := chooseComposeProjectMenu(tt, "sample", "停止项目…"); err != nil {
				t.Fatal(err)
			}
			source := a.sessions[0]
			a.active = 1
			tt.Frame()
			if err := tt.Click("停止项目"); err != nil {
				t.Fatal(err)
			}
			pumpDocker(t, updates, func() bool { return !source.management.busy })
			calls, _ := os.ReadFile(f.Calls)
			if !strings.Contains(string(calls), "stop stop --timeout 10 web") || a.active != 1 {
				t.Fatal("reviewed operation was redirected or changed", string(calls), source.management.message)
			}
			if fail && strings.Contains(source.management.message, "已执行") || !fail && !strings.Contains(source.management.message, "已执行") {
				t.Fatal("operation result was misreported", source.management.message)
			}
			if a.sessions[1].management != nil && a.sessions[1].management.message != "" {
				t.Fatal("another server received the operation result")
			}
		})
	}
}

func TestContainerUITerminalUsesPTYAndDoesNotBecomeHostReconnect(t *testing.T) {
	a, f, updates := dockerUIFixture(t)
	source := a.sessions[0]
	a.openContainerTerminal(source, f.Target.Target, "web", "sh", nil)
	pumpDocker(t, updates, func() bool { return len(a.sessions) > 2 || strings.Contains(a.status, "失败") })
	if len(a.sessions) != 3 {
		t.Fatal(a.status)
	}
	s := a.sessions[2]
	if s.target.Kind != "docker-terminal" || s.target.Docker.Target.ID != f.Target.Target.ID || s.name != "容器 · web" {
		t.Fatal(s.target, s.name)
	}
	s.term.Send([]byte("容器中文测试\n"))
	deadline := time.NewTimer(10 * time.Second)
	defer deadline.Stop()
	for !strings.Contains(s.term.Text(), "容器中文测试") {
		select {
		case fn := <-updates:
			fn()
		case <-time.After(10 * time.Millisecond):
		case <-deadline.C:
			t.Fatal(s.term.Text())
		}
	}
	s.term.Resize(100, 30)
	fileHeader := ui.NewTester(func(c *ui.Context) { a.fileLocationHeader(c, newFilePane("fixture", true)) }, 300, 40)
	if _, ok := fileHeader.Find("容器宿主机文件来源"); !ok || !fileHeader.HasText("fixture") {
		t.Fatal("container tab disguised host SFTP as container files")
	}
	a.closeSessionAt(a.sessionIndex(s))
	if a.hasSession(s) {
		t.Fatal("container tab remained")
	}
}

func TestComposeSourceMismatchCannotBeClearedBySelection(t *testing.T) {
	a := managementApp()
	d := a.sessions[0].stats.Inventory.Docker
	rows := []monitor.Container{{ID: strings.Repeat("a", 64), Project: "sample", Service: "a", State: "running", ComposeDir: "/srv/a", ComposeFiles: "/srv/a/c.yml"}, {ID: strings.Repeat("b", 64), Project: "sample", Service: "b", State: "running", ComposeDir: "/srv/b", ComposeFiles: "/srv/b/c.yml"}}
	a.openComposeUpdate(rows, d)
	s := a.prompt.docker
	s.services[1].Selected = false
	s.error = ""
	a.loadComposePlan(s)
	if s.loading || s.sourceError == "" || s.plan != nil {
		t.Fatal("mixed sources became actionable")
	}
}

func TestDockerDialogDockMigrationAndLockReleaseLogReceiver(t *testing.T) {
	a := managementApp()
	a.dock, a.diagnosticTab = toolDiagnostics, 4
	windows := fakeToolsFactory(a)
	a.detachTools()
	a.renderingToolsWindow = true
	d := a.sessions[0].stats.Inventory.Docker
	a.openContainerDetails(d.Containers[0], d)
	a.renderingToolsWindow = false
	s := a.sessions[0].management.containerDetails
	s.log.buffer.Write([]byte("retained line\n"))
	s.log.query = "retained"
	main := ui.NewTester(a.view, 1100, 800)
	tools := ui.NewTester(a.detachedToolsView, 960, 720)
	if main.HasText("容器日志详情") || !tools.HasText("retained line") {
		t.Fatal("container details have wrong window owner")
	}
	a.setToolsDock(false)
	(*windows)[0].finishPendingUpdates()
	main.Frame()
	if a.sessions[0].management.containerDetails != s || a.prompt.open || !main.HasText("retained line") || s.log.query != "retained" || s.ctx.Err() != nil || (*windows)[0].shown != 1 {
		t.Fatal("docking lost details or raised a window")
	}
	a.lock()
	main.Frame()
	if s.ctx.Err() == nil || a.prompt.open || main.HasText("retained line") {
		t.Fatal("lock retained logs or receiver")
	}
}

func TestComposeChangingSelectedServicesInvalidatesPlan(t *testing.T) {
	a := managementApp()
	d := a.sessions[0].stats.Inventory.Docker
	row := d.Containers[0]
	row.Project, row.Service, row.ComposeDir, row.ComposeFiles = "sample", "web", "/srv/sample", "/srv/sample/compose.yaml"
	a.openComposeUpdate([]monitor.Container{row}, d)
	s := a.prompt.docker
	s.compose = s.composeSelection()
	s.plan = &serverops.ComposePlan{Hash: strings.Repeat("a", 64), Images: []string{"example/web:1"}}
	tt := ui.NewTester(a.view, 1100, 800)
	if err := tt.Click("更新服务 web"); err != nil {
		t.Fatal(err)
	}
	if s.plan != nil {
		t.Fatal("changed selection retained reviewed plan")
	}
}

func TestComposeCancelWaitingDoesNotReportSuccessOrStartRebuild(t *testing.T) {
	a, f, updates := dockerUIFixture(t)
	if err := os.WriteFile(f.State, []byte("hold-pull"), 0600); err != nil {
		t.Fatal(err)
	}
	d := a.sessions[0].stats.Inventory.Docker
	a.openComposeUpdate(d.Containers[:1], d)
	s := a.prompt.docker
	a.loadComposePlan(s)
	pumpDocker(t, updates, func() bool { return !s.loading })
	if s.plan == nil {
		t.Fatal(s.error)
	}
	a.startComposeJob(s)
	if len(a.dockerJobs) != 1 {
		t.Fatal("job did not start")
	}
	job := a.dockerJobs[0]
	pumpDocker(t, updates, func() bool { return job.phase == "pull" || job.status != "running" })
	a.openDockerJob(job)
	tt := ui.NewTester(a.view, 1100, 800)
	if err := tt.Click("停止等待"); err != nil {
		t.Fatal(err)
	}
	pumpDocker(t, updates, func() bool { return job.status != "running" })
	if job.status != "error" || !strings.Contains(job.error, "实际状态") {
		t.Fatal(job.status, job.error)
	}
	calls, _ := os.ReadFile(f.Calls)
	if strings.Contains(string(calls), "up up") {
		t.Fatal("cancelled download reached rebuild", string(calls))
	}
}
