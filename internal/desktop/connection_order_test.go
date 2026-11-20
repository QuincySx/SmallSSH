package desktop

import (
	"reflect"
	"testing"

	"sshclient/internal/protocol"

	"github.com/egoist/mygo/ui"
	"sshclient/internal/broker"
	"sshclient/internal/vault"
)

func TestConnectionOrderDropMovesWholeGroupsAndFilteredMembers(t *testing.T) {
	rows := []vault.Summary{{ID: "b1", Group: "B"}, {ID: "b2", Group: "B"}, {ID: "a1", Group: "A"}, {ID: "u"}}
	version := vault.ConnectionOrderRevision(rows)
	for _, tc := range []struct {
		drag   connectionDrag
		target connectionRailRow
		after  bool
		want   []vault.ConnectionPlacement
	}{
		{connectionDrag{Revision: version, Group: "B", Header: true}, connectionRailRow{group: "A", header: true}, true, []vault.ConnectionPlacement{{ID: "a1", Group: "A"}, {ID: "b1", Group: "B"}, {ID: "b2", Group: "B"}, {ID: "u", Group: ""}}},
		{connectionDrag{Revision: version, ID: "b2"}, connectionRailRow{group: "A", header: true}, false, []vault.ConnectionPlacement{{ID: "b1", Group: "B"}, {ID: "a1", Group: "A"}, {ID: "b2", Group: "A"}, {ID: "u", Group: ""}}},
		{connectionDrag{Revision: version, ID: "b2"}, connectionRailRow{group: "B", connection: rows[0]}, false, []vault.ConnectionPlacement{{ID: "b2", Group: "B"}, {ID: "b1", Group: "B"}, {ID: "a1", Group: "A"}, {ID: "u", Group: ""}}},
		{connectionDrag{Revision: version, ID: "u"}, connectionRailRow{group: "A", connection: rows[2]}, false, []vault.ConnectionPlacement{{ID: "b1", Group: "B"}, {ID: "b2", Group: "B"}, {ID: "u", Group: "A"}, {ID: "a1", Group: "A"}}},
	} {
		got, err := connectionOrderDrop(rows, tc.drag, tc.target, tc.after)
		if err != nil || !reflect.DeepEqual(got.Items, tc.want) {
			t.Fatal("drop lost hidden members or placement", got, err)
		}
	}
	if _, err := connectionOrderDrop(rows, connectionDrag{Revision: "stale", ID: "b1"}, connectionRailRow{group: "A", header: true}, false); err == nil {
		t.Fatal("stale drag accepted")
	}
}

func TestConnectionOrderPointerDropPersistsThroughOwnerRPC(t *testing.T) {
	h := newConnectionFlowHarness(t)
	a := h.a
	for i, id := range []string{"bridge", "web", "db"} {
		p := h.fixture.Profile()
		p.ID = id
		p.Name = id
		p.Group = "B"
		if i == 2 {
			p.Group = "A"
		}
		if err := h.server.Vault.SaveProfile(broker.VaultPath(h.dir), p); err != nil {
			t.Fatal(err)
		}
	}
	a.connections = h.server.Vault.Summaries()
	a.page = 1
	h.ui = ui.NewTester(a.connectionTree, 230, 600)
	source, ok := h.ui.Find("bridge")
	if !ok {
		t.Fatal(h.ui.Texts())
	}
	target, ok := h.ui.Find("连接分组：A")
	if !ok {
		t.Fatal(h.ui.Texts())
	}
	x, y := source.X+source.W/2, source.Y+source.H/2
	h.ui.Press(x, y)
	h.ui.Move(x+14, y)
	h.ui.Move(target.X+target.W/2, target.Y+target.H/2)
	h.ui.Release(target.X+target.W/2, target.Y+target.H/2)
	h.ui.Frame()
	moved, ok := h.ui.Find("bridge")
	if !ok || moved.Y <= target.Y || a.connectionRailCache.pending == nil {
		t.Fatal("row did not land before the UI received the saving result", moved, target)
	}
	before := protocol.Message{Connections: a.connections}
	epoch, version := a.epoch.Load(), a.connectionOrderVersion.Load()
	a.applyConnectionSnapshot(epoch, version, before, nil)
	h.ui.Frame()
	if still, _ := h.ui.Find("bridge"); still.Y != moved.Y {
		t.Fatal("a poll displaced the row during the write", still, moved)
	}
	h.wait(func() bool { return a.connectionRailCache.pending == nil })
	a.applyConnectionSnapshot(epoch, version, before, nil)
	h.ui.Frame()
	if still, _ := h.ui.Find("bridge"); still.Y != moved.Y {
		t.Fatal("a queued older poll displaced the row after acknowledgement", still, moved)
	}
	p, err := h.server.Vault.Profile("bridge")
	if err != nil || p.Group != "A" {
		t.Fatal("native list drag did not reach durable owner write", p.Group, err, a.status)
	}
}

func TestConnectionOrderConflictRestoresConfirmedRows(t *testing.T) {
	h := newConnectionFlowHarness(t)
	a := h.a
	for _, id := range []string{"bridge", "db"} {
		p := h.fixture.Profile()
		p.ID, p.Name, p.Group = id, id, "Servers"
		if err := h.server.Vault.SaveProfile(broker.VaultPath(h.dir), p); err != nil {
			t.Fatal(err)
		}
	}
	a.connections = h.server.Vault.Summaries()
	a.page = 1
	h.ui = ui.NewTester(a.connectionTree, 230, 400)
	change, err := connectionOrderDrop(a.connections, connectionDrag{
		Revision: vault.ConnectionOrderRevision(a.connections), ID: "db",
	}, connectionRailRow{group: "Servers", connection: a.connections[0]}, false)
	if err != nil {
		t.Fatal(err)
	}
	// Another owner has added a connection since this UI read the list.
	p := h.fixture.Profile()
	p.ID, p.Name, p.Group = "new", "new", "Servers"
	if err := h.server.Vault.SaveProfile(broker.VaultPath(h.dir), p); err != nil {
		t.Fatal(err)
	}

	first, _ := h.ui.Find("bridge")
	if !a.saveConnectionOrder(change) {
		t.Fatal("could not preview the placement")
	}
	h.ui.Frame()
	if moved, _ := h.ui.Find("db"); moved.Y != first.Y {
		t.Fatal("pending placement was not shown", moved, first)
	}
	h.wait(func() bool { return a.connectionRailCache.pending == nil })
	if restored, _ := h.ui.Find("bridge"); restored.Y != first.Y || a.status == "" {
		t.Fatal("rejected placement did not restore the confirmed order and report the conflict", restored, a.status)
	}
}
