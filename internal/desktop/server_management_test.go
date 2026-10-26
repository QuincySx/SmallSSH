package desktop

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"
	"sshclient/internal/approval"
	"sshclient/internal/broker"
	"sshclient/internal/client"
	"sshclient/internal/localipc"
	"sshclient/internal/monitor"
	"sshclient/internal/testserver"
	"sshclient/internal/vault"
)

func managementApp() *app {
	info := &monitor.Inventory{Time: time.Now(), Manager: "systemd", Services: []monitor.Service{{Name: "nginx.service", Enabled: "enabled", Active: "active", Sub: "running"}, {Name: "worker.service", Enabled: "disabled", Active: "inactive"}}, Docker: &monitor.DockerInfo{Status: "ready", Version: "28.0", EngineID: "fixture-engine", Socket: "/var/run/docker.sock", Containers: []monitor.Container{{ID: strings.Repeat("a", 64), Names: "web", Image: "nginx:latest", State: "running", Status: "Up 1 hour", Ports: "8080->80/tcp"}, {ID: strings.Repeat("b", 64), Names: "stopped", State: "exited", Status: "Exited"}}, Images: []monitor.Image{{ID: "sha256:a", Repository: "large", Tag: "latest", Size: "1GB", CreatedAt: "2026-10-19 00:00:00 +0000 UTC"}, {ID: "sha256:b", Repository: "small", Tag: "latest", Size: "20MB", CreatedAt: "2026-10-20 00:00:00 +0000 UTC"}}}}
	stats := &monitor.Stats{Time: time.Now(), BootID: "00000000-0000-0000-0000-000000000000", Processes: []monitor.Process{{PID: 42, StartTicks: 1234, Name: "worker", CPU: 90}, {PID: 43, StartTicks: 4567, Name: "other", CPU: 10}}, Inventory: info}
	return &app{online: true, ctx: context.Background(), page: 1, selected: "fixture", sessions: []*session{{host: "fixture", name: "Fixture", stats: stats, target: approval.Operation{Endpoint: "fixture@127.0.0.1:22", HostFingerprint: "SHA256:fixture"}}, {host: "second", name: "Second", stats: &monitor.Stats{Time: time.Now(), Inventory: info}, target: approval.Operation{Endpoint: "fixture@127.0.0.2:22", HostFingerprint: "SHA256:second"}}}}
}

func TestManagementMenusUseStableSelectionAndExplicitConfirm(t *testing.T) {
	a := managementApp()
	a.diagnosticTab = 1
	tt := ui.NewTester(func(c *ui.Context) { a.diagnosticsView(c); a.promptView(c) }, 1100, 550)
	if err := tt.Click("worker"); err != nil {
		t.Fatal(err)
	}
	// Reordering must keep the selected lifetime, rather than its old row index.
	a.sessions[0].stats.Processes[0].CPU = 1
	tt.Frame()
	if err := tt.Click("列表操作"); err != nil {
		t.Fatal(err)
	}
	if err := tt.ChooseMenuItem("结束进程…"); err != nil {
		t.Fatal(err)
	}
	if !a.prompt.open || !strings.Contains(a.prompt.target, "PID 42") || !a.prompt.danger {
		t.Fatal(a.prompt)
	}
	if err := tt.Click("取消"); err != nil {
		t.Fatal(err)
	}
	if a.prompt.open || a.sessions[0].management.busy {
		t.Fatal("cancel invoked an action")
	}
	a.active = 1
	tt.Frame()
	if a.managementState().processChoice.Len() != 0 {
		t.Fatal("selection leaked into another session")
	}
	a.active = 0
	a.sessions[0].stats.Processes[0].StartTicks = 0
	tt.Frame()
	if err := tt.Click("worker"); err != nil {
		t.Fatal(err)
	}
	if err := tt.Click("列表操作"); err != nil {
		t.Fatal(err)
	}
	if err := tt.ChooseMenuItem("强制结束…"); err == nil {
		t.Fatal("missing identity allowed kill")
	}
}

func TestServiceAndDockerMenusAndNumericImageSort(t *testing.T) {
	a := managementApp()
	a.diagnosticTab = 2
	tt := ui.NewTester(func(c *ui.Context) { a.diagnosticsView(c); a.promptView(c) }, 1100, 600)
	if !tt.HasText("worker.service") || !tt.HasText("已关闭") {
		t.Fatal("disabled service disappeared", tt.Texts())
	}
	if err := tt.Click("nginx.service"); err != nil {
		t.Fatal(err)
	}
	if err := tt.Click("列表操作"); err != nil {
		t.Fatal(err)
	}
	if err := tt.ChooseMenuItem("关闭自启…"); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(a.prompt.message, "当前运行的服务继续运行") {
		t.Fatal(a.prompt)
	}
	tt.Key(0, ui.KeyEscape)
	a.diagnosticTab = 4
	tt.Frame()
	if !tt.HasText("web") || !tt.HasText("nginx:latest") {
		t.Fatal(tt.Texts())
	}
	if err := tt.Click("web"); err != nil {
		t.Fatal(err)
	}
	if err := tt.Click("容器操作 web"); err != nil {
		t.Fatal(err)
	}
	if err := tt.ChooseMenuItem("停止容器…"); err != nil {
		t.Fatal(err)
	}
	if !a.prompt.open || !strings.Contains(a.prompt.target, "aaaaaaaaaaaa") || !strings.Contains(a.prompt.message, "10 秒") {
		t.Fatal(a.prompt)
	}
	tt.Key(0, ui.KeyEscape)
	if err := tt.Click("镜像"); err != nil {
		t.Fatal(err)
	}
	if err := tt.Click("大小"); err != nil {
		t.Fatal(err)
	}
	assertStatusRowOrder(t, tt, "small", "large")
	if err := tt.Click("创建时间"); err != nil {
		t.Fatal(err)
	}
	assertStatusRowOrder(t, tt, "large", "small")
}

func TestDockerUnavailableStatesRemainExplicitAndInert(t *testing.T) {
	for _, status := range []string{"missing", "unavailable"} {
		a := managementApp()
		a.diagnosticTab = 4
		a.sessions[0].stats.Inventory.Docker = &monitor.DockerInfo{Status: status, Note: "fixture"}
		tt := ui.NewTester(a.diagnosticsView, 900, 450)
		want := "未安装 Docker"
		if status == "unavailable" {
			want = "无法连接本机 Docker"
		}
		if !tt.HasText(want) {
			t.Fatal(tt.Texts())
		}
		if _, ok := tt.Find("列表操作"); ok {
			t.Fatal("unavailable Docker exposed actions")
		}
	}
}

func TestNewInventorySurvivesOlderMonitorDelivery(t *testing.T) {
	a := managementApp()
	s := a.sessions[0]
	updates := make(chan func(), 2)
	a.dispatch = func(fn func()) { updates <- fn }
	fresh := *s.stats.Inventory
	fresh.Time = time.Now().Add(time.Minute)
	s.stats.Inventory = &fresh
	a.updateServerStatus(s, a.generation, &monitor.Stats{CPU: 22, Inventory: &monitor.Inventory{Time: time.Now().Add(-time.Minute)}}, "更新")
	(<-updates)()
	if s.stats.Inventory != &fresh || s.stats.CPU != 22 {
		t.Fatal("late inventory undid completed action")
	}
}

// The actual vault, owner proof, broker and SSH protocol execute a generated
// command. systemctl/docker are isolated fixtures, never the host binaries.
func TestManagementThroughBrokerSSHAndTabSwitchDoesNotApproveExternal(t *testing.T) {
	dir, err := os.MkdirTemp("", "manage-")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(dir)
	if err := localipc.Prepare(dir); err != nil {
		t.Fatal(err)
	}
	bin := filepath.Join(dir, "bin")
	if err := os.Mkdir(bin, 0700); err != nil {
		t.Fatal(err)
	}
	stateFile := filepath.Join(dir, "enabled")
	if err := os.WriteFile(stateFile, []byte("enabled"), 0600); err != nil {
		t.Fatal(err)
	}
	script := `#!/bin/sh
while [ "$#" -gt 0 ]; do
 case "$1" in
  disable) printf disabled > "$TEST_SERVICE_STATE"; exit 0;;
  list-unit-files) printf 'nginx.service ';cat "$TEST_SERVICE_STATE";printf '\n';exit 0;;
  list-units) printf 'nginx.service loaded active running Fixture web\n';exit 0;;
 esac
 shift
done
exit 1
`
	if err := os.WriteFile(filepath.Join(bin, "systemctl"), []byte(script), 0700); err != nil {
		t.Fatal(err)
	}
	containerStateFile := filepath.Join(dir, "container-state")
	if err := os.WriteFile(containerStateFile, []byte("running"), 0600); err != nil {
		t.Fatal(err)
	}
	dockerScript := `#!/bin/sh
if [ "$1" = --version ]; then printf 'Docker fixture\n';exit 0;fi
[ "$1" = --host ] && [ "$2" = unix:///var/run/docker.sock ] || exit 80
[ -z "$DOCKER_HOST$DOCKER_CONTEXT$DOCKER_TLS_VERIFY$DOCKER_CERT_PATH" ] || exit 81
shift 2
case "$1 $2" in
 "info --format")
  if [ "$3" = '{{.ID}}' ];then printf 'fixture-engine\n';else printf '{"ID":"fixture-engine","Version":"28.0"}\n';fi;;
 "container ls") printf '{"ID":"@ID@","Names":"web","Image":"nginx:latest","State":"%s"}\n' "$(cat "$TEST_CONTAINER_STATE")";;
 "container stop") [ "$3" = -t ] && [ "$4" = 10 ] && [ "$5" = '@ID@' ] || exit 82;printf exited > "$TEST_CONTAINER_STATE";;
 "image ls") printf '{"ID":"sha256:a","Repository":"nginx","Tag":"latest","Size":"20MB"}\n';;
 *) exit 83;;
esac
`
	dockerScript = strings.ReplaceAll(dockerScript, "@ID@", strings.Repeat("a", 64))
	if err := os.WriteFile(filepath.Join(bin, "docker"), []byte(dockerScript), 0700); err != nil {
		t.Fatal(err)
	}
	commands := make(chan string, 4)
	fixture := testserver.NewWithExec(t, func(command string) (string, string, int) {
		commands <- command
		cmd := exec.Command("/bin/sh", "-c", command)
		cmd.Env = append(os.Environ(), "PATH="+bin+":/usr/bin:/bin", "TEST_SERVICE_STATE="+stateFile, "TEST_CONTAINER_STATE="+containerStateFile)
		var out, stderr bytes.Buffer
		cmd.Stdout = &out
		cmd.Stderr = &stderr
		code := 0
		if err := cmd.Run(); err != nil {
			code = 1
		}
		return out.String(), stderr.String(), code
	})
	pass := []byte("management isolated vault password")
	v, err := vault.New(pass)
	if err != nil {
		t.Fatal(err)
	}
	if err := v.Put(fixture.Profile()); err != nil {
		t.Fatal(err)
	}
	if err := vault.Save(broker.VaultPath(dir), v, true); err != nil {
		t.Fatal(err)
	}
	v.Close()
	server, err := broker.Open(dir, pass)
	if err != nil {
		t.Fatal(err)
	}
	defer server.Close()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	updates := make(chan func(), 8)
	a := managementApp()
	a.ctx = ctx
	a.dir = dir
	a.client = client.Client{Dir: dir}
	a.server = server
	a.dispatch = func(fn func()) { updates <- fn }
	profile := fixture.Profile()
	a.sessions[0].target = approval.Operation{Endpoint: fmt.Sprintf("%s@%s:%d", profile.User, profile.Host, profile.Port), HostFingerprint: profile.Fingerprint}
	external, _, err := a.client.Request(ctx, approval.Operation{Host: profile.ID, Kind: "exec", Command: "unrelated", Client: "external test"}, time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	a.confirmService(a.sessions[0].stats.Inventory.Services[0], "disable")
	if !a.prompt.open {
		t.Fatal("confirmation missing")
	}
	a.active = 1
	confirm := a.prompt.confirm
	a.prompt = promptState{}
	confirm("")
	select {
	case fn := <-updates:
		fn()
	case <-time.After(15 * time.Second):
		t.Fatal("management command did not finish")
	}
	if !strings.Contains(a.sessions[0].management.message, "已执行") || a.sessions[1].management != nil {
		t.Fatal("result assigned to wrong tab", a.sessions[0].management)
	}
	data, _ := os.ReadFile(stateFile)
	if string(data) != "disabled" {
		t.Fatal("service fixture was not changed", string(data))
	}
	services := a.sessions[0].stats.Inventory.Services
	if len(services) != 1 || services[0].Enabled != "disabled" || services[0].Active != "active" {
		t.Fatal("disable also stopped or hid service", services)
	}
	command := <-commands
	if strings.Contains(command, "sudo ") || strings.Contains(command, " stop -- ") {
		t.Fatal("unreviewed extra mutation")
	}
	status, err := a.client.Call(ctx, broker.Message{Action: "status", ID: external.ID})
	if err != nil || status.Request == nil || status.Request.Status != "pending" {
		t.Fatal("management approved another caller", status, err)
	}
	if a.pass != "" || a.ownerPass != "" || a.approvalWin != nil {
		t.Fatal("owner management retained password or opened approval UI")
	}
	a.runServerAction(a.sessions[0], "启动服务", "systemctl --no-ask-password start -- 'nginx.service'\n")
	// Busy state suppresses a repeated submission before the result arrives.
	a.runServerAction(a.sessions[0], "启动服务", "systemctl --no-ask-password start -- 'nginx.service'\n")
	select {
	case fn := <-updates:
		fn()
	case <-time.After(15 * time.Second):
		t.Fatal("error result missing")
	}
	if a.sessions[0].management.busy || strings.Contains(a.sessions[0].management.message, "已执行") || !strings.Contains(a.sessions[0].management.message, "未完成") {
		t.Fatal("failed command reported success", a.sessions[0].management.message)
	}
	a.active = 0
	d := a.sessions[0].stats.Inventory.Docker
	if d == nil || d.Status != "ready" || len(d.Containers) != 1 {
		t.Fatal("Docker fixture did not reach UI", d)
	}
	a.confirmContainer(d.Containers[0], d, "stop")
	confirm = a.prompt.confirm
	a.prompt = promptState{}
	confirm("")
	select {
	case fn := <-updates:
		fn()
	case <-time.After(15 * time.Second):
		t.Fatal("Docker stop did not finish")
	}
	d = a.sessions[0].stats.Inventory.Docker
	if !strings.Contains(a.sessions[0].management.message, "已执行") || d.Containers[0].State != "exited" {
		t.Fatal("Docker stop result missing", d, a.sessions[0].management.message)
	}
}
