package desktop

import (
	"fmt"
	"math"
	"testing"

	"github.com/egoist/mygo/ui"
	"sshclient/internal/monitor"
)

func TestShallowStatusDockShowsEveryMetricAndPreservesDetailNavigation(t *testing.T) {
	for _, config := range []struct {
		width int
		share float32
	}{{1000, 72}, {1000, 90}, {800, 72}} {
		t.Run(fmt.Sprintf("%d/share=%v", config.width, config.share), func(t *testing.T) {
			a := compactStatusDemo()
			a.terminalShare = config.share
			tt := ui.NewTester(a.view, config.width, 760)
			term, _ := tt.Find("终端区域")
			viewport, _ := tt.Find("服务器资源内容")
			reveal := func(label string) ui.Rect {
				t.Helper()
				// Tester labels are clipped to their viewport, so offscreen
				// labels have zero size rather than their full layout bounds.
				a.managementState().resourceOverviewScroll.Y = 0
				tt.Frame()
				for {
					bounds, found := tt.Find(label)
					if found && bounds.W > 0 && bounds.H >= 10 {
						return bounds
					}
					scroll := a.managementState().resourceOverviewScroll
					if scroll.Y >= scroll.MaxY {
						t.Fatal("compact content cannot be revealed", label, bounds, scroll)
					}
					tt.Scroll(viewport.X+viewport.W/2, viewport.Y+viewport.H/2, 0, 8)
				}
			}
			for _, name := range []string{"CPU", "内存", "网络", "磁盘"} {
				card := reveal(name + "详情")
				if card.H > 65 || card.Y < viewport.Y || card.Y+card.H > viewport.Y+viewport.H+1 || card.X+card.W > viewport.X+viewport.W-statusScrollGutter+1 {
					t.Fatal("compact metric is clipped horizontally or lost its viewport", name, card, viewport)
				}
				if _, found := tt.Find(name + "迷你趋势"); found {
					t.Fatal("small dock still reserves a chart in every card", name)
				}
			}
			for _, value := range []string{size(a.sessions[0].stats.MemoryUsed) + " / " + size(a.sessions[0].stats.MemoryTotal), "↑ 6.1 KiB/s · eth0", "写 77.3 KiB/s"} {
				bounds := reveal(value)
				if bounds.Y < viewport.Y || bounds.Y+bounds.H > viewport.Y+viewport.H+1 {
					t.Fatal("secondary metric cannot be revealed", value, bounds)
				}
			}
			if tt.HasText("CPU  ") || tt.HasText("内存  ") {
				t.Fatal("card repeats its title in the value")
			}
			for _, name := range []string{"CPU", "内存", "网络", "磁盘"} {
				reveal(name + "详情")
				if err := tt.Click(name + "详情"); err != nil {
					t.Fatal(err)
				}
				if a.statusMetric != name {
					t.Fatal("compact card lost its detail action", name)
				}
				if err := tt.Click("返回概览"); err != nil {
					t.Fatal(err)
				}
				current, _ := tt.Find("终端区域")
				if current != term {
					t.Fatal("opening compact card details moved the terminal", term, current)
				}
			}
		})
	}
}

func TestStatusCardsAdaptToActualViewportWhenDraggingAndRedocking(t *testing.T) {
	a := compactStatusDemo()
	a.terminalShare = 72
	tt := ui.NewTester(a.view, 1000, 760)
	short, _ := tt.Find("CPU详情")
	term, _ := tt.Find("终端区域")
	x, y := term.X+term.W/2, term.Y+term.H
	tt.Press(x, y)
	tt.Move(x, y-240)
	tt.Release(x, y-240)
	tall, _ := tt.Find("CPU详情")
	if short.H > 65 || tall.H != 106 || !tt.HasText("CPU迷你趋势") {
		t.Fatal("dragging the dock taller did not restore trend cards", short, tall)
	}
	term, _ = tt.Find("终端区域")
	x, y = term.X+term.W/2, term.Y+term.H
	tt.Press(x, y)
	tt.Move(x, y+240)
	tt.Release(x, y+240)
	shortAgain, _ := tt.Find("CPU详情")
	if shortAgain.H != short.H || math.Abs(float64(shortAgain.Y-short.Y)) > 1 {
		t.Fatal("returning to a shallow dock lost compact layout", short, shortAgain)
	}
	a.setToolsDock(true)
	tt.Frame()
	tall, _ = tt.Find("CPU详情")
	if tall.H != 106 || !tt.HasText("CPU迷你趋势") {
		t.Fatal("tall right dock kept a shallow layout", tall)
	}
	a.setToolsDock(false)
	tt.Frame()
	shortAgain, _ = tt.Find("CPU详情")
	if shortAgain.H != short.H {
		t.Fatal("redocking at the bottom lost compact layout", shortAgain)
	}
	tt.Frame()
	if tt.FramePending() {
		t.Fatal("adaptive density keeps requesting frames after layout settles")
	}
}

func compactStatusDemo() *app {
	a := statusDemo()
	s := a.sessions[0].stats
	s.Networks = append(s.Networks, monitor.Network{Name: "lo", Valid: true}, monitor.Network{Name: "br-demo", RX: 3 << 30, TX: 1 << 30, Valid: true})
	s.TCP = monitor.TCPInfo{Available: true, RateValid: true, ActiveOpens: 1381, PassiveOpens: 451, Established: 5}
	s.Inventory.Interfaces = []monitor.Interface{{Name: "eth0", State: "up", Addresses: []string{"192.0.2.10/24"}}, {Name: "docker0", Virtual: true, Addresses: []string{"172.17.0.1/16"}}, {Name: "lo", Virtual: true, Addresses: []string{"127.0.0.1/8", "::1/128"}}, {Name: "br-demo", Virtual: true, Addresses: []string{"172.18.0.1/16"}}}
	a.managementState().virtualOpen = true
	return a
}

func TestCompactNetworkRowsFitContentAtBothWidths(t *testing.T) {
	for _, width := range []int{420, 1100} {
		t.Run(fmt.Sprint(width), func(t *testing.T) {
			a := compactStatusDemo()
			a.sidebarHidden = true
			tt := ui.NewTester(func(c *ui.Context) {
				ui.Column(c).Fill().Gap(8).Children(func() { a.networkCards(c, a.sessions[0].stats) })
			}, width, 850)
			var cards []ui.Rect
			columns := 1
			if width == 1100 {
				columns = 2
			}
			for i, name := range []string{"docker0", "lo", "br-demo"} {
				card, ok := tt.Find("网卡 " + name)
				if !ok || card.H < 60 || card.H > 150 {
					t.Fatal("network card clipped or unnecessarily tall", name, card)
				}
				if i > 0 && i%columns != 0 {
					previous := cards[i-1]
					if gap := card.X - previous.X - previous.W; card.Y != previous.Y || gap < 7 || gap > 9 {
						t.Fatal("network cards did not share the available row", previous, card)
					}
				} else if i > 0 {
					previous := cards[i-columns]
					if gap := card.Y - previous.Y - previous.H; gap < 7 || gap > 9 {
						t.Fatal("network rows have artificial spacing or overlap", previous, card)
					}
				}
				value, ok := tt.Find(name)
				if !ok || value.Y < card.Y || value.Y+value.H > card.Y+card.H {
					t.Fatal("interface name clipped", name, card, value)
				}
				cards = append(cards, card)
			}
			if _, ok := tt.Find("虚拟网卡列表"); ok {
				t.Fatal("three interfaces should not need a nested scrollbar")
			}
		})
	}
}

func TestLargeInterfaceListMeasuresRowsAndRemainsVirtualAfterResize(t *testing.T) {
	a := compactStatusDemo()
	a.sidebarHidden = true
	s := a.sessions[0].stats
	s.Networks = []monitor.Network{{Name: "eth0", Valid: true}}
	s.Inventory.Interfaces = []monitor.Interface{{Name: "eth0", State: "up"}}
	for n := range 200 {
		name := fmt.Sprintf("veth-%03d", n)
		s.Networks = append(s.Networks, monitor.Network{Name: name, Valid: true})
		s.Inventory.Interfaces = append(s.Inventory.Interfaces, monitor.Interface{Name: name, Virtual: true})
	}
	tt := ui.NewTester(func(c *ui.Context) {
		ui.Column(c).Fill().Children(func() { a.networkCards(c, s) })
	}, 1100, 600)
	for _, width := range []int{1100, 420, 1100} {
		tt.SetSize(width, 600)
		a.managementState().networkList.ScrollTo(0, ui.Start)
		tt.Frame()
		first, ok := tt.Find("网卡 veth-000")
		nextRow := "网卡 veth-001"
		if width == 1100 {
			nextRow = "网卡 veth-002"
			second, ok2 := tt.Find("网卡 veth-001")
			if !ok2 || second.Y != first.Y || second.X < first.X+first.W {
				t.Fatal("wide virtual interface list did not use two columns", first, second)
			}
		}
		second, ok2 := tt.Find(nextRow)
		if !ok || !ok2 || second.Y-first.Y-first.H < 7 || second.Y-first.Y-first.H > 9 {
			t.Fatal("measured interface rows did not reflow", width, first, second)
		}
		if tt.HasText("veth-199") {
			t.Fatal("large interface list rendered its entire inventory")
		}
		list, ok := tt.Find("虚拟网卡列表")
		if !ok || list.H > 321 {
			t.Fatal("large inventory grew beyond the bounded viewport", list)
		}
		tt.Scroll(list.X+list.W/2, list.Y+list.H/2, 0, 100000)
		if !tt.HasText("veth-199") {
			t.Fatal("last interface became unreachable", width)
		}
	}
}
