package desktop

import (
	"fmt"
	"strings"
	"testing"

	"github.com/egoist/mygo/ui"
	"sshclient/internal/monitor"
)

func responsiveWorkspaceDemo() *app {
	a := compactStatusDemo()
	a.terminalShare = 40
	s := a.sessions[0].stats
	s.Disks = []monitor.Disk{
		{Mount: "/", Device: "/dev/vda1", Filesystem: "ext4", Total: 40 << 30, Used: 8 << 30, Free: 32 << 30},
		{Mount: "/srv/data", Device: "/dev/disk/by-id/a-long-storage-device-identifier", Filesystem: "xfs", Total: 1 << 40, Used: 128 << 30, Free: 896 << 30},
		{Mount: "/mnt/archive", Device: "/dev/vdc1", Filesystem: "ext4", Total: 80 << 30, Used: 8 << 30, Free: 72 << 30},
	}
	s.Inventory.Docker = &monitor.DockerInfo{Status: "ready", Version: "28.0", EngineID: "fixture-engine", Socket: "/var/run/docker.sock"}
	for i, name := range []string{"web", "api", "worker", "scheduler"} {
		s.Inventory.Docker.Containers = append(s.Inventory.Docker.Containers, monitor.Container{
			ID: strings.Repeat(string(rune('a'+i)), 64), Names: name, State: "running", Status: "Up 1 day", Image: "example/" + name + ":stable", Project: "personal",
		})
	}
	return a
}

func TestWholeWindowResizeKeepsToolsUsableAndRestoresRightDock(t *testing.T) {
	for _, right := range []bool{false, true} {
		t.Run(fmt.Sprint(right), func(t *testing.T) {
			a := responsiveWorkspaceDemo()
			a.toolsRight, a.terminalWidthShare = right, 62
			tt := ui.NewTester(a.view, 1280, 900)
			for _, width := range []int{1280, 800, 640, 560, 1920, 560, 1280} {
				tt.SetSize(width, 760)
				term, ok := tt.Find("终端区域")
				panel, hasPanel := tt.Find("工具面板")
				if !ok || !hasPanel || term.W < 100 || term.H < 100 || panel.W < 300 || panel.X+panel.W > float32(width)+1 {
					t.Fatal("main-window resize lost usable terminal or tools", width, term, panel)
				}
				if right && width >= 900 {
					if panel.X < term.X+term.W {
						t.Fatal("wide window did not restore right docking", width, term, panel)
					}
				} else if panel.X != term.X || panel.Y < term.Y+term.H {
					t.Fatal("narrow window did not preserve a usable bottom dock", width, term, panel)
				}
				for _, label := range []string{"服务器状态", "工具面板布局", "收起工具面板", "概览", "Docker"} {
					r, found := tt.Find(label)
					if !found || r.X < panel.X-1 || r.X+r.W > panel.X+panel.W+1 {
						t.Fatal("tool navigation overflowed the resized main window", width, label, r, panel)
					}
				}
				if a.toolsRight != right {
					t.Fatal("responsive layout replaced the saved dock preference")
				}
			}
		})
	}
}

func TestFilesystemDetailsReflowWithWholeWindow(t *testing.T) {
	a := responsiveWorkspaceDemo()
	a.fileExpanded = true
	a.statusMetric = "磁盘"
	tt := ui.NewTester(a.view, 1280, 1400)
	for _, config := range []struct{ width, mounts int }{{560, 1}, {1000, 2}, {1280, 2}, {1600, 3}, {2560, 3}, {560, 1}} {
		tt.SetSize(config.width, 1400)
		for _, group := range []struct {
			labels  []string
			columns int
		}{
			{[]string{"文件系统 /", "文件系统 /srv/data", "文件系统 /mnt/archive"}, config.mounts},
		} {
			labels := group.labels
			viewport, _ := tt.Find("服务器资源内容")
			tt.Scroll(viewport.X+viewport.W/2, viewport.Y+viewport.H/2, 0, -100000)
			first, found := tt.Find(labels[0])
			if !found {
				t.Fatal("resource section missing", labels[0])
			}
			tt.Scroll(viewport.X+viewport.W/2, viewport.Y+viewport.H/2, 0, first.Y-viewport.Y-8)
			var cards []ui.Rect
			for _, label := range labels {
				r, ok := tt.Find(label)
				if !ok || r.W < 300 || r.X+r.W > float32(config.width)+1 {
					t.Fatal("resource card clipped after main-window resize", config, label, r)
				}
				for _, previous := range cards {
					if r.X < previous.X+previous.W && r.X+r.W > previous.X && r.Y < previous.Y+previous.H && r.Y+r.H > previous.Y {
						t.Fatal("resource cards overlap", config, previous, r)
					}
				}
				cards = append(cards, r)
			}
			for i, r := range cards {
				if (r.Y == cards[0].Y) != (i < group.columns) {
					t.Fatal("resource cards ignored their content density", config, labels, cards)
				}
			}
		}
	}
}

func TestDockerContainersKeepMultipleColumnsAndNarrowToolbar(t *testing.T) {
	for _, tab := range []int{0} {
		t.Run(fmt.Sprint(tab), func(t *testing.T) {
			a := responsiveWorkspaceDemo()
			a.fileExpanded, a.diagnosticTab = true, 4
			a.managementState().dockerTab = tab
			tt := ui.NewTester(a.view, 1600, 900)
			for _, width := range []int{1600, 2560, 1000, 1280, 560, 1600} {
				tt.SetSize(width, 900)
				search, ok := tt.Find("搜索 Docker")
				if !ok || search.W < 80 || search.X+search.W > float32(width)+1 {
					t.Fatal("Docker search overflowed the main window", width, search)
				}
				var first ui.Rect
				order := "bda"
				for i, id := range order {
					r, found := tt.Find("容器 " + strings.Repeat(string(id), 64))
					if !found || r.W < 300 || r.X+r.W > float32(width)+1 || r.H > 150 {
						t.Fatal("Docker card clipped", tab, width, i, r)
					}
					if i == 0 {
						first = r
					} else if (r.Y == first.Y) != (width >= 1280 && i == 1) {
						t.Fatal("Docker cards did not reflow", tab, width, first, r)
					}
				}
			}
			d := a.sessions[0].stats.Inventory.Docker
			d.Containers = d.Containers[:1]
			tt.Frame()
			card, found := tt.Find("容器 " + d.Containers[0].ID)
			pane, _ := tt.Find("工具面板")
			if !found || card.W < pane.W-32 {
				t.Fatal("a single container reserved unused columns", tab, card, pane)
			}
		})
	}
}
