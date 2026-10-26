package desktop

import (
	"context"
	"github.com/egoist/mygo/ui"
	"github.com/egoist/mygo/ui/editor"
	"sshclient/internal/approval"
	"sshclient/internal/monitor"
	"sshclient/internal/transfer"
	"sshclient/internal/vault"
	"testing"
	"time"
)

func TestNativeViewsRenderAndAuthorizationNeverAutoApproves(t *testing.T) {
	a := &app{ctx: context.Background(), create: true, port: "22", edit: &editor.State{Buffer: editor.NewBuffer("")}, form: vault.Profile{Auth: "password"}}
	tt := ui.NewTester(a.view, 1180, 800)
	if !tt.HasText("创建本机保险库") {
		t.Fatal(tt.Texts())
	}
	a.online = true
	a.requests = []approval.Request{{ID: "test", Status: "pending", Operation: approval.Operation{Host: "fixture", Kind: "exec", Command: "uname -a", Client: "AI"}}}
	a.selectedRequest = "test"
	for page := 0; page < 4; page++ {
		a.page = page
		tt.Frame()
		if page == 3 && !tt.HasText("允许") {
			t.Fatal("missing human approval action", tt.Texts())
		}
	}
	if a.requests[0].Status != "pending" {
		t.Fatal("rendering auto-approved a request")
	}
}

func TestWorkspaceGroupsMonitorAndQueueRender(t *testing.T) {
	a := &app{ctx: context.Background(), online: true, page: 1, selected: "fixture", groups: map[string]*bool{}, sidebarWidth: 250, terminalShare: 72, port: "22", fileSelected: -1, edit: &editor.State{Buffer: editor.NewBuffer("")}, connections: []vault.Summary{{ID: "fixture", Name: "Test server", Host: "127.0.0.1", Group: "Production"}}, sessions: []*session{{host: "fixture", stats: &monitor.Stats{Time: time.Now(), CPU: 42, MemoryTotal: 1024, MemoryUsed: 512, Processes: []monitor.Process{{PID: 1, Name: "sshd", CPU: 3, Memory: 1}}, Disks: []monitor.Disk{{Mount: "/", Total: 1000, Used: 500}}}}}, jobs: []transfer.Job{{ID: "test", Host: "fixture", Direction: "download", Local: "/tmp/local", Remote: "/srv/file", Size: 100, Bytes: 50, State: "paused"}}}
	tt := ui.NewTester(a.view, 1180, 800)
	for _, s := range []string{"Production", "Test server", "目录路径", "文件窗口菜单", "工具面板布局"} {
		if !tt.HasText(s) {
			t.Fatal(s, tt.Texts())
		}
	}
	a.showTool(toolTransfers)
	tt.Frame()
	if !tt.HasText("继续 / 重试") {
		t.Fatal(tt.Texts())
	}
	a.transfersOpen = false
	a.showTool(toolDiagnostics)
	a.diagnosticTab = 1
	tt.Frame()
	if !tt.HasText("sshd") || !tt.HasText("PID") || !tt.HasText("CPU %") {
		t.Fatal(tt.Texts())
	}
	a.page = 1
	a.search = "no-match"
	tt.Frame()
	for _, text := range tt.Texts() {
		if text == "Test server" {
			t.Fatal("search ignored")
		}
	}
}
