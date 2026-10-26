//go:build preview_qa && terminal_integration

package desktop

import (
	"image/png"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/egoist/mygo/plugins/terminal"
	"github.com/egoist/mygo/ui"
	"sshclient/internal/approval"
	"sshclient/internal/dockerlog"
	"sshclient/internal/serverops"
)

func dockerWorkflowDemo() *app {
	a := managementApp()
	a.dock, a.diagnosticTab = toolDiagnostics, 4
	a.sidebarWidth = 180
	d := a.sessions[0].stats.Inventory.Docker
	d.Containers[0].Names = "sub-store"
	d.Containers[0].Project = "subscriptions"
	d.Containers[0].Service = "web"
	d.Containers[0].Image = "example/sub-store:stable"
	d.Containers[0].ComposeDir = "/srv/subscriptions"
	d.Containers[0].ComposeFiles = "/srv/subscriptions/compose.yaml"
	return a
}

func TestDockerWorkflowSnapshotsWithoutWindows(t *testing.T) {
	if os.Getenv("SSH_CLIENT_PREVIEW_QA") != "1" {
		t.Skip("opt-in offline preview")
	}
	dir := os.Getenv("SSH_CLIENT_UI_SNAPSHOT_DIR")
	if !filepath.IsAbs(dir) {
		t.Fatal("absolute output path required")
	}
	if err := os.MkdirAll(dir, 0700); err != nil {
		t.Fatal(err)
	}
	for _, variant := range []string{"logs", "logs-dark", "logs-small", "logs-right", "compose", "compose-small", "compose-changed", "task", "task-error", "task-center", "terminal"} {
		t.Run(variant, func(t *testing.T) {
			a := dockerWorkflowDemo()
			d := a.sessions[0].stats.Inventory.Docker
			view, width, height := a.view, 1150, 820
			if strings.HasSuffix(variant, "small") {
				a.toolsDetached = true
				a.renderingToolsWindow = true
				view, width, height = a.detachedToolsView, 640, 480
			}
			if variant == "logs-right" {
				view, width, height = a.fileArea, 420, 650
			}
			switch {
			case strings.HasPrefix(variant, "logs"):
				a.openContainerDetails(d.Containers[0], d)
				defer a.cancelDockerWork()
				a.terminalShare = 52
				if !strings.HasSuffix(variant, "small") {
					term, err := terminal.New(terminal.Options{Conn: newTerminalRecorder(), Theme: terminal.DarkTheme(), ClipboardRead: terminal.ClipboardDeny, NoClipboardWrite: true})
					if err != nil {
						t.Fatal(err)
					}
					defer term.Close()
					a.sessions[0].term = term
					term.Feed([]byte("root@demo:~# uptime\r\n 12:30:00 up 7 days, 1 user, load average: 0.10, 0.08, 0.03\r\nroot@demo:~# "))
				}
				a.sessions[0].management.containerDetails.log.buffer.Write([]byte("2026-10-21T10:00:00Z INFO  Sub-Store is listening on :3001\n2026-10-21T10:00:01Z INFO  Loaded 12 subscriptions\n2026-10-21T10:00:04Z INFO  GET /api/subscriptions 200  8ms\n2026-10-21T10:00:06Z WARN  Upstream request took 1.2s\n2026-10-21T10:00:07Z INFO  Sync completed\n"))
			case strings.HasPrefix(variant, "compose"):
				a.openComposeUpdate(d.Containers[:1], d)
				s := a.prompt.docker
				s.compose = s.composeSelection()
				s.plan = &serverops.ComposePlan{Hash: strings.Repeat("a", 64), Services: []string{"web"}, Images: []string{"example/sub-store:stable"}, Version: "2.39.0"}
				if variant == "compose-changed" {
					s.plan.Images = []string{"example/sub-store:next"}
				}
			case strings.HasPrefix(variant, "task"):
				job := &dockerJob{title: "更新 subscriptions", host: "fixture", endpoint: "root@192.0.2.1:22", phase: "pull", status: "running", started: time.Now(), log: &dockerLogView{buffer: &dockerlog.Buffer{}, follow: true}}
				job.log.buffer.Write([]byte("Pulling example/sub-store:stable\n[1/3] Downloading image layers…\n[2/3] Verifying checksum…\n"))
				a.dockerJobs = []*dockerJob{job}
				a.activityTab = 1
				if variant == "task-error" {
					job.status = "error"
					job.error = "已重建，状态核对未通过：web 尚未就绪"
					job.result = []serverops.ComposeStatus{{Service: "web", State: "running", Health: "unhealthy"}}
				}
				if variant != "task-center" {
					a.openDockerJob(job)
				}
			case variant == "terminal":
				term, err := terminal.New(terminal.Options{Conn: newTerminalRecorder(), Theme: terminal.DarkTheme(), ClipboardRead: terminal.ClipboardDeny, NoClipboardWrite: true})
				if err != nil {
					t.Fatal(err)
				}
				defer term.Close()
				a.sessions[0].term = term
				a.sessions[0].name = "容器 · sub-store"
				a.sessions[0].target = approval.Operation{Kind: "docker-terminal", Docker: serverops.DockerRequest{Target: serverops.DockerTarget{Socket: d.Socket, Engine: d.EngineID, ID: d.Containers[0].ID}, Shell: "sh"}}
				term.Feed([]byte("/app # uname -a\r\nLinux container 6.12 x86_64 GNU/Linux\r\n/app # ls\r\nconfig  data  node_modules  package.json\r\n/app # "))
			}
			a.renderingToolsWindow = false
			tt := ui.NewTester(view, width, height)
			tt.SetDark(variant == "logs-dark")
			if variant == "task-center" {
				if err := tt.Click("任务中心"); err != nil {
					t.Fatal(err)
				}
			}
			labels := []string{}
			if strings.HasPrefix(variant, "logs") {
				labels = []string{"容器日志详情", "打开容器终端", "返回容器列表", "搜索日志", "日志行数", "暂停接收", "刷新日志", "复制筛选日志", "跟随最新", "日志内容"}
			}
			if strings.HasPrefix(variant, "compose") {
				labels = []string{"更新 Compose对话框", "预览更新", "开始更新", "取消"}
			}
			if strings.HasPrefix(variant, "task") && variant != "task-center" {
				labels = []string{"更新 subscriptions对话框", "关闭"}
			}
			if variant == "task-center" {
				labels = []string{"任务中心面板", "任务详情 更新 subscriptions"}
			}
			for _, label := range labels {
				r, ok := tt.Find(label)
				if !ok || r.X < 0 || r.Y < 0 || r.X+r.W > float32(width)+1 || r.Y+r.H > float32(height)+1 {
					t.Fatal("Docker control clipped", variant, label, r)
				}
			}
			f, err := os.Create(filepath.Join(dir, "docker-work-"+variant+".png"))
			if err != nil {
				t.Fatal(err)
			}
			err = png.Encode(f, tt.Image())
			closeErr := f.Close()
			if err != nil || closeErr != nil {
				t.Fatal(err, closeErr)
			}
		})
	}
}
