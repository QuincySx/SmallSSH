package desktop

import (
	"context"
	"errors"
	"fmt"
	"image"
	"reflect"
	"slices"
	"strings"
	"testing"

	"github.com/egoist/mygo/ui"
	"sshclient/internal/broker"
	"sshclient/internal/desktop/components"
	"sshclient/internal/protocol"
	"sshclient/internal/sshconn"
	"sshclient/internal/vault"
)

func TestConnectionRailSelectsWithoutConnecting(t *testing.T) {
	a := fileFollowDemo()
	a.connections[0].Group, a.connections[1].Group = "生产", "测试"
	a.files = []sshconn.File{{Name: "unchanged.txt"}}
	a.remotePath, a.listedPath, a.editShown = "/keep", "/keep", true
	first, second, pane := a.sessions[0], a.sessions[1], a.primaryFilePane
	tt := ui.NewTester(a.connectionRail, int(components.RailWidth), 360)
	for _, name := range []string{"连接 A", "连接 B", "连接 C"} {
		bounds, ok := tt.Find(name)
		if !ok || bounds.W != 36 || bounds.H != connectionRailItemHeight || bounds.X < 0 || bounds.X+bounds.W > components.RailWidth {
			t.Fatal("connection lost its clickable avatar", name, bounds)
		}
	}
	for _, group := range []string{"生产", "测试", "未分组"} {
		if _, ok := tt.Find("连接分组：" + group); !ok {
			t.Fatal("group separator missing", group)
		}
	}
	if err := tt.Click("连接 B"); err != nil {
		t.Fatal(err)
	}
	if a.selected != "b" || a.active != 0 || len(a.connecting) != 0 || len(a.sessions) != 2 || a.sessions[0] != first || a.sessions[1] != second || a.primaryFilePane != pane || pane.endpoint != "a" || a.remotePath != "/keep" || a.listedPath != "/keep" || !a.editShown || len(a.files) != 1 {
		t.Fatal("single avatar click changed terminal, files or connection state", a.selected, a.active, pane.endpoint)
	}
	if got := connectionRailTooltip(vault.Summary{Name: "生产 Web", User: "ops", Host: "192.0.2.1"}); got != "生产 Web\nops@192.0.2.1" {
		t.Fatal("avatar tooltip lost connection identity", got)
	}
}

func TestConnectionRailDoubleClickStartsNewTerminalAndNavigationGuard(t *testing.T) {
	for _, dirty := range []bool{false, true} {
		t.Run(fmt.Sprint(dirty), func(t *testing.T) {
			a := fileFollowDemo()
			if dirty {
				a.newConnection()
				a.form.Name = "keep my unsaved connection"
			}
			tt := ui.NewTester(a.connectionRail, int(components.RailWidth), 360)
			if err := tt.Click("连接 B"); err != nil {
				t.Fatal(err)
			}
			if a.active != 0 || len(a.connecting) != 0 {
				t.Fatal("first click connected")
			}
			if err := tt.Click("连接 B"); err != nil {
				t.Fatal(err)
			}
			if dirty {
				if !a.prompt.open || a.prompt.title != "放弃未保存的连接？" || a.page != 0 || a.active != 0 || a.form.Name != "keep my unsaved connection" {
					t.Fatal("double click bypassed unsaved navigation guard")
				}
				return
			}
			// Without an unlocked broker, a new terminal reaches the normal
			// authorization guard instead of silently reusing the existing tab.
			if a.active != 0 || a.primaryFilePane.endpoint != "a" || a.selected != "b" || a.page != 1 || len(a.sessions) != 2 || len(a.connecting) != 0 || !strings.Contains(a.status, "保险库已锁定") {
				t.Fatal("double click reused an existing tab or bypassed authorization", a.active, a.primaryFilePane.endpoint, a.status)
			}
		})
	}
}

func TestConnectionRailEnterKeepsUnsavedConnectionGuard(t *testing.T) {
	a := fileFollowDemo()
	a.newConnection()
	a.form.Name = "keep my unsaved connection"
	tt := ui.NewTester(a.connectionRail, int(components.RailWidth), 360)
	if err := tt.Click("连接 B"); err != nil {
		t.Fatal(err)
	}
	tt.Key(0, ui.KeyEnter)
	if !a.prompt.open || a.prompt.title != "放弃未保存的连接？" || a.page != 0 || len(a.sessions) != 2 || a.form.Name != "keep my unsaved connection" {
		t.Fatal("Enter bypassed the unsaved navigation guard", a.prompt.open, a.page)
	}
}

func TestConnectionRailKeepsConnectionContextActions(t *testing.T) {
	a := fileFollowDemo()
	cancelled := false
	a.connecting = map[string]context.CancelFunc{"b": func() { cancelled = true }}
	tt := ui.NewTester(a.connectionRail, int(components.RailWidth), 360)
	if err := tt.RightClick("连接 B"); err != nil {
		t.Fatal(err)
	}
	want := []string{"取消连接", "新建终端标签", "终端连接", "新建端口转发…", "VNC 远程桌面…", "编辑连接", "AI 访问…", "备注…", "删除连接"}
	if !reflect.DeepEqual(tt.Menu(), want) {
		t.Fatal("compact menu differs from expanded connection actions", tt.Menu())
	}
	if err := tt.ChooseMenuItem("取消连接"); err != nil || !cancelled {
		t.Fatal("cancel connection action missing", err)
	}
	if err := tt.RightClick("连接 B"); err != nil {
		t.Fatal(err)
	}
	if err := tt.ChooseMenuItem("新建端口转发…"); err != nil || !a.tunnels.open || a.tunnels.host != "b" {
		t.Fatal("tunnel manager did not bind the selected server", err)
	}
	a.tunnels.open = false
	if err := tt.RightClick("连接 B"); err != nil {
		t.Fatal(err)
	}
	if err := tt.ChooseMenuItem("编辑连接"); err != nil || a.page != 0 || a.details == nil || a.details.ID != "b" {
		t.Fatal("edit did not use connection navigation", err)
	}
}

func TestConnectionRailVirtualizesAndScrollsAllConnections(t *testing.T) {
	for _, compact := range []bool{false, true} {
		t.Run(fmt.Sprint(compact), func(t *testing.T) {
			a := &app{ctx: context.Background(), online: true, page: 1}
			for i := 0; i < 10000; i++ {
				a.connections = append(a.connections, vault.Summary{ID: fmt.Sprint(i), Name: fmt.Sprintf("rail-host-%05d", i), Group: "大组", Host: "192.0.2.1", User: "ops"})
			}
			width := 248
			if compact {
				width = int(components.RailWidth)
			}
			tt := ui.NewTester(func(c *ui.Context) { a.connectionSidebar(c, compact) }, width, 360)
			visible := map[string]bool{}
			for _, label := range tt.Texts() {
				if strings.HasPrefix(label, "rail-host-") {
					visible[label] = true
				}
			}
			if len(visible) < 3 || len(visible) > 30 {
				t.Fatal("sidebar did not bound its visible avatar rows", len(visible))
			}
			tt.Scroll(24, 180, 0, 20000)
			if bounds, ok := tt.Find("rail-host-00000"); ok && bounds.H > 0 {
				t.Fatal("sidebar did not advance its scroll viewport")
			}
			a.connectionRailList.ScrollToEnd()
			tt.Frame()
			if err := tt.Click("rail-host-09999"); err != nil || a.selected != "9999" || len(a.connecting) != 0 {
				t.Fatal("last saved connection is not selectable", err, a.selected)
			}
		})
	}
}

func TestConnectionRailGroupOrderPreservesExpandedConnectionOrder(t *testing.T) {
	rows := connectionRailRows([]vault.Summary{{ID: "z1", Group: "z"}, {ID: "a2", Group: "a"}, {ID: "z2", Group: "z"}, {ID: "a1", Group: "a"}, {ID: "none"}}, "", nil)
	got := []string{}
	for _, row := range rows {
		if row.header {
			got = append(got, "group:"+row.group)
		} else {
			got = append(got, row.connection.ID)
		}
	}
	want := []string{"group:z", "z1", "z2", "group:a", "a2", "a1", "group:", "none"}
	if !reflect.DeepEqual(got, want) {
		t.Fatal("rail reordered expanded groups or their connections", got)
	}
}

func TestConnectionRailKeyboardSkipsGroupsAndTraversesVirtualViewport(t *testing.T) {
	a := fileFollowDemo()
	a.connections = nil
	for i := 0; i < 45; i++ {
		a.connections = append(a.connections, vault.Summary{ID: fmt.Sprintf("keyboard-%02d", i), Name: fmt.Sprintf("键盘连接 %02d", i), Group: fmt.Sprintf("组 %d", i/15), User: "ops", Host: "192.0.2.1"})
	}
	a.selected = "keyboard-00"
	tt := ui.NewTester(func(c *ui.Context) {
		ui.Row(c).Fill().Children(func() {
			a.connectionRail(c)
			ui.Button(c, "下一控件")
		})
	}, 220, 260)
	if err := tt.Click("键盘连接 00"); err != nil {
		t.Fatal(err)
	}
	for i := 1; i <= 15; i++ {
		tt.Key(0, ui.KeyDown)
		if a.selected != fmt.Sprintf("keyboard-%02d", i) {
			t.Fatal("Down did not select the next connection across virtual rows", i, a.selected)
		}
	}
	if bounds, ok := tt.Find("键盘连接 15"); !ok || bounds.H != connectionRailItemHeight {
		t.Fatal("keyboard selection did not scroll through the group separator", bounds)
	}
	tt.Key(0, ui.KeyUp)
	if a.selected != "keyboard-14" {
		t.Fatal("Up stopped at a group separator", a.selected)
	}
	tt.Key(0, ui.KeyEnd)
	if bounds, ok := tt.Find("键盘连接 44"); a.selected != "keyboard-44" || !ok || bounds.H != connectionRailItemHeight {
		t.Fatal("End did not reach the last virtual connection", a.selected, bounds)
	}
	if a.status != "" || a.active != 0 || a.contentFocus || a.primaryFilePane.endpoint != "a" || len(a.sessions) != 2 || len(a.connecting) != 0 {
		t.Fatal("keyboard navigation connected or changed the workspace")
	}
	// Enter opens a new terminal like a double click. Without an unlocked
	// broker it reaches the normal authorization guard, not an old tab.
	tt.Key(0, ui.KeyEnter)
	if a.selected != "keyboard-44" || a.page != 1 || len(a.sessions) != 2 || len(a.connecting) != 0 || !strings.Contains(a.status, "保险库已锁定") {
		t.Fatal("Enter did not start a new terminal for the selected server", a.selected, a.status)
	}
	a.status = ""
	tt.Key(0, ui.KeyEnd)
	tt.Key(0, ui.KeyTab)
	if !tt.Focused("下一控件") {
		t.Fatal("Tab walked the visible avatars instead of leaving the rail")
	}
	tt.Key(ui.Shift, ui.KeyTab)
	if !tt.Focused("连接头像列表") {
		t.Fatal("Tab did not restore the rail's single focus stop")
	}
	tt.Key(0, ui.KeyHome)
	if bounds, ok := tt.Find("键盘连接 00"); a.selected != "keyboard-00" || !ok || bounds.H != connectionRailItemHeight {
		t.Fatal("Home did not return to the first virtual connection", a.selected, bounds)
	}
	tt.Key(0, ui.KeyUp)
	if a.selected != "keyboard-00" {
		t.Fatal("Up moved before the first connection", a.selected)
	}

	tt.Key(0, ui.KeyEnd)
	a.connections[44].Group = "0"
	tt.Frame()
	if a.selected != "keyboard-44" {
		t.Fatal("regrouping changed selected identity")
	}
	tt.Key(0, ui.KeyDown)
	if a.selected != "keyboard-44" {
		t.Fatal("navigation used a stale row index after in-place regrouping", a.selected)
	}
	refresh := slices.Clone(a.connections)
	refresh[0].Group = "重新分组"
	a.connections = refresh
	tt.Frame()
	tt.Key(0, ui.KeyUp)
	if a.selected != "keyboard-43" {
		t.Fatal("navigation used a stale row index after a refreshed group order", a.selected)
	}
}

func TestConnectionRailModelReusesUnchangedRowsAndRebuildsEditedSnapshots(t *testing.T) {
	connections := []vault.Summary{
		{ID: "one", Name: "First", Group: "a", User: "ops", Host: "192.0.2.1"},
		{ID: "two", Name: "Second", Group: "b", User: "ops", Host: "192.0.2.2"},
	}
	var model connectionRailModel
	first := model.rowsFor(connections, "", nil)
	if again := model.rowsFor(connections, "", nil); &again[0] != &first[0] {
		t.Fatal("unchanged input rebuilt the flattened rows")
	}
	if equalRefresh := model.rowsFor(slices.Clone(connections), "", nil); &equalRefresh[0] != &first[0] {
		t.Fatal("equal refresh rebuilt the flattened rows")
	}
	if allocations := testing.AllocsPerRun(100, func() { model.rowsFor(connections, "", nil) }); allocations != 0 {
		t.Fatal("unchanged rows allocated", allocations)
	}

	connections[0].Name = "Renamed in place"
	renamed := model.rowsFor(connections, "", nil)
	if &renamed[0] == &first[0] || renamed[1].connection.Name != "Renamed in place" || first[1].connection.Name != "First" {
		t.Fatal("in-place rename was not captured by an independent snapshot")
	}
	connections[0].Group = "z"
	regrouped := model.rowsFor(connections, "", nil)
	if &regrouped[0] == &renamed[0] || regrouped[0].group != "z" || regrouped[2].group != "b" || regrouped[1].connection.ID != "one" {
		t.Fatal("in-place group edit did not rebuild ordered rows")
	}

	refresh := slices.Clone(connections)
	refresh[0].Host = "192.0.2.10"
	replaced := model.rowsFor(refresh, "", nil)
	if &replaced[0] == &regrouped[0] || replaced[1].connection.Host != "192.0.2.10" || model.source[0].Host != "192.0.2.10" {
		t.Fatal("same-length changed refresh retained stale connection identity")
	}
	refresh[0].Name = "Changed after refresh"
	if model.source[0].Name == refresh[0].Name {
		t.Fatal("cached source aliases the caller's refresh slice")
	}
}

// Render either width through the same parent and keep the list alive between
// frames. Item bounds and the actual avatar pixels must stay at the same place.
func TestConnectionSidebarModeChangesPreserveAvatarGeometryAndScroll(t *testing.T) {
	a := &app{ctx: context.Background(), online: true, page: 1, selected: "stable-002"}
	for i := 0; i < 100; i++ {
		a.connections = append(a.connections, vault.Summary{ID: fmt.Sprintf("stable-%03d", i), Name: fmt.Sprintf("稳定连接 %03d", i), Group: fmt.Sprintf("组 %d", i/20), Host: "192.0.2.1"})
	}
	compact := false
	tt := ui.NewTester(func(c *ui.Context) {
		width := float32(248)
		if compact {
			width = components.RailWidth
		}
		ui.Row(c).Fill().Children(func() {
			ui.Column(c).Width(width).FillHeight().Shrink(0).Children(func() { a.connectionSidebar(c, compact) })
			ui.Box(c).Grow(1).FillHeight()
		})
	}, 600, 350)
	checkToggle := func() {
		t.Helper()
		before := map[string]ui.Rect{}
		for _, label := range tt.Texts() {
			if strings.HasPrefix(label, "稳定连接 ") || strings.HasPrefix(label, "连接分组：") || label == "连接搜索栏" {
				if bounds, ok := tt.Find(label); ok && bounds.W > 0 && bounds.H > 0 {
					before[label] = bounds
				}
			}
		}
		pixels := tt.Image()
		first, last := a.connectionRailList.Visible()
		selected := a.selected
		selectedLabel := ""
		for _, connection := range a.connections {
			if connection.ID == selected {
				selectedLabel = connection.Name
				break
			}
		}
		compact = !compact
		tt.Frame()
		afterPixels := tt.Image()
		for label, was := range before {
			now, ok := tt.Find(label)
			if !ok || now.Y != was.Y || now.H != was.H {
				t.Fatalf("mode change moved %q: %v -> %v (%v)", label, was, now, ok)
			}
			if strings.HasPrefix(label, "稳定连接 ") {
				beforeAvatarX, afterAvatarX := was.X+4, now.X+4
				if beforeAvatarX != afterAvatarX {
					t.Fatal("avatar moved between sidebar modes", label, beforeAvatarX, afterAvatarX)
				}
				// Visible edge rows may be clipped by the virtual viewport.
				if now.H > connectionRailItemHeight+0.01 {
					t.Fatal("connection item height changed", label, now)
				}
				// Selection feedback changes from an expanded row to the compact
				// circle. Its target geometry and selected ID are still checked.
				if label == selectedLabel {
					continue
				}
				// Partial edge rows can intersect the rounded sidebar mask at a
				// different horizontal position. Compare complete avatar slots.
				if was.H < connectionRailItemHeight-.01 || now.H < connectionRailItemHeight-.01 {
					continue
				}
				// Unselected avatars keep exactly the same pixels in their 36px slot.
				top := int(was.Y + (connectionRailItemHeight-28)/2)
				crop := image.Rect(int(beforeAvatarX)+1, top+1, int(beforeAvatarX)+27, top+27)
				for y := max(crop.Min.Y, 0); y < min(crop.Max.Y, pixels.Bounds().Max.Y); y++ {
					for x := crop.Min.X; x < crop.Max.X; x++ {
						if pixels.RGBAAt(x, y) != afterPixels.RGBAAt(x, y) {
							t.Fatalf("mode change replaced or moved avatar pixels for %q at %d,%d", label, x, y)
						}
					}
				}
			}
		}
		if nowFirst, nowLast := a.connectionRailList.Visible(); nowFirst != first || nowLast != last || a.selected != selected {
			t.Fatal("mode change altered virtual scroll range or selection", first, last, nowFirst, nowLast, selected, a.selected)
		}
	}
	checkToggle()
	checkToggle()
	tt.Scroll(24, 190, 0, 1100)
	// Park the pointer off the list: a hovered row is faded and tinted on purpose,
	// and MyGo keeps the row under the pointer hovered after a scroll instead of
	// dropping the hover the way v0.5.5 did. The pixel check below is about the
	// avatar slot, not about hover.
	tt.Move(560, 20)
	checkToggle()
	checkToggle()
	a.connectionRailList.ScrollToEnd()
	tt.Frame()
	checkToggle()
	checkToggle()
}

func TestConnectionSidebarSharesSearchAndCollapsedGroupsAcrossModes(t *testing.T) {
	a := fileFollowDemo()
	a.connections[0].Group, a.connections[1].Group, a.connections[2].Group = "生产", "测试", "测试"
	closed := false
	a.groups = map[string]*bool{"生产": &closed}
	a.search = "连接"
	compact := false
	tt := ui.NewTester(func(c *ui.Context) { a.connectionSidebar(c, compact) }, 248, 360)
	if _, ok := tt.Find("连接 A"); ok {
		t.Fatal("expanded sidebar ignored group collapse")
	}
	if _, ok := tt.Find("连接 B"); !ok {
		t.Fatal("expanded sidebar lost matching connection")
	}
	rows := a.connectionRailCache.rows
	compact = true
	tt.SetSize(int(components.RailWidth), 360)
	tt.Frame()
	if _, ok := tt.Find("连接 A"); ok {
		t.Fatal("compact sidebar ignored shared group collapse")
	}
	if _, ok := tt.Find("连接 B"); !ok || &a.connectionRailCache.rows[0] != &rows[0] {
		t.Fatal("mode change rebuilt or changed the shared connection rows")
	}
	if err := tt.Click("连接分组：生产"); err != nil || !closed {
		t.Fatal("compact group separator did not preserve group expansion action", err)
	}
	if _, ok := tt.Find("连接 A"); !ok {
		t.Fatal("compact group expansion did not reveal its connection")
	}
	a.search = "连接 B"
	tt.Frame()
	for _, label := range []string{"连接 A", "连接 C"} {
		if _, ok := tt.Find(label); ok {
			t.Fatal("compact sidebar ignored shared search", label)
		}
	}
	compact = false
	tt.SetSize(248, 360)
	tt.Frame()
	if _, ok := tt.Find("连接 B"); !ok || a.search != "连接 B" || !closed {
		t.Fatal("expanded sidebar lost search or group expansion state")
	}
}

func TestCompactConnectionSearchExpandsAndFocusesSharedInput(t *testing.T) {
	a := fileFollowDemo()
	a.sidebarHidden = true
	tt := ui.NewTester(func(c *ui.Context) { a.connectionSidebar(c, a.sidebarHidden) }, 248, 360)
	if err := tt.Click("搜索连接"); err != nil {
		t.Fatal(err)
	}
	if a.sidebarHidden || !tt.Focused("搜索连接") {
		t.Fatal("compact search did not expand and focus the search input", a.sidebarHidden)
	}
	tt.Type("连接 B")
	if a.search != "连接 B" {
		t.Fatal("search input did not receive typing after expansion", a.search)
	}
	if _, ok := tt.Find("连接 B"); !ok {
		t.Fatal("focused expanded search lost matching connection")
	}
	main := ui.NewTester(a.view, 1000, 760)
	selected := a.selected
	if err := main.Click("设置"); err != nil {
		t.Fatal(err)
	}
	settings := ui.NewTester(a.settingsWindowView, 1080, 760)
	if err := settings.Click("设置分类：授权中心"); err != nil {
		t.Fatal(err)
	}
	if a.settingsSection != settingsApprovals || !a.settingsOpen {
		t.Fatal("approval category did not open in settings")
	}
	if err := settings.Click("关闭设置"); err != nil {
		t.Fatal(err)
	}
	main.Frame()
	if _, found := main.Find("连接侧栏"); !found || a.sidebarHidden || a.search != "连接 B" || a.selected != selected {
		t.Fatal("settings changed expanded search or selection")
	}
	if err := main.Click("搜索连接"); err != nil || !main.Focused("搜索连接") {
		t.Fatal("search input lost focusability after settings", err)
	}
}

func TestConnectionSidebarModelTracksSearchAndInPlaceGroupExpansion(t *testing.T) {
	connections := []vault.Summary{{ID: "a", Name: "First", Group: "one"}, {ID: "b", Name: "Second", Group: "two"}}
	closed := false
	groups := map[string]*bool{"one": &closed}
	var model connectionRailModel
	first := model.rowsFor(connections, "", groups)
	if len(first) != 3 || !first[0].header || first[1].group != "two" || first[2].connection.ID != "b" {
		t.Fatal("collapsed group was not omitted from shared rows", first)
	}
	if again := model.rowsFor(connections, "", groups); &again[0] != &first[0] {
		t.Fatal("unchanged search/group state rebuilt rows")
	}
	if allocations := testing.AllocsPerRun(100, func() { model.rowsFor(connections, "", groups) }); allocations != 0 {
		t.Fatal("unchanged shared filter/group state allocated", allocations)
	}
	closed = true
	opened := model.rowsFor(connections, "", groups)
	if len(opened) != 4 || opened[1].connection.ID != "a" {
		t.Fatal("in-place group expansion did not invalidate cached rows", opened)
	}
	filtered := model.rowsFor(connections, "SECOND", groups)
	if len(filtered) != 2 || filtered[1].connection.ID != "b" {
		t.Fatal("shared search did not invalidate cached rows", filtered)
	}
}

func TestConnectionRailFirstSnapshotReplacesLoading(t *testing.T) {
	h := newConnectionFlowHarness(t)
	a := h.a
	a.page = 1
	a.pollWake = make(chan struct{}, 1)
	p := h.fixture.Profile()
	p.ID, p.Name = "bridge", "bridge"
	if err := h.server.Vault.SaveProfile(broker.VaultPath(h.dir), p); err != nil {
		t.Fatal(err)
	}
	a.startUnlockedSession()
	h.ui = ui.NewTester(a.connectionTree, 230, 400)
	if _, ok := h.ui.Find("正在加载连接…"); !ok {
		t.Fatal("first workspace frame did not show the loading state")
	}
	epoch := a.epoch.Load()
	a.applyConnectionSnapshot(epoch, 0, protocol.Message{}, errors.New("temporary local read failure"))
	h.ui.Frame()
	if _, ok := h.ui.Find("无法加载连接，正在重试…"); !ok {
		t.Fatal("failed first read looked like an empty saved list")
	}
	go a.poll()
	h.wait(func() bool { return !a.connectionRailCache.loading })
	if _, ok := h.ui.Find("bridge"); !ok {
		t.Fatal("successful retry did not show the saved connection", a.connections)
	}

	for _, compact := range []bool{false, true} {
		a.connections, a.hosts = nil, nil
		a.connectionRailCache.loading = true
		width := 230
		if compact {
			width = int(components.RailWidth)
		}
		tt := ui.NewTester(func(c *ui.Context) { a.connectionSidebar(c, compact) }, width, 400)
		empty := ui.NewTester(a.emptyTerminalGroup, 500, 400)
		if empty.HasText("添加你的第一台服务器") {
			t.Fatal("loading showed the empty vault prompt")
		}
		if _, ok := tt.Find("正在加载连接…"); !ok {
			t.Fatal("missing loading skeleton", compact)
		}
		a.applyConnectionSnapshot(epoch, 0, protocol.Message{}, nil)
		tt.Frame()
		empty.Frame()
		if !empty.HasText("添加你的第一台服务器") {
			t.Fatal("the empty vault lost its add-server prompt")
		}
		if _, ok := tt.Find("正在加载连接…"); ok {
			t.Fatal("a genuinely empty vault kept showing loading", compact)
		}
	}
}
