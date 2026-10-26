package desktop

import (
	"context"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"
	"sshclient/internal/monitor"
)

func TestServerStateFollowsExactSessionWithoutManualSampling(t *testing.T) {
	first := &session{host: "alpha", name: "Alpha first", stats: &monitor.Stats{Time: time.Now(), CPU: 11}, monitorStatus: "自动更新"}
	second := &session{host: "beta", name: "Beta", stats: &monitor.Stats{Time: time.Now(), CPU: 22}, monitorStatus: "自动更新"}
	third := &session{host: "alpha", name: "Alpha second", stats: &monitor.Stats{Time: time.Now(), CPU: 33}, monitorStatus: "自动更新"}
	a := &app{ctx: context.Background(), online: true, page: 1, selected: "alpha", sessions: []*session{first, second, third}, groups: map[string]*bool{}}
	a.showTool(toolDiagnostics)
	tt := ui.NewTester(a.view, 1280, 900)
	if !tt.HasText("11.0%") || tt.HasText("开始采样") || tt.HasText("停止采样") {
		t.Fatal(tt.Texts())
	}
	for _, test := range []struct {
		label, cpu string
		index      int
	}{{"Beta", "22.0%", 1}, {"Alpha second", "33.0%", 2}} {
		if err := tt.Click(test.label); err != nil {
			t.Fatal(err)
		}
		if a.active != test.index || !tt.HasText(test.cpu) {
			t.Fatal("status followed another session", a.active, tt.Texts())
		}
	}
	a.switchHost("not-connected")
	tt.Frame()
	if !tt.HasText("33.0%") || a.serverStateSession() != third {
		t.Fatal("sidebar selection replaced the active terminal status", tt.Texts())
	}
	a.switchHost("alpha")
	a.active = 2
	third.closed, third.monitorStatus = true, "已断开，保留最后状态"
	tt.Frame()
	if !tt.HasText("33.0%") {
		t.Fatal("last state disappeared after disconnect", tt.Texts())
	}
	a.closeSession()
	tt.Frame()
	if a.serverStateSession() != second {
		t.Fatal("closing a tab did not select remaining server state")
	}
	a.closeSession()
	a.closeSession()
	tt.Frame()
	if a.serverStateSession() != nil || !tt.HasText("连接服务器后自动显示状态") {
		t.Fatal("closed tabs left server status behind", tt.Texts())
	}
}
