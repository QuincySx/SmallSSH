package desktop

import (
	"fmt"
	"strings"
	"testing"

	"github.com/egoist/mygo/ui"
	"sshclient/internal/dockerlog"
	"sshclient/internal/monitor"
	"sshclient/internal/serverops"
)

func assessedStatusDemo() *app {
	a := responsiveWorkspaceDemo()
	s := a.sessions[0].stats
	s.Cores = append(s.Cores, monitor.CPUUsage{Name: "cpu1", Used: 7, Valid: true})
	s.Inventory.Cores = len(s.Cores)
	s.Inventory.CPU = "Intel Xeon Platinum Virtual Processor " + strings.Repeat("LongModelIdentifier", 8)
	s.Networks = append(s.Networks, monitor.Network{Name: "ens192", Valid: true, RX: 84 << 30, TX: 3 << 30})
	s.Inventory.Interfaces = append(s.Inventory.Interfaces, monitor.Interface{Name: "ens192", State: "up", Addresses: []string{"192.0.2.20/24", "2001:db8:1234:5678:9abc:def0:1234:5678/64"}})
	s.BlockIO = append(s.BlockIO, monitor.BlockIO{Name: "nvme0n1", ReadRate: 2048, WriteRate: 4096, ReadIOPS: 2, WriteIOPS: 4, Busy: 1, Valid: true})
	d := s.Inventory.Docker
	d.Containers[0].Ports = "0.0.0.0:8080->80/tcp, [2001:db8:1234:5678:9abc:def0:1234:5678]:8443->443/tcp, 127.0.0.1:9000-9010->9000-9010/tcp"
	d.Images = []monitor.Image{{ID: strings.Repeat("a", 64), Repository: "example/web", Tag: "stable", Size: "10MB"}}
	return a
}

func showAssessedSection(a *app, section string) []string {
	s := a.sessions[0]
	switch section {
	case "system":
		a.diagnosticTab = 3
		return []string{"系统信息 操作系统", "系统信息 硬件"}
	case "network":
		a.statusMetric = "网络"
		return []string{"网卡 eth0", "网卡 ens192"}
	case "blocks":
		a.statusMetric = "磁盘"
		return []string{"块设备 vda", "块设备 nvme0n1"}
	case "processes":
		a.diagnosticTab = 1
	case "services":
		a.diagnosticTab = 2
	case "images":
		a.diagnosticTab = 4
		a.managementState().dockerTab = 1
	case "container-info", "logs":
		a.diagnosticTab = 4
		d := s.stats.Inventory.Docker
		a.managementState().containerDetails = &dockerDialogState{kind: "container", session: s, row: d.Containers[0], target: serverops.DockerTarget{ID: d.Containers[0].ID, Engine: d.EngineID, Socket: d.Socket}, tab: 1}
		if section == "logs" {
			state := a.managementState().containerDetails
			state.tab, state.tail = 0, "200 行"
			state.log = &dockerLogView{buffer: &dockerlog.Buffer{}, follow: true}
			state.log.buffer.Write([]byte("2026-10-21T10:00:00Z INFO Server is ready\n"))
		}
		return []string{"容器基本信息", "容器端口映射"}
	}
	return nil
}

func TestNetworkMetricsRespondToCardWidthRatherThanWindowWidth(t *testing.T) {
	a := assessedStatusDemo()
	a.fileExpanded = true
	a.statusMetric = "网络"
	tt := ui.NewTester(a.view, 1280, 1600)
	for _, width := range []int{1280, 1600, 1280} {
		tt.SetSize(width, 1600)
		rx, _ := tt.Find("网卡 eth0 接收速率")
		cumulative, _ := tt.Find("网卡 eth0 累计接收")
		if (rx.Y == cumulative.Y) != (width >= 1600) {
			t.Fatal("network card used the full pane's width for its metrics", width, rx, cumulative)
		}
	}
	s := a.sessions[0].stats
	s.Networks = s.Networks[:len(s.Networks)-1]
	tt.Frame()
	rx, _ := tt.Find("网卡 eth0 接收速率")
	cumulative, _ := tt.Find("网卡 eth0 累计接收")
	if rx.Y != cumulative.Y {
		t.Fatal("single full-width interface did not reclaim the metric row", rx, cumulative)
	}
}

func TestVirtualInterfaceGridAccountsForDisclosureIndent(t *testing.T) {
	a := assessedStatusDemo()
	a.fileExpanded = true
	a.statusMetric = "网络"
	tt := ui.NewTester(a.view, 1090, 1600)
	for _, width := range []int{1090, 1091, 1090} {
		tt.SetSize(width, 1600)
		first, found := tt.Find("网卡 docker0")
		second, ok := tt.Find("网卡 lo")
		if !found || !ok || (first.Y == second.Y) != (width == 1091) || width == 1091 && first.W < 420 {
			t.Fatal("virtual cards split before two minimum widths fit below the disclosure", width, first, second)
		}
	}
}

func TestDockerLogControlsWrapInsideNarrowMainWindow(t *testing.T) {
	a := assessedStatusDemo()
	a.fileExpanded = true
	showAssessedSection(a, "logs")
	tt := ui.NewTester(a.view, 1600, 900)
	for _, width := range []int{1600, 560, 1000, 560} {
		tt.SetSize(width, 900)
		pane, _ := tt.Find("工具面板")
		for _, label := range []string{"返回容器列表", "打开容器终端", "搜索日志", "日志行数", "继续接收", "刷新日志", "复制筛选日志", "跟随最新", "日志内容"} {
			r, found := tt.Find(label)
			if !found || r.W < 10 || r.X < pane.X || r.X+r.W > pane.X+pane.W+1 || r.Y+r.H > pane.Y+pane.H+1 {
				t.Fatal("Docker log controls overflowed the pane", width, label, r, pane)
			}
		}
	}
}

func TestStatusSectionCardsReflowAcrossWholeWindow(t *testing.T) {
	for _, section := range []string{"system", "network", "blocks", "container-info"} {
		t.Run(section, func(t *testing.T) {
			a := assessedStatusDemo()
			a.fileExpanded = true
			labels := showAssessedSection(a, section)
			tt := ui.NewTester(a.view, 1600, 1600)
			for _, width := range []int{560, 1000, 1280, 1600, 2560, 560} {
				tt.SetSize(width, 1600)
				pane, _ := tt.Find("工具面板")
				first, ok := tt.Find(labels[0])
				second, ok2 := tt.Find(labels[1])
				columns := 1
				if width >= 1000 && (section != "network" || width >= 1280) {
					columns = 2
				}
				if !ok || !ok2 || first.W < 300 || second.W < 300 || first.X < pane.X || second.X+second.W > pane.X+pane.W+1 || (first.Y == second.Y) != (columns == 2) {
					t.Fatal("section did not fill or reflow within the main window", width, pane, first, second)
				}
				if columns == 2 && second.X < first.X+first.W || columns == 1 && second.Y < first.Y+first.H {
					t.Fatal("section cards overlap", width, first, second)
				}
				if section == "system" {
					value, found := tt.Find(a.sessions[0].stats.Inventory.CPU)
					if !found || value.X+value.W > second.X+second.W+1 {
						t.Fatal("long hardware details overflow their card", width, value, second)
					}
				}
				if section == "container-info" {
					for _, mapping := range strings.Split(a.sessions[0].stats.Inventory.Docker.Containers[0].Ports, ", ") {
						value, found := tt.Find(mapping)
						if !found || value.X < second.X || value.X+value.W > second.X+second.W+1 {
							t.Fatal("port mapping was lost or overflowed its card", width, mapping, value, second)
						}
					}
				}
			}
		})
	}
}

func TestStatusTablesAndToolbarsFitNarrowWholeWindow(t *testing.T) {
	for _, config := range []struct {
		tab, docker int
		table       string
		lastHeader  string
	}{{1, 0, "进程列表", "运行时间"}, {2, 0, "自启服务列表", "启用"}, {4, 1, "Docker 镜像列表", "创建时间"}, {4, 0, "Docker 容器列表", "累计磁盘 读 / 写"}} {
		t.Run(config.table, func(t *testing.T) {
			a := assessedStatusDemo()
			a.fileExpanded, a.diagnosticTab = true, config.tab
			a.managementState().dockerTab = config.docker
			a.managementState().containerList = true
			tt := ui.NewTester(a.view, 1600, 900)
			for _, width := range []int{1600, 560, 1000, 560} {
				tt.SetSize(width, 900)
				pane, _ := tt.Find("工具面板")
				table, found := tt.Find(config.table)
				if !found || table.W < 300 || table.H < 100 || table.X+table.W > pane.X+pane.W+1 {
					t.Fatal("table lost its viewport", width, table, pane)
				}
				tt.Scroll(table.X+table.W/2, table.Y+table.H/2, 100000, 0)
				header, found := tt.Find(config.lastHeader)
				if !found || header.X < table.X-1 || header.X+header.W > table.X+table.W+1 {
					t.Fatal("rightmost table column became unreachable", width, config.lastHeader, header, table)
				}
			}
		})
	}
}

func TestSingleCoreMeterFillsItsResourceSection(t *testing.T) {
	a := assessedStatusDemo()
	a.sidebarHidden = true
	a.sessions[0].stats.Cores = a.sessions[0].stats.Cores[:1]
	tt := ui.NewTester(func(c *ui.Context) { ui.Column(c).Fill().Children(func() { a.coreSummary(c, a.sessions[0].stats) }) }, 1400, 300)
	for _, width := range []int{1400, 360, 1400} {
		tt.SetSize(width, 300)
		grid, found := tt.Find("逻辑核心网格")
		core, ok := tt.Find("核心 cpu0")
		if !found || !ok || core.W < grid.W-1 {
			t.Fatal(fmt.Sprint(width), "single core reserved empty columns", grid, core)
		}
	}
}
