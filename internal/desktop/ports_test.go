package desktop

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"
	"sshclient/internal/monitor"
)

func portsApp() *app {
	a := managementApp()
	a.diagnosticTab = 5
	a.sessions[0].stats.PortSnapshot = &monitor.PortSnapshot{
		Time: time.Date(2026, 11, 6, 4, 5, 6, 0, time.UTC), Status: "partial", Source: "ss", ProcessLimited: true, Truncated: true,
		Ports: []monitor.Port{
			{Protocol: "TCP", State: "LISTEN", LocalAddress: "0.0.0.0", LocalPort: "443", PeerAddress: "*", PeerPort: "*", Process: "nginx", PID: 445},
			{Protocol: "UDP", State: "UNCONN", LocalAddress: "0.0.0.0", LocalPort: "53", PeerAddress: "*", PeerPort: "*"},
			{Protocol: "TCP", State: "LISTEN", LocalAddress: "::1", LocalPort: "5432", PeerAddress: "::", PeerPort: "*", Process: "postgres", PID: 300},
			{Protocol: "TCP", State: "ESTAB", LocalAddress: "192.0.2.10", LocalPort: "22", PeerAddress: "198.51.100.24", PeerPort: "56342", Process: "remote\u202eserver", PID: 812},
			{Protocol: "UDP", State: "ESTAB", LocalAddress: "192.0.2.10", LocalPort: "45000", PeerAddress: "10.0.0.2", PeerPort: "53", Process: "dns-client", PID: 900},
			{Protocol: "TCP", State: "ESTAB", LocalAddress: "127.0.0.1", LocalPort: "1234", PeerAddress: "127.0.0.2", PeerPort: "4321", Process: "loopback-client", PID: 901},
		},
	}
	return a
}

func TestPortsTabReadOnlyScopesSearchAndSnapshotTime(t *testing.T) {
	a := portsApp()
	a.online = false
	a.sessions[0].target.Endpoint = ""
	tt := ui.NewTester(a.diagnosticsView, 1150, 600)
	if !tt.HasText("nginx") || !tt.HasText("0.0.0.0:53") || tt.HasText("postgres") || tt.HasText("dns-client") {
		t.Fatal("non-loopback listener scope mixed connected sockets", tt.Texts())
	}
	for _, label := range []string{"端口信息不完整", "部分进程信息不可见", "记录已截断", fmt.Sprintf("采样于 %s", a.sessions[0].stats.PortSnapshot.Time.Local().Format("15:04:05"))} {
		if !tt.HasText(label) {
			t.Fatal("missing snapshot status", label, tt.Texts())
		}
	}
	if _, ok := tt.Find("列表操作"); ok || a.statusActionsAvailable() {
		t.Fatal("port table exposed management actions")
	}
	if err := tt.Click("搜索端口"); err != nil {
		t.Fatal(err)
	}
	tt.Type("NGINX")
	if !tt.HasText("nginx") || tt.HasText("0.0.0.0:53") {
		t.Fatal("case-insensitive search did not filter", tt.Texts())
	}
	state := a.managementState()
	state.portScope, state.portSearch = portScopeLocal, ""
	tt.Frame()
	if !tt.HasText("postgres") || !tt.HasText("[::1]:5432") || tt.HasText("nginx") {
		t.Fatal("IPv6 loopback listener scope", tt.Texts())
	}
	state.portScope = portScopeConnections
	tt.Frame()
	if !tt.HasText("198.51.100.24:56342") || !tt.HasText("10.0.0.2:53") || tt.HasText("loopback-client") || tt.HasText("nginx") {
		t.Fatal("external peers lost private IP or mixed listeners/loopback", tt.Texts())
	}
	if !tt.HasText(`remote\u202eserver`) || strings.Contains(strings.Join(tt.Texts(), ""), "\u202e") {
		t.Fatal("untrusted process display was not escaped", tt.Texts())
	}
	if err := tt.Click("PID"); err != nil {
		t.Fatal(err)
	}
	if state.portSort.Column != "pid" {
		t.Fatal("port column did not update sorting", state.portSort)
	}
	a.diagnosticTab = 0
	tt.Frame()
	if err := tt.Click("进程"); err != nil {
		t.Fatal(err)
	}
	if a.diagnosticTab != 1 {
		t.Fatal("existing process tab index changed", a.diagnosticTab)
	}
	if err := tt.Click("端口"); err != nil {
		t.Fatal(err)
	}
	if a.diagnosticTab != 5 {
		t.Fatal("ports must append after the five existing tabs", a.diagnosticTab)
	}
}

func TestPortsTabUnavailableEmptyAndPartialRemainDistinct(t *testing.T) {
	for _, tc := range []struct {
		name     string
		snapshot *monitor.PortSnapshot
		want     string
		table    bool
	}{
		{"legacy", nil, "尚未采样端口信息", false},
		{"missing", &monitor.PortSnapshot{Status: "unavailable", Note: "missing-tools"}, "未找到 ss 或 netstat", false},
		{"failed", &monitor.PortSnapshot{Status: "unavailable", Note: "probe-failed"}, "采样工具执行失败", false},
		{"incomplete", &monitor.PortSnapshot{Status: "unavailable", Note: "incomplete-output"}, "采样未完成", false},
		{"zero", &monitor.PortSnapshot{Status: "ready"}, "端口采样完成", true},
		{"partial", &monitor.PortSnapshot{Status: "partial", ProcessLimited: true, Ports: []monitor.Port{{Protocol: "TCP", State: "LISTEN", LocalAddress: "0.0.0.0", LocalPort: "22", PeerAddress: "*", PeerPort: "*"}}}, "部分进程信息不可见", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			a := portsApp()
			a.sessions[0].stats.PortSnapshot = tc.snapshot
			tt := ui.NewTester(a.diagnosticsView, 1000, 450)
			if !tt.HasText(tc.want) {
				t.Fatal("snapshot state disappeared", tc.want, tt.Texts())
			}
			if _, ok := tt.Find("端口列表"); ok != tc.table {
				t.Fatal("unavailable data became an empty socket list", tc.name, ok)
			}
			if tc.name == "zero" && !tt.HasText("当前范围没有匹配记录") {
				t.Fatal("successful empty sample was not explicit", tt.Texts())
			}
			if tc.name == "partial" && !tt.HasText("0.0.0.0:22") {
				t.Fatal("missing process permission hid the socket", tt.Texts())
			}
		})
	}
}

func TestPortsNumericSortSharedOwnerSearchAndSessionState(t *testing.T) {
	ports := []monitor.Port{
		{Protocol: "TCP", State: "LISTEN", LocalAddress: "0.0.0.0", LocalPort: "100", Owners: []monitor.PortOwner{{Process: "web", PID: 10}, {Process: "web-worker", PID: 11}}},
		{Protocol: "TCP", State: "LISTEN", LocalAddress: "0.0.0.0", LocalPort: "22", PID: 20},
	}
	sortPorts(ports, ui.SortOrder{Column: "local"})
	if ports[0].LocalPort != "22" {
		t.Fatal("port numbers sorted lexically", ports)
	}
	if got := visiblePorts(ports, portScopeListeners, "web-worker"); len(got) != 1 || got[0].LocalPort != "100" {
		t.Fatal("search ignored shared owners", got)
	}
	process, pid := portOwnerText(ports[1])
	if process != "web, web-worker" || pid != "10, 11" {
		t.Fatal("shared owners were not displayed", process, pid)
	}
	a := portsApp()
	a.managementState().portSearch = "first-session"
	a.active = 1
	if state := a.managementState(); state.portSearch != "" || state.portScope != "" {
		t.Fatal("port filter state leaked between sessions", state)
	}
}
