package desktop

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"
	"sshclient/internal/desktop/components"
	"sshclient/internal/monitor"
	"sshclient/internal/vault"
)

func TestToolsCollapseMoveAndDiagnosticsPreserveTerminalSpace(t *testing.T) {
	a := &app{ctx: context.Background(), online: true, page: 1, selected: "web", groups: map[string]*bool{}, sidebarWidth: 190, connections: []vault.Summary{{ID: "web", Name: "Web", Host: "127.0.0.1"}}}
	tt := ui.NewTester(a.view, 1280, 900)
	before, ok := tt.Find("终端区域")
	if !ok || before.H < 500 {
		t.Fatal("terminal does not dominate the workspace", before)
	}
	if tt.HasText("开始监控") || tt.HasText("待执行命令") || tt.HasText(applicationName) || tt.HasText("CPU  ") {
		t.Fatal("low-frequency controls are still on the main plane", tt.Texts())
	}
	if e := tt.Click("收起工具面板"); e != nil {
		t.Fatal(e)
	}
	after, _ := tt.Find("终端区域")
	if after.H < before.H+100 {
		t.Fatal("collapsing did not return space to terminal", before, after)
	}
	if _, ok := tt.Find("工具面板"); ok {
		t.Fatal("hidden panel still consumes layout")
	}
	if e := tt.Click("显示工具面板"); e != nil {
		t.Fatal(e)
	}
	if e := tt.Click("工具面板布局"); e != nil {
		t.Fatal(e)
	}
	if e := tt.Click("放到右侧"); e != nil {
		t.Fatal(e)
	}
	panel, _ := tt.Find("工具面板")
	term, _ := tt.Find("终端区域")
	if panel.X < term.X+term.W || panel.H < term.H-5 {
		t.Fatal("right dock did not sit alongside terminal", panel, term)
	}
	a.sessions = []*session{{host: "web", stats: &monitor.Stats{Time: time.Now(), MemoryTotal: 512 << 20, MemoryUsed: 256 << 20, Inventory: &monitor.Inventory{Manager: "systemd", Services: []monitor.Service{{Name: "frp.service", Enabled: "enabled", Active: "active", Sub: "running"}}}}}}
	a.showTool(toolDiagnostics)
	tt.Frame()
	if e := tt.Click("自启服务"); e != nil {
		t.Fatal(e)
	}
	if !tt.HasText("frp.service") || !tt.HasText("运行中") {
		t.Fatal(tt.Texts())
	}
	tt.SetSize(1000, 760)
	tt.SetDark(true)
	if _, ok := tt.Find("锁定"); !ok {
		t.Fatal("overflow became inaccessible at minimum window size")
	}
}

func TestLayoutRestoreContainsNoConnectionDataAndRejectsInvalidSizes(t *testing.T) {
	a := &app{dir: t.TempDir(), toolsHidden: true, toolsRight: true, dock: toolCommands, terminalShare: 73, terminalWidthShare: 61, sidebarWidth: 180, sidebarHidden: true, selected: "private-server"}
	if e := os.Chmod(a.dir, 0700); e != nil {
		t.Fatal(e)
	}
	if e := a.saveLayout(); e != nil {
		t.Fatal(e)
	}
	data, _ := os.ReadFile(filepath.Join(a.dir, "layout.json"))
	if strings.Contains(string(data), "private-server") {
		t.Fatal("connection data leaked into preferences")
	}
	for _, retired := range []string{"approvalSidebarHidden", "settingsSidebarHidden"} {
		if strings.Contains(string(data), retired) {
			t.Fatal("layout retained a retired page-specific sidebar preference", retired)
		}
	}
	b := &app{dir: a.dir}
	b.loadLayout()
	if !b.toolsRight || !b.toolsHidden || b.terminalShare != 73 || b.dock != toolCommands || !b.sidebarHidden || b.sidebarWidth != 180 {
		t.Fatal("layout was not restored")
	}
	os.WriteFile(filepath.Join(a.dir, "layout.json"), []byte(`{"version":1,"sidebar":9999999,"terminalShare":-1,"tab":99}`), 0600)
	b = &app{dir: a.dir, sidebarWidth: 190, terminalShare: 72}
	b.loadLayout()
	if b.sidebarWidth != 190 || b.terminalShare != 72 || b.dock != toolFiles || b.sidebarHidden {
		t.Fatal("corrupt preferences broke layout")
	}
}

func TestSidebarToggleKeepsTabsPinnedFilesAndRestoreButton(t *testing.T) {
	for _, mode := range []string{"bottom", "right", "tools-hidden", "expanded"} {
		t.Run(mode, func(t *testing.T) {
			a := fileFollowDemo()
			a.sidebarWidth = 220
			a.toolsRight, a.toolsHidden, a.fileExpanded = mode == "right", mode == "tools-hidden", mode == "expanded"
			a.chooseFileEndpoint(a.primaryFilePane, "c")
			pane, first, second := a.primaryFilePane, a.sessions[0], a.sessions[1]
			tt := ui.NewTester(a.view, 1280, 900)
			before, visible := tt.Find("终端区域")
			anchor, found := tt.Find("收窄连接侧栏")
			if !found {
				t.Fatal("missing sidebar toggle")
			}
			if err := tt.Click("收窄连接侧栏"); err != nil {
				t.Fatal(err)
			}
			if _, ok := tt.Find("连接侧栏"); ok || !a.sidebarHidden || a.sidebarWidth != 220 {
				t.Fatal("hidden sidebar still occupies layout or lost its width")
			}
			if rail, ok := tt.Find("连接窄栏"); !ok || rail.X != 0 || rail.W != components.RailWidth {
				t.Fatal("connection shortcuts disappeared instead of narrowing", rail)
			}
			if restore, ok := tt.Find("展开连接侧栏"); !ok || restore != anchor {
				t.Fatal("sidebar toggle moved after collapsing", anchor, restore)
			}
			after, stillVisible := tt.Find("终端区域")
			if visible != stillVisible || visible && (after.X != components.RailWidth || after.W <= before.W || after.H != before.H) {
				t.Fatal("sidebar did not return horizontal space to the terminal", before, after)
			}
			// A second click at the unchanged pointer position must restore it.
			x, y := anchor.X+anchor.W/2, anchor.Y+anchor.H/2
			tt.Press(x, y)
			tt.Release(x, y)
			restored, _ := tt.Find("终端区域")
			if a.sidebarHidden || a.sidebarWidth != 220 || restored != before || len(a.sessions) != 2 || a.sessions[0] != first || a.sessions[1] != second || a.primaryFilePane != pane || pane.follow || pane.endpoint != "c" || pane.browser().dir != "/srv/c" {
				t.Fatal("sidebar restoration changed tabs, geometry or the pinned file target")
			}
		})
	}
}

func TestWorkspaceHeadersAlignWithEmptyAndConnectedTerminals(t *testing.T) {
	for _, width := range []int{560, 900, 1280} {
		for _, connected := range []bool{false, true} {
			a := fileFollowDemo()
			if !connected {
				a.sessions = nil
			}
			tt := ui.NewTester(a.view, width, 760)
			for _, dark := range []bool{false, true} {
				tt.SetDark(dark)
				left, l := tt.Find("连接栏标题")
				right, r := tt.Find("终端栏标题1")
				if !l || !r || left.Y != right.Y || left.H != right.H {
					t.Fatalf("headers do not share top and bottom edges (width=%d connected=%v dark=%v): %v %v", width, connected, dark, left, right)
				}
			}
		}
	}
}

func TestStandardLayoutMenuRestoresLeftAndStackedWorkspace(t *testing.T) {
	a := fileFollowDemo()
	a.sidebarHidden, a.toolsHidden, a.fileExpanded = true, true, true
	tt := ui.NewTester(a.view, 1280, 900)
	if err := tt.Click("设置"); err != nil {
		t.Fatal(err)
	}
	if err := tt.Click("恢复默认布局"); err != nil {
		t.Fatal(err)
	}
	if err := tt.Click("工作台"); err != nil {
		t.Fatal(err)
	}
	left, l := tt.Find("连接侧栏")
	center, m := tt.Find("终端区域")
	right, r := tt.Find("工具面板")
	if !l || !m || !r || a.sidebarHidden || a.toolsHidden || a.fileExpanded || a.toolsRight || left.X+left.W > center.X || center.X != right.X || center.W != right.W || center.Y+center.H > right.Y {
		t.Fatal("standard layout did not put the terminal above its tools", left, center, right)
	}
}

func TestToolsToggleIsIndependentFromConnectionSidebar(t *testing.T) {
	a := fileFollowDemo()
	a.chooseFileEndpoint(a.primaryFilePane, "c")
	first, second, pane := a.sessions[0], a.sessions[1], a.primaryFilePane
	tt := ui.NewTester(a.view, 1280, 900)
	original, _ := tt.Find("终端区域")
	if err := tt.Click("隐藏工具面板"); err != nil {
		t.Fatal(err)
	}
	full, _ := tt.Find("终端区域")
	if _, ok := tt.Find("工具面板"); ok || full.H <= original.H || full.X != original.X || full.W != original.W || a.sidebarHidden {
		t.Fatal("hiding tools changed the sidebar or did not return vertical space", original, full)
	}
	if err := tt.Click("显示工具面板"); err != nil {
		t.Fatal(err)
	}
	restored, _ := tt.Find("终端区域")
	if restored != original || a.dock != toolFiles || a.toolsRight {
		t.Fatal("restore lost the stacked workspace", original, restored)
	}
	if err := tt.Click("收起工具面板"); err != nil {
		t.Fatal(err)
	}
	if err := tt.Click("收窄连接侧栏"); err != nil {
		t.Fatal(err)
	}
	if _, ok := tt.Find("显示工具面板"); !ok || !a.sidebarHidden || !a.toolsHidden {
		t.Fatal("both collapsed panels lost their restore controls")
	}
	if err := tt.Click("显示工具面板"); err != nil {
		t.Fatal(err)
	}
	if !a.sidebarHidden || a.toolsHidden {
		t.Fatal("restoring tools also restored the sidebar")
	}
	if err := tt.Click("展开连接侧栏"); err != nil {
		t.Fatal(err)
	}
	if a.sidebarHidden || a.toolsHidden || a.sessions[0] != first || a.sessions[1] != second || a.primaryFilePane != pane || pane.follow || pane.endpoint != "c" || pane.browser().dir != "/srv/c" {
		t.Fatal("independent toggles changed sessions or the pinned file window")
	}
}

func TestLayoutIgnoresRetiredPageSidebarPreferences(t *testing.T) {
	for _, version := range []int{1, 2} {
		for _, preference := range []struct {
			name, fields string
			hidden       bool
		}{
			{"expanded", `"sidebarHidden":false,"approvalSidebarHidden":true,"settingsSidebarHidden":true`, false},
			{"collapsed", `"sidebarHidden":true,"approvalSidebarHidden":false,"settingsSidebarHidden":false`, true},
			{"omitted", `"approvalSidebarHidden":true,"settingsSidebarHidden":true`, false},
		} {
			t.Run(fmt.Sprintf("v%d/%s", version, preference.name), func(t *testing.T) {
				a := &app{dir: t.TempDir(), sidebarHidden: true}
				if err := os.Chmod(a.dir, 0700); err != nil {
					t.Fatal(err)
				}
				data := fmt.Sprintf(`{"version":%d,%s}`, version, preference.fields)
				if err := os.WriteFile(filepath.Join(a.dir, "layout.json"), []byte(data), 0600); err != nil {
					t.Fatal(err)
				}
				a.loadLayout()
				if a.sidebarHidden != preference.hidden {
					t.Fatal("retired page preferences overrode the shared sidebar state", a.sidebarHidden)
				}
				if err := a.saveLayout(); err != nil {
					t.Fatal(err)
				}
				b := &app{dir: a.dir, sidebarHidden: !preference.hidden}
				b.loadLayout()
				if b.sidebarHidden != preference.hidden {
					t.Fatal("saving and reloading lost the shared sidebar state", b.sidebarHidden)
				}
			})
		}
	}
}

func TestPreviousLayoutMigratesToStackedWorkspaceOnce(t *testing.T) {
	a := &app{dir: t.TempDir(), sidebarWidth: 190}
	if err := os.Chmod(a.dir, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(a.dir, "layout.json"), []byte(`{"version":1,"right":true,"sidebarHidden":true,"sidebar":220,"terminalShare":68,"tab":3}`), 0600); err != nil {
		t.Fatal(err)
	}
	a.loadLayout()
	if a.toolsRight || !a.sidebarHidden || a.sidebarWidth != 220 || a.terminalShare != 68 || a.dock != toolDiagnostics {
		t.Fatal("previous layout did not migrate without losing other preferences")
	}
	// A later explicit dock choice in the current format remains respected.
	a.toolsRight = true
	if err := a.saveLayout(); err != nil {
		t.Fatal(err)
	}
	b := &app{dir: a.dir}
	b.loadLayout()
	if !b.toolsRight || !b.sidebarHidden || b.sidebarWidth != 220 {
		t.Fatal("migration kept overriding a later user choice")
	}
}

func TestAppMenuDoesNotReserveBareTerminalControlShortcuts(t *testing.T) {
	a := &app{}
	for _, group := range a.menuGroups() {
		for _, action := range group.actions {
			if strings.HasPrefix(action.shortcut, "CmdOrCtrl+") && !strings.Contains(action.shortcut, "Shift+") {
				t.Fatal("terminal Ctrl key intercepted on Windows/Linux", action.shortcut)
			}
		}
	}
}
