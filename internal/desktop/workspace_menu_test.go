package desktop

import (
	"context"
	"fmt"
	"testing"

	"github.com/egoist/mygo/ui"
	"github.com/egoist/mygo/ui/editor"
	"sshclient/internal/approval"
	"sshclient/internal/desktop/components"
)

func TestPrimaryPagesUseWholeContentAndRetainWorkspace(t *testing.T) {
	a := fileFollowDemo()
	a.ensureFileLayout()
	a.chooseFileEndpoint(a.primaryFilePane, "c")
	a.fileExpanded = true
	pane, layout, first, second := a.primaryFilePane, a.fileLayout, a.sessions[0], a.sessions[1]
	width := a.sidebarWidth
	tt := ui.NewTester(a.view, 1000, 760)
	for _, next := range []struct {
		label string
		page  int
	}{{"授权中心", 3}, {"设置", 4}, {"授权中心", 3}, {"工作台", 1}} {
		if err := tt.Click(next.label); err != nil {
			t.Fatal(err)
		}
		if a.page != next.page {
			t.Fatal("navigation selected the wrong page", next.label, a.page)
		}
		if next.page != 1 {
			for _, label := range []string{"连接栏标题", "搜索连接", "终端区域", "文件列表"} {
				if _, found := tt.Find(label); found {
					t.Fatal("workbench remained mounted on another page", next.label, label)
				}
			}
			content, found := tt.Find("主内容")
			if !found || content.X != 0 || content.W != 1000 {
				t.Fatal("primary page did not use the full content width", next.label, content)
			}
		}
		if a.primaryFilePane != pane || a.fileLayout != layout || a.sessions[0] != first || a.sessions[1] != second || pane.follow || pane.endpoint != "c" || pane.browser().dir != "/srv/c" || a.sidebarWidth != width || !a.fileExpanded {
			t.Fatal("switching pages discarded workspace state", next.label)
		}
	}
	if _, found := tt.Find("连接栏标题"); !found {
		t.Fatal("returning did not restore the workbench")
	}
}

func TestWorkspaceRecoveryLivesInSettingsAndLockIsDirect(t *testing.T) {
	a := fileFollowDemo()
	a.sidebarHidden, a.toolsHidden, a.toolsRight = true, true, true
	tt := ui.NewTester(a.view, 1000, 760)
	for _, label := range []string{"应用菜单", "视图菜单", "恢复默认布局"} {
		if _, found := tt.Find(label); found {
			t.Fatal("workbench contains an unrelated menu or settings action", label)
		}
	}
	if _, found := tt.Find("锁定"); !found {
		t.Fatal("lock has no direct entry")
	}
	if err := tt.Click("设置"); err != nil {
		t.Fatal(err)
	}
	header, _ := tt.Find("全局导航栏")
	recovery, found := tt.Find("恢复默认布局")
	if !found || recovery.Y < header.Y+header.H {
		t.Fatal("layout recovery is not in settings content", recovery)
	}
	if err := tt.Click("恢复默认布局"); err != nil {
		t.Fatal(err)
	}
	if a.page != 4 || a.sidebarHidden || a.toolsHidden || a.toolsRight || a.contentFocus {
		t.Fatal("settings recovery changed page or stole hidden terminal focus")
	}
	if err := tt.Click("工作台"); err != nil {
		t.Fatal(err)
	}
	if _, found := tt.Find("连接栏标题"); !found {
		t.Fatal("recovery lost the workbench sidebar")
	}
}

func TestPrimaryNavigationSupportsKeyboardWithoutAnAppMenu(t *testing.T) {
	a := fileFollowDemo()
	tt := ui.NewTester(a.workspaceHeader, 560, 60)
	if err := tt.Click("工作台"); err != nil {
		t.Fatal(err)
	}
	for _, next := range []struct {
		key  ui.Key
		page int
	}{
		{ui.KeyRight, 3}, {ui.KeyRight, 4}, {ui.KeyLeft, 3}, {ui.KeyLeft, 1},
	} {
		tt.Key(0, next.key)
		if a.page != next.page {
			t.Fatal("keyboard did not switch the primary page", next.page, a.page)
		}
	}
	for _, label := range []string{"应用菜单", "视图菜单"} {
		if _, found := tt.Find(label); found {
			t.Fatal("obsolete menu returned", label)
		}
	}
}

func TestWorkspaceHeaderStaysSingleAndControlsFollowTheirPage(t *testing.T) {
	for _, width := range []int{560, 1280} {
		for _, mode := range []string{"terminal", "approval", "settings", "expanded", "sidebar-hidden", "terminal-split"} {
			t.Run(fmt.Sprintf("%d/%s", width, mode), func(t *testing.T) {
				a := fileFollowDemo()
				switch mode {
				case "approval":
					a.page = 3
				case "settings":
					a.page = 4
				case "expanded":
					a.fileExpanded = true
				case "sidebar-hidden":
					a.sidebarHidden = true
				case "terminal-split":
					a.ensureTerminalGroups()
					a.splitExistingTerminal(a.sessions[1])
				}
				tt := ui.NewTester(a.view, width, 760)
				for _, dark := range []bool{false, true} {
					tt.SetDark(dark)
					header, ok := tt.Find("全局导航栏")
					if !ok || header.Y != 0 || header.H != 36 || header.W != float32(width) {
						t.Fatal("global header moved", header)
					}
					count := 0
					for _, text := range tt.Texts() {
						if text == "全局导航栏" {
							count++
						}
					}
					if count != 1 {
						t.Fatal("global navigation was duplicated", count)
					}
					for _, label := range []string{"应用菜单", "视图菜单"} {
						if _, found := tt.Find(label); found {
							t.Fatal("obsolete menu returned", label)
						}
					}
					controls := []string{"工作台", "授权中心", "设置", "传输任务", "锁定", a.pageSidebarLabel(a.workspaceNavigationPage())}
					if a.workspaceNavigationPage() == 1 {
						controls = append(controls, "隐藏工具面板")
					} else {
						for _, label := range []string{"收窄连接侧栏", "展开连接侧栏", "隐藏工具面板", "显示工具面板"} {
							if r, found := tt.Find(label); found && r.Y < header.H {
								t.Fatal("workbench controls leaked into another primary page", label, r)
							}
						}
					}
					sidebar, _ := tt.Find(a.pageSidebarLabel(a.workspaceNavigationPage()))
					if sidebar.X != 6 || sidebar.W != 30 || sidebar.H != 30 {
						t.Fatal("the shared sidebar control moved away from the top left", sidebar)
					}
					for _, label := range controls {
						r, found := tt.Find(label)
						if !found || r.W < 24 || r.X < 0 || r.X+r.W > float32(width)+1 || r.Y < 0 || r.Y+r.H > header.H+1 {
							t.Fatal("global control clipped", label, r, header)
						}
					}
				}
			})
		}
	}
}

func TestPrimaryNavigationPreservesDraftAndDirectLockConfirmation(t *testing.T) {
	a := &app{ctx: context.Background(), online: true, page: 0, port: "22"}
	a.connection.returnPage = 4
	a.form.Name, a.form.Password = "未保存连接", "synthetic secret"
	if a.workspaceNavigationPage() != 4 {
		t.Fatal("connection editor lost its parent page")
	}
	tt := ui.NewTester(func(c *ui.Context) {
		ui.Column(c).Fill().Children(func() { a.workspaceHeader(c); a.promptView(c) })
	}, 1000, 760)
	if err := tt.Click("授权中心"); err != nil {
		t.Fatal(err)
	}
	if a.page != 0 || !a.prompt.open || a.form.Password == "" {
		t.Fatal("navigation bypassed unsaved connection protection")
	}
	if err := tt.Click("取消"); err != nil {
		t.Fatal(err)
	}
	if a.page != 0 || a.form.Name != "未保存连接" || a.form.Password == "" {
		t.Fatal("cancel lost connection draft")
	}
	if err := tt.Click("授权中心"); err != nil {
		t.Fatal(err)
	}
	if err := tt.Click("放弃并继续"); err != nil {
		t.Fatal(err)
	}
	if a.page != 3 || a.form.Password != "" {
		t.Fatal("confirmed navigation did not clean the discarded draft")
	}

	a = fileFollowDemo()
	a.edit = &editor.State{Buffer: editor.NewBuffer("saved")}
	a.editPath = "/tmp/example.txt"
	a.edit.Insert("changed")
	tt = ui.NewTester(a.view, 1280, 900)
	if err := tt.Click("锁定"); err != nil {
		t.Fatal(err)
	}
	if !a.online || !a.prompt.open || a.prompt.title != "锁定前处理未保存修改" {
		t.Fatal("direct lock bypassed the modified-editor confirmation")
	}
	if err := tt.Click("取消"); err != nil {
		t.Fatal(err)
	}
	if !a.online || !a.edit.Buffer.Modified() {
		t.Fatal("cancelling lock discarded editor work")
	}
}

func TestPrimaryNavigationRetainsPressedTargetDuringUpdates(t *testing.T) {
	a := fileFollowDemo()
	a.fileExpanded = true
	tt := ui.NewTester(a.view, 560, 760)
	r, ok := tt.Find("授权中心")
	if !ok {
		t.Fatal("expanded files lost global navigation")
	}
	tt.Press(r.X+r.W/2, r.Y+r.H/2)
	a.status = "updated while pressed"
	tt.Frame()
	tt.Release(r.X+r.W/2, r.Y+r.H/2)
	if a.page != 3 {
		t.Fatal("background update lost pressed navigation")
	}
}

func TestPrimaryPagesKeepDetachedToolsIndependent(t *testing.T) {
	a := fileFollowDemo()
	windows := fakeToolsFactory(a)
	main := ui.NewTester(a.view, 1280, 900)
	detachFromMenu(t, main)
	if !a.toolsDetached || a.contentFocus || len(*windows) != 1 || !(*windows)[0].focused {
		t.Fatal("detach stole focus from the tool window")
	}
	if err := main.Click("设置"); err != nil {
		t.Fatal(err)
	}
	if a.page != 4 || !a.toolsDetached || a.contentFocus {
		t.Fatal("settings disturbed the detached workspace")
	}
	tools := ui.NewTester(a.detachedToolsView, 960, 720)
	if err := tools.Click("命令"); err != nil {
		t.Fatal(err)
	}
	if a.contentFocus || !a.toolsDetached || a.dock != toolCommands || a.page != 4 {
		t.Fatal("independent tool selection changed the primary page")
	}
}

func TestPrimaryPageChangePreservesGlobalLockFocus(t *testing.T) {
	a := fileFollowDemo()
	a.page = 4
	tt := ui.NewTester(a.workspaceHeader, 560, 60)
	if err := tt.Click("设置"); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 6 && !tt.Focused("锁定"); i++ {
		tt.Key(0, ui.KeyTab)
	}
	if !tt.Focused("锁定") {
		t.Fatal("keyboard navigation could not reach the direct lock action")
	}
	// A native-menu page change must not reuse this focus for a workbench action.
	a.page = 1
	tt.Frame()
	if !tt.Focused("锁定") || tt.Focused("收窄连接侧栏") || tt.Focused("隐藏工具面板") {
		t.Fatal("page-specific controls inherited the global lock identity")
	}
	a.page = 3
	tt.Frame()
	if !tt.Focused("锁定") {
		t.Fatal("the global lock action lost its stable identity on authorization")
	}
}

func TestPrimaryPagesShareSidebarStateAndKeepContentSelection(t *testing.T) {
	a := fileFollowDemo()
	_, first := reviewFixture("full")
	first.Status = "approved"
	second := first
	second.ID, second.Operation.Endpoint = "second-sidebar-request", "deploy@192.0.2.20:2222"
	a.requests = []approval.Request{first, second}
	a.approvalTab, a.selectedRequest = 1, second.ID
	a.settingsSection = settingsSecurity
	width, pane, terminal := a.sidebarWidth, a.primaryFilePane, a.sessions[0]
	tt := ui.NewTester(a.view, 1000, 760)
	pages := []struct {
		id                int
		tab, region, rail string
		width             float32
	}{
		{3, "授权中心", "授权分类", "授权分类窄栏", approvalCategoryWidth},
		{4, "设置", "设置分类", "设置分类窄栏", 152},
		{1, "工作台", "连接侧栏", "连接窄栏", width},
	}
	anchor, _ := tt.Find(a.pageSidebarLabel(1))
	for _, origin := range pages {
		for _, hidden := range []bool{true, false} {
			if err := tt.Click(origin.tab); err != nil {
				t.Fatal(err)
			}
			before, found := tt.Find(a.pageSidebarLabel(origin.id))
			if !found || before != anchor || before.X != 6 || before.Y < 0 || before.Y+before.H > 36 || before.W != 30 || before.H != 30 {
				t.Fatal("sidebar control moved from its shared top-left position", origin.tab, before, anchor)
			}
			var details ui.Rect
			if origin.id == 3 {
				details, _ = tt.Find("授权详情区域")
			}
			tt.ClickAt(anchor.X+anchor.W/2, anchor.Y+anchor.H/2)
			if a.sidebarHidden != hidden {
				t.Fatal("sidebar control did not update the shared state", origin.tab, hidden)
			}
			if origin.id == 3 && hidden {
				after, _ := tt.Find("授权详情区域")
				if after.W <= details.W || a.contentFocus {
					t.Fatal("collapsing authorization failed to return space or stole terminal focus", details, after)
				}
			}
			for _, page := range pages {
				if err := tt.Click(page.tab); err != nil {
					t.Fatal(err)
				}
				if a.sidebarHidden != hidden {
					t.Fatal("page switch changed the shared sidebar state", origin.tab, page.tab, hidden)
				}
				region, expectedWidth, absent := page.region, page.width, page.rail
				if hidden {
					region, expectedWidth, absent = page.rail, components.RailWidth, page.region
				}
				bounds, found := tt.Find(region)
				if !found || bounds.X != 0 || bounds.W != expectedWidth {
					t.Fatal("page did not reflect the shared sidebar state", origin.tab, page.tab, hidden, bounds)
				}
				if _, found := tt.Find(absent); found {
					t.Fatal("page mounted both sidebar widths", page.tab, absent)
				}
				if control, found := tt.Find(a.pageSidebarLabel(page.id)); !found || control != anchor {
					t.Fatal("page switch moved the shared toggle", page.tab, control, anchor)
				}
				header, _ := tt.Find("全局导航栏")
				if header.H != 36 || header.Y != 0 {
					t.Fatal("page switching added another navigation row", header)
				}
				if a.approvalTab != 1 || a.selectedRequest != second.ID || a.settingsSection != settingsSecurity || a.sidebarWidth != width || a.primaryFilePane != pane || a.sessions[0] != terminal {
					t.Fatal("shared sidebar state changed page content or layout preferences", page.tab)
				}
				if page.id == 3 && !tt.HasText(second.Operation.Endpoint) {
					t.Fatal("returning to authorization lost the selected request")
				}
			}
		}
	}
}

func TestApprovalCollapseKeepsNarrowSelectionAndClearsPasswordOnNewTarget(t *testing.T) {
	a, first := reviewFixture("full")
	a.ctx, a.online, a.page = context.Background(), true, 3
	second := first
	second.ID, second.Operation.Endpoint = "second-narrow-sidebar", "deploy@192.0.2.20:2222"
	a.requests = []approval.Request{first, second}
	a.selectedRequest = first.ID
	tt := ui.NewTester(a.view, 560, 760)
	if err := tt.Click("保险库主密码"); err != nil {
		t.Fatal(err)
	}
	tt.Type("synthetic-review-password")
	if err := tt.Click("收窄授权分类"); err != nil {
		t.Fatal(err)
	}
	if _, found := tt.Find("选择授权请求"); !found || a.ownerPass == "" || a.selectedRequest != first.ID {
		t.Fatal("collapsing the request navigation changed the active review")
	}
	if err := tt.Click("选择授权请求"); err != nil {
		t.Fatal(err)
	}
	if err := tt.Click("2. 我的 AI 助手 · 客户端会话授权"); err != nil {
		t.Fatal(err)
	}
	if a.selectedRequest != second.ID || a.ownerPass != "" || !tt.HasText(second.Operation.Endpoint) {
		t.Fatal("restored request selection reused authentication for another target")
	}
	for _, label := range []string{"保险库主密码", "不允许", "允许"} {
		r, found := tt.Find(label)
		if !found || r.X < 0 || r.X+r.W > 560 || r.Y+r.H > 760 {
			t.Fatal("narrow review clipped a required decision", label, r)
		}
	}
}

func TestLockReleasesConnectionRailMetadataAndListCallbacks(t *testing.T) {
	a := fileFollowDemo()
	_ = ui.NewTester(a.connectionRail, 48, 360)
	if len(a.connectionRailCache.rows) == 0 || a.connectionRailList.Key == nil {
		t.Fatal("fixture did not populate connection metadata and virtual-list callbacks")
	}
	a.lock()
	if a.connectionRailCache.source != nil || a.connectionRailCache.rows != nil || a.connectionRailList.Key != nil || a.connectionRailList.Label != nil {
		t.Fatal("lock retained connection rail metadata")
	}
}
