package desktop

import (
	"context"
	"math"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"
	"sshclient/internal/monitor"
	"sshclient/internal/vault"
)

func statusDemo() *app {
	a := &app{ctx: context.Background(), online: true, page: 1, groups: map[string]*bool{}, selected: "demo", sidebarWidth: 190, terminalShare: 65, dock: toolDiagnostics, statusRange: "5 分钟"}
	a.connections = []vault.Summary{{ID: "demo", Name: "演示服务器", Host: "192.0.2.10", User: "root", Port: 22, Group: "个人服务"}}
	sess := &session{host: "demo", name: "演示服务器", monitorStatus: "自动更新 · 每 2 秒"}
	now := time.Now().Truncate(time.Second)
	for j := 0; j < 451; j++ {
		wave := float64(j)
		cpu := 12 + 8*math.Sin(wave/18) + 18*math.Exp(-math.Pow((wave-395)/12, 2))
		rx := 16000 + 12000*math.Sin(wave/21) + 50000*math.Exp(-math.Pow((wave-390)/9, 2))
		s := &monitor.Stats{Time: now.Add(time.Duration(j-450) * 2 * time.Second), CPU: cpu, CPUDetail: monitor.CPUUsage{Name: "cpu", Used: cpu, User: cpu * .7, System: cpu * .3, Wait: 1.2, Valid: true}, Cores: []monitor.CPUUsage{{Name: "cpu0", Used: cpu, Valid: true}}, MemoryUsed: uint64((210 + 12*math.Sin(wave/40)) * 1048576), MemoryTotal: 512 << 20, MemoryAvailable: 290 << 20, MemoryFree: 130 << 20, MemoryCached: 150 << 20, MemoryBuffers: 10 << 20, SwapTotal: 512 << 20, SwapUsed: 18 << 20, Load: "0.12 / 0.08 / 0.05", Uptime: 238268, ProcessCount: 88, RunningProcesses: 1, DefaultNetwork: "eth0", Networks: []monitor.Network{{Name: "eth0", RX: 1824 << 20, TX: 647 << 20, RXRate: rx, TXRate: rx * .28, RXPackets: 72014, TXPackets: 43010, Valid: true}, {Name: "docker0", RX: 12 << 20, TX: 20 << 20, RXRate: 130, TXRate: 390, Valid: true}}, BlockIO: []monitor.BlockIO{{Name: "vda", ReadRate: 8192 + 4096*math.Sin(wave/16), WriteRate: 50000 + 30000*math.Sin(wave/17), ReadIOPS: 1.3, WriteIOPS: 7.2, Busy: 3.6, Valid: true}}, Disks: []monitor.Disk{{Device: "/dev/vda1", Mount: "/", Filesystem: "ext4", Total: 20 << 30, Used: 5268 << 20, Free: 14 << 30}, {Device: "tmpfs", Mount: "/run", Filesystem: "tmpfs", Total: 52 << 20, Used: 2 << 20, Free: 50 << 20}}, Processes: []monitor.Process{{PID: 812, Name: "sshd", User: "root", State: "Ss", Elapsed: "03:12:40", CPU: 1.2, RSS: 7 << 20}, {PID: 445, Name: "nginx", User: "www-data", State: "S", Elapsed: "03:12:39", CPU: .4, RSS: 4 << 20}, {PID: 723, Name: "sub-store", User: "root", State: "Sl", Elapsed: "03:12:35", CPU: .8, RSS: 42 << 20}}, Inventory: &monitor.Inventory{Time: now, Hostname: "demo-node", OS: "Debian GNU/Linux 12", Architecture: "x86_64", Kernel: "6.1.0-28-cloud-amd64", Model: "Cloud Server", CPU: "Intel Xeon Platinum Virtual CPU", Cores: 1, Manager: "systemd", Services: []monitor.Service{{Name: "nginx.service", Enabled: "enabled", Active: "active", Sub: "running", Description: "A high performance web server"}, {Name: "frps.service", Enabled: "enabled", Active: "active", Sub: "running"}}}}
		sess.stats = s
		sess.history.add(s)
	}
	a.sessions = []*session{sess}
	return a
}

func TestResourceNavigationPreservesTerminalAndPinsReturn(t *testing.T) {
	for _, mode := range []string{"bottom", "small", "right", "expanded"} {
		t.Run(mode, func(t *testing.T) {
			a := statusDemo()
			a.toolsRight, a.fileExpanded = mode == "right", mode == "expanded"
			a.terminalWidthShare = 62
			width, height := 1280, 900
			if mode == "small" {
				width, height = 1000, 760
			}
			tt := ui.NewTester(a.view, width, height)
			original, terminalVisible := tt.Find("终端区域")
			panel, _ := tt.Find("工具面板")
			for _, name := range []string{"CPU", "内存", "网络", "磁盘"} {
				if err := tt.Click(name + "详情"); err != nil {
					t.Fatal(name, err, tt.Texts())
				}
				after, visible := tt.Find("终端区域")
				afterPanel, _ := tt.Find("工具面板")
				if a.fileExpanded != (mode == "expanded") || a.statusMetric != name || visible != terminalVisible || after != original || afterPanel != panel {
					t.Fatal("detail changed terminal or pane geometry", mode, original, after, panel, afterPanel)
				}
				back, ok := tt.Find("返回概览")
				viewport, scrollable := tt.Find("服务器资源内容")
				if !ok || !scrollable || back.Y+back.H > viewport.Y || back.Y < panel.Y {
					t.Fatal("return is not pinned above the scroll viewport", back, viewport)
				}
				tt.Scroll(viewport.X+viewport.W/2, viewport.Y+viewport.H/2, 0, 10000)
				if scroll := a.managementState().resourceDetailScroll; scroll.MaxY > 0 && scroll.Y == 0 {
					t.Fatal("test detail was not scrolled")
				}
				pinned, _ := tt.Find("返回概览")
				if pinned != back {
					t.Fatal("return disappeared with scrolled content", back, pinned)
				}
				if err := tt.Click("返回概览"); err != nil {
					t.Fatal(err)
				}
				after, visible = tt.Find("终端区域")
				if a.statusMetric != "" || a.fileExpanded != (mode == "expanded") || after != original || visible != terminalVisible {
					t.Fatal("return changed terminal geometry", original, after)
				}
			}
			if mode == "expanded" {
				if err := tt.Click("恢复终端分栏"); err != nil {
					t.Fatal(err)
				}
				if _, ok := tt.Find("终端区域"); !ok || a.fileExpanded {
					t.Fatal("explicit restore did not return the terminal")
				}
			}
			if err := tt.Click("系统信息"); err != nil {
				t.Fatal(err)
			}
			if !tt.HasText("Debian GNU/Linux 12") || !tt.HasText("Intel Xeon Platinum Virtual CPU") || tt.HasText("192.0.2.10") || tt.HasText("SSH 端口") {
				t.Fatal(tt.Texts())
			}
		})
	}
}

func TestResourceReturnRestoresOverviewScrollPosition(t *testing.T) {
	a := statusDemo()
	tt := ui.NewTester(a.view, 560, 760)
	viewport, _ := tt.Find("服务器资源内容")
	tt.Scroll(viewport.X+viewport.W/2, viewport.Y+viewport.H/2, 0, 24)
	state := a.managementState()
	position := state.resourceOverviewScroll.Y
	if position == 0 {
		t.Fatal("overview was not scrolled")
	}
	if err := tt.Click("CPU详情"); err != nil {
		t.Fatal(err)
	}
	if state.resourceDetailScroll.Y != 0 {
		t.Fatal("new detail inherited the overview scroll")
	}
	viewport, _ = tt.Find("服务器资源内容")
	tt.Scroll(viewport.X+viewport.W/2, viewport.Y+viewport.H/2, 0, 10000)
	if err := tt.Click("返回概览"); err != nil {
		t.Fatal(err)
	}
	if a.statusMetric != "" || state.resourceOverviewScroll.Y != position {
		t.Fatal("return lost the overview position", position, state.resourceOverviewScroll.Y)
	}
}

func TestOverviewOnlyShowsSummaryCardsAndFitsScrollbarGutter(t *testing.T) {
	a := responsiveWorkspaceDemo()
	a.fileExpanded, a.sidebarHidden = true, true
	tt := ui.NewTester(a.view, 1280, 900)
	for _, config := range []struct{ width, columns int }{{360, 1}, {397, 1}, {398, 2}, {585, 2}, {586, 3}, {773, 3}, {774, 4}, {1280, 4}, {360, 1}} {
		// These breakpoints describe content width; compact navigation now
		// deliberately retains its 48px rail beside the resource viewport.
		tt.SetSize(config.width+48, 900)
		viewport, _ := tt.Find("服务器资源内容")
		var cards []ui.Rect
		for i, name := range []string{"CPU详情", "内存详情", "网络详情", "磁盘详情"} {
			card, found := tt.Find(name)
			if !found || card.W < 180 || card.X < viewport.X || card.X+card.W > viewport.X+viewport.W-statusScrollGutter+1 {
				t.Fatal("summary card is missing, compressed, or under the scrollbar", config, name, card, viewport)
			}
			if i > 0 && (card.Y == cards[0].Y) != (i < config.columns) {
				t.Fatal("overview did not fit complete columns before wrapping", config, cards, card)
			}
			cards = append(cards, card)
		}
		if _, found := tt.Find("系统摘要"); !found {
			t.Fatal("system summary missing")
		}
		for _, label := range []string{"核心负载", "网卡卡片", "TCP 连接状态", "文件系统容量卡片", "概览容器资源卡片"} {
			if _, found := tt.Find(label); found {
				t.Fatal("overview still expands detail sections", label)
			}
		}
	}
	for _, check := range []struct{ card, detail string }{{"CPU详情", "逻辑核心网格"}, {"网络详情", "网卡 eth0"}, {"磁盘详情", "文件系统容量卡片"}} {
		if err := tt.Click(check.card); err != nil {
			t.Fatal(err)
		}
		if _, found := tt.Find(check.detail); !found {
			t.Fatal("detail data is no longer reachable through the summary card", check)
		}
		if err := tt.Click("返回概览"); err != nil {
			t.Fatal(err)
		}
	}
}

func TestStatusHistoryIsBoundedAndSessionIsolated(t *testing.T) {
	a := statusDemo()
	sess := a.sessions[0]
	now := sess.stats.Time
	if len(sess.history.points) != statusHistoryLimit {
		t.Fatal(len(sess.history.points))
	}
	duplicate := *sess.stats
	duplicate.CPU = 99
	sess.history.add(&duplicate)
	if sess.history.points[len(sess.history.points)-1].cpu == 99 {
		t.Fatal("duplicate timestamp rewrote history")
	}
	for j := 1; j <= 1000; j++ {
		s := duplicate
		s.Time = now.Add(time.Duration(j) * 2 * time.Second)
		sess.history.add(&s)
	}
	if len(sess.history.points) != statusHistoryLimit || sess.history.points[0].at.Before(now.Add(1100*time.Second)) {
		t.Fatal("history grew beyond window")
	}
	old := len(sess.history.points)
	a.dispatch = func(fn func()) { fn() }
	a.generation = 3
	a.updateServerStatus(sess, 2, &monitor.Stats{Time: now.Add(time.Hour)}, "late")
	if len(sess.history.points) != old || sess.monitorStatus == "late" {
		t.Fatal("stale generation polluted history")
	}
	other := &session{host: "other"}
	a.updateServerStatus(other, 3, &monitor.Stats{Time: now.Add(time.Hour)}, "late")
	if len(other.history.points) != 0 {
		t.Fatal("unattached session accepted history")
	}
	sess.closed = true
	a.updateServerStatus(sess, 3, &monitor.Stats{Time: now.Add(time.Hour)}, "late")
	if sess.stats.Time.After(now) {
		t.Fatal("closed session accepted history")
	}
}

func TestTrendHoverUsesTimestampsAcrossMissingSamples(t *testing.T) {
	now := time.Now()
	points := []statusPoint{{at: now}, {at: now.Add(2 * time.Second)}, {at: now.Add(60 * time.Second)}}
	if nearestStatusPoint(points, now.Add(3*time.Second)) != 1 || nearestStatusPoint(points, now.Add(59*time.Second)) != 2 || nearestStatusPoint(nil, now) != -1 {
		t.Fatal("hover picked by array index rather than time")
	}
}

func TestResourceCardsNeverOverlapAndMissingRatesAreUnknown(t *testing.T) {
	for _, right := range []bool{false, true} {
		a := statusDemo()
		a.toolsRight = right
		a.terminalWidthShare = 62
		tt := ui.NewTester(a.view, 1000, 760)
		cpu, _ := tt.Find("CPU详情")
		mem, _ := tt.Find("内存详情")
		if cpu.Y == mem.Y && cpu.X+cpu.W > mem.X {
			t.Fatal("resource cards overlap", right, cpu, mem)
		}
		if err := tt.Click("自启服务"); err != nil || !tt.HasText("nginx.service") {
			t.Fatal("narrow toolbar click was intercepted", right, err, tt.Texts())
		}
	}
	if sampleRate(0, false) != "—" || sampleRate(0, true) != "0 B/s" {
		t.Fatal("missing sample rendered as idle")
	}
}

func BenchmarkServerStatusFrames(b *testing.B) {
	for _, metric := range []string{"overview", "CPU", "网络"} {
		b.Run(metric, func(b *testing.B) {
			a := statusDemo()
			if metric != "overview" {
				a.statusMetric = metric
				a.fileExpanded = true
			}
			tt := ui.NewTester(a.view, 1280, 900)
			tt.Frame()
			b.ReportAllocs()
			b.ResetTimer()
			for range b.N {
				tt.Frame()
			}
		})
	}
}
