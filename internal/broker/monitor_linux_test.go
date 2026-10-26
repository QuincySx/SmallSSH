//go:build linux

package broker_test

import (
	"context"
	"sshclient/internal/approval"
	"sshclient/internal/broker"
	"testing"
	"time"
)

func TestLinuxMonitorSSHRealSamplesAndRevocation(t *testing.T) {
	c, _ := setup(t)
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	r, ticket := request(t, c, approval.Operation{Host: "fixture", Kind: "monitor", Client: "linux integration"})
	approve(t, c, r)
	s, e := c.Start(ctx, r.ID, ticket, 0, 0)
	if e != nil {
		t.Fatal(e)
	}
	defer s.Close()
	var inventorySeen bool
	for i := 0; i < 3; i++ {
		m, e := s.Receive()
		if e != nil || m.Stats == nil {
			t.Fatal(m, e)
		}
		if m.Stats.MemoryTotal == 0 || len(m.Stats.Processes) == 0 || len(m.Stats.Disks) == 0 || m.Stats.CPU < 0 || m.Stats.CPU > 100 {
			t.Fatal(m.Stats)
		}
		if info := m.Stats.Inventory; info != nil {
			inventorySeen = true
			if info.OS == "" || info.Architecture == "" || info.Cores < 1 {
				t.Fatal(info)
			}
		}
	}
	if !inventorySeen {
		t.Fatal("fixed inventory probe never reached the approved monitor stream")
	}
	if e = c.Decide(ctx, r.ID, false, true, pass); e != nil {
		t.Fatal(e)
	}
	if _, e = s.Receive(); e == nil {
		t.Fatal("revoked monitoring continues")
	}
	if _, e = c.Call(ctx, broker.Message{Action: "request", Operation: approval.Operation{Host: "fixture", Kind: "monitor", Client: "malicious", Command: "arbitrary"}, TTL: 60}); e == nil {
		t.Fatal("monitor accepted arbitrary command")
	}
}
