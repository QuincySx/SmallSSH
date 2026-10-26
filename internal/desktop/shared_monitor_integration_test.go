//go:build terminal_integration

package desktop

import (
	"fmt"
	"sync/atomic"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"
	"sshclient/internal/monitor"
	"sshclient/internal/testserver"
	"sshclient/internal/vault"
)

func TestLiveTerminalTabsShareServerCollectionAndDesktopHistory(t *testing.T) {
	h := newConnectionFlowHarness(t)
	var counters, inventories, docker atomic.Int32
	fixture := testserver.NewWithExec(t, func(command string) (string, string, int) {
		switch command {
		case monitor.Command:
			n := counters.Add(1)
			return fmt.Sprintf("@@CPU\ncpu %d 0 100 %d 0 0 0 0\n@@MEM\nMemTotal: 524288 kB\nMemAvailable: 300000 kB\n", 100+n*10, 800+n*90), "", 0
		case monitor.InventoryCommand:
			inventories.Add(1)
			return "@@OS\nTest Linux\n@@SERVICE_MANAGER\nsystemd\n@@DOCKER_STATUS\nmissing\n", "", 0
		case monitor.DockerStatsCommand:
			docker.Add(1)
			return "", "isolated fixture has no Docker", 1
		default:
			return "", "unexpected fixture probe", 1
		}
	})
	p := fixture.Profile()
	if err := h.a.client.SaveConnection(h.ctx, vault.ProfileChange{Profile: p}, h.server.Vault); err != nil {
		t.Fatal(err)
	}
	h.a.connections = h.server.Vault.Summaries()
	h.a.page, h.a.selected = 1, p.ID
	h.a.toolsHidden = true
	h.ui = ui.NewTester(h.a.view, 1100, 760)
	for range 3 {
		h.a.openShellMode(true)
	}
	shared := func() bool {
		if len(h.a.sessions) != 3 {
			return false
		}
		first := h.a.sessions[0]
		if first.stats == nil || first.stats.SourceID == "" || len(first.history.points) < 2 {
			return false
		}
		for _, s := range h.a.sessions[1:] {
			if s.stats != first.stats || len(s.history.points) != len(first.history.points) {
				return false
			}
			if &s.history.points[0] != &first.history.points[0] {
				return false
			}
		}
		return true
	}
	h.wait(shared)
	first := h.a.sessions[0]
	source, at := first.stats.SourceID, first.stats.Time
	if counters.Load() < 2 || inventories.Load() != 1 || docker.Load() != 1 {
		t.Fatal("tabs did not share slow collection lanes", counters.Load(), inventories.Load(), docker.Load(), len(first.history.points))
	}
	if fixture.Auths.Load() != 3 {
		t.Fatal("telemetry opened another SSH connection", fixture.Auths.Load())
	}
	h.a.closeSessionAt(0)
	h.wait(func() bool {
		if len(h.a.sessions) != 2 {
			return false
		}
		left, right := h.a.sessions[0], h.a.sessions[1]
		return left.stats != nil && left.stats.Time.After(at) && left.stats.SourceID == source && right.stats == left.stats
	})
	if fixture.Auths.Load() != 3 {
		t.Fatal("provider handoff opened another SSH connection")
	}
	for len(h.a.sessions) > 0 {
		h.a.closeSessionAt(0)
	}
	// Observe the two-second counter lane after the final tab closes.
	// Backend cancellation tests separately cover the longer-lived slow lanes.
	settle := time.NewTimer(100 * time.Millisecond)
	for {
		select {
		case update := <-h.updates:
			update()
		case <-settle.C:
			goto settled
		}
	}
settled:
	before := counters.Load()
	quiet := time.NewTimer(2200 * time.Millisecond)
	defer quiet.Stop()
	for {
		select {
		case update := <-h.updates:
			update()
		case <-quiet.C:
			if counters.Load() != before || inventories.Load() != 1 || docker.Load() != 1 {
				t.Fatal("last closed tab left counter collection running", before, counters.Load(), inventories.Load(), docker.Load())
			}
			return
		}
	}
}
