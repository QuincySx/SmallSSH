package desktop

import (
	"testing"
	"time"

	"github.com/egoist/mygo/ui"
	"sshclient/internal/monitor"
)

func TestContainerCardsShowMatchingStatsAndRejectStaleOrOtherDaemon(t *testing.T) {
	a := managementApp()
	s := a.sessions[0]
	d := s.stats.Inventory.Docker
	metrics := &monitor.DockerSnapshot{Time: time.Now(), Status: "ready", EngineID: d.EngineID, Socket: d.Socket, Rows: []monitor.ContainerStats{{ID: d.Containers[0].ID, Memory: 205 << 20, CPU: 203.5, CPUValid: true, MemoryValid: true}}}
	s.stats.DockerStats = metrics
	tt := ui.NewTester(func(c *ui.Context) { ui.Column(c).Fill().Children(func() { a.dockerView(c, s.stats.Inventory) }) }, 1050, 500)
	if !tt.HasText("205.0 MiB") || !tt.HasText("204%") {
		t.Fatal("live metrics missing", tt.Texts())
	}
	metrics.EngineID = "another-daemon"
	tt.Frame()
	if tt.HasText("205.0 MiB") {
		t.Fatal("resource values followed another daemon")
	}
	metrics.EngineID = d.EngineID
	metrics.Time = time.Now().Add(-time.Minute)
	tt.Frame()
	if tt.HasText("205.0 MiB") {
		t.Fatal("stale resource values presented as current")
	}
	if err := tt.Click("容器显示设置"); err != nil {
		t.Fatal(err)
	}
	if err := tt.ChooseMenuItem("列表视图"); err != nil {
		t.Fatal(err)
	}
	if _, ok := tt.Find("Docker 容器列表"); !ok {
		t.Fatal("shared data did not switch to table")
	}
}

func TestVirtualNetworkCardsDefaultCollapsedAndKeepCumulativeFlow(t *testing.T) {
	a := managementApp()
	s := a.sessions[0].stats
	s.DefaultNetwork = "eth0"
	s.Networks = []monitor.Network{{Name: "eth0", RX: 84 << 30, TX: 532 << 20, RXRate: 201 << 10, TXRate: 23 << 10, Valid: true}, {Name: "veth42", RX: 20 << 30, TX: 96 << 20, Valid: true}}
	s.Inventory.Interfaces = []monitor.Interface{{Name: "eth0", Addresses: []string{"192.0.2.10/24"}, State: "up"}, {Name: "veth42", Virtual: true, State: "up"}}
	tt := ui.NewTester(func(c *ui.Context) { ui.Column(c).Fill().Children(func() { a.networkCards(c, s) }) }, 900, 500)
	if !tt.HasText("192.0.2.10/24") || !tt.HasText("84.0 GiB") || tt.HasText("veth42") {
		t.Fatal(tt.Texts())
	}
	if err := tt.Click("虚拟网卡 · 1"); err != nil {
		t.Fatal(err)
	}
	tt.Advance(500 * time.Millisecond)
	if !tt.HasText("veth42") || !tt.HasText("96.0 MiB") {
		t.Fatal(tt.Texts())
	}
}

func TestMountCardsKeepRootOverlayAndReadOnlyROM(t *testing.T) {
	a := managementApp()
	a.sidebarHidden = true
	s := a.sessions[0].stats
	s.Disks = []monitor.Disk{{Mount: "/", Device: "overlayfs:/overlay", Filesystem: "overlay", Total: 74 << 20, Used: 70 << 20, Free: 4 << 20, ModeKnown: true}, {Mount: "/overlay", Device: "/dev/ubi0_2", Filesystem: "ubifs", Total: 74 << 20, Used: 70 << 20, Free: 4 << 20, ModeKnown: true}, {Mount: "/rom", Device: "/dev/root", Filesystem: "squashfs", Total: 16 << 20, Used: 16 << 20, ReadOnly: true, ModeKnown: true}}
	tt := ui.NewTester(func(c *ui.Context) { ui.Column(c).Fill().Children(func() { a.diskCapacity(c, s, false) }) }, 420, 600)
	for _, size := range []struct{ width, columns int }{{420, 1}, {900, 2}, {1400, 3}, {900, 2}, {420, 1}} {
		tt.SetSize(size.width, 600)
		for _, text := range []string{"overlayfs:/overlay", "/overlay", "/rom", "SQUASHFS", "只读", "70.0 MiB / 74.0 MiB"} {
			if !tt.HasText(text) {
				t.Fatal(size.width, text, tt.Texts())
			}
		}
		var cards []ui.Rect
		for _, mount := range []string{"/", "/overlay", "/rom"} {
			card, ok := tt.Find("文件系统 " + mount)
			if !ok || card.X < 0 || card.X+card.W > float32(size.width)+1 || card.H < 60 || card.H > 110 {
				t.Fatal("mount card clipped or unnecessarily tall", size.width, mount, card)
			}
			if len(cards) > 0 && (card.W-cards[0].W < -1 || card.W-cards[0].W > 1) {
				t.Fatal("incomplete row expanded beyond its grid column", size.width, cards[0], card)
			}
			for _, previous := range cards {
				if card.X < previous.X+previous.W && card.X+card.W > previous.X && card.Y < previous.Y+previous.H && card.Y+card.H > previous.Y {
					t.Fatal("mount cards overlap", size.width, previous, card)
				}
			}
			cards = append(cards, card)
		}
		for i, card := range cards {
			if (card.Y == cards[0].Y) != (i < size.columns) {
				t.Fatal("mount cards did not reflow on resize", size.width, cards)
			}
		}
	}
	s.Disks = s.Disks[:1]
	tt.SetSize(1400, 600)
	card, foundCard := tt.Find("文件系统 /")
	grid, foundGrid := tt.Find("文件系统容量卡片")
	if !foundCard || !foundGrid || card.W < grid.W-1 {
		t.Fatal("a single mount reserved unused columns", card, grid)
	}
}
