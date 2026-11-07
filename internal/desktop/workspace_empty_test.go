package desktop

import (
	"context"
	"testing"

	"github.com/egoist/mygo/ui"
	"sshclient/internal/vault"
)

func TestWorkspaceEmptyStatesAndSearchFocus(t *testing.T) {
	server := vault.Summary{ID: "opaque-id", Name: "Example server", Host: "192.0.2.10", User: "demo"}
	for _, state := range []struct {
		name, selected, search, title, action string
		connections                           []vault.Summary
	}{
		{"no servers", "", "", "添加你的第一台服务器", "添加服务器", nil},
		{"no selection", "", "", "选择服务器，开始连接", "选择服务器", []vault.Summary{server}},
		{"stale selection", "deleted", "", "选择服务器，开始连接", "选择服务器", []vault.Summary{server}},
		{"selected", "opaque-id", "", "Example server", "连接终端", []vault.Summary{server}},
		{"selected but filtered", "opaque-id", "no matches", "Example server", "连接终端", []vault.Summary{server}},
	} {
		t.Run(state.name, func(t *testing.T) {
			a := &app{ctx: context.Background(), online: true, page: 1, sidebarHidden: true, sidebarWidth: 190, connections: state.connections, selected: state.selected, search: state.search}
			tt := ui.NewTester(a.view, 1000, 760)
			empty := ui.NewTester(a.emptyTerminalGroup, 600, 400)
			if !empty.HasText(state.title) || !empty.HasText(state.action) || !tt.HasText(state.action) || len(a.sessions) != 0 || len(a.connecting) != 0 {
				t.Fatal("incorrect empty state or unexpected connection", tt.Texts())
			}
			if _, ok := tt.Find("终端栏标题1"); !ok {
				t.Fatal("empty terminal lost its header")
			}
			if state.action == "选择服务器" {
				if err := tt.Click(state.action); err != nil || a.sidebarHidden || !tt.Focused("搜索连接") {
					t.Fatalf("search action: %v hidden=%v focused=%v", err, a.sidebarHidden, tt.Focused("搜索连接"))
				}
				tt.Type("example")
				if a.search != state.search+"example" || len(a.connecting) != 0 {
					t.Fatal("search was not focused or started a connection", a.search)
				}
			}
			if state.action == "添加服务器" {
				if err := tt.Click(state.action); err != nil || a.page != 0 || len(a.connecting) != 0 {
					t.Fatalf("add action: %v page=%d", err, a.page)
				}
			}
		})
	}
}

func TestWorkspaceEmptySelectionUsesPublicName(t *testing.T) {
	a := &app{online: true, page: 1, selected: "opaque-id", connections: []vault.Summary{{ID: "opaque-id", Host: "192.0.2.10"}}}
	tt := ui.NewTester(a.emptyTerminalGroup, 600, 400)
	if !tt.HasText("192.0.2.10") || tt.HasText("opaque-id") || !tt.HasText("连接终端") {
		t.Fatal("empty terminal exposed an opaque identifier", tt.Texts())
	}
	a.connections[0].Name = "safe\x1b\u202eevil"
	tt.Frame()
	if !tt.HasText(`safe\x1b\u202eevil`) || tt.HasText(a.connections[0].Name) || tt.HasText("opaque-id") {
		t.Fatal("empty terminal did not make control and direction characters visible", tt.Texts())
	}
}

func TestWorkspaceConnectionRowsStayCompactInBothSidebarModes(t *testing.T) {
	for _, compact := range []bool{false, true} {
		a := &app{online: true, page: 1, connections: []vault.Summary{{ID: "a", Name: "First"}, {ID: "b", Name: "Second"}}}
		width := 220
		if compact {
			width = 48
		}
		tt := ui.NewTester(func(c *ui.Context) { a.connectionSidebar(c, compact) }, width, 400)
		first, one := tt.Find("First")
		second, two := tt.Find("Second")
		if !one || !two || first.H != 36 || second.H != 36 || second.Y-first.Y-first.H != 2 {
			t.Fatalf("compact=%v rows: %v %v", compact, first, second)
		}
	}
}
