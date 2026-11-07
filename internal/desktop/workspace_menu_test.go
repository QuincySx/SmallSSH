package desktop

import (
	"context"
	"fmt"
	"testing"

	"github.com/egoist/mygo/ui"
	"github.com/egoist/mygo/ui/editor"
	"sshclient/internal/approval"
)

func TestPrimaryPagesUseWholeContentAndRetainWorkspace(t *testing.T) {
	a := fileFollowDemo()
	a.ensureFileLayout()
	a.chooseFileEndpoint(a.primaryFilePane, "c")
	a.fileExpanded = true
	pane, layout, first, second := a.primaryFilePane, a.fileLayout, a.sessions[0], a.sessions[1]
	width := a.sidebarWidth
	for _, next := range []struct {
		page    int
		section string
	}{{3, settingsApprovals}, {4, settingsWorkspace}, {3, settingsApprovals}} {
		a.navigatePrimaryPage(next.page)
		if !a.settingsOpen || a.settingsSection != next.section || a.page != 1 {
			t.Fatal("independent settings opened the wrong section or replaced the workspace", next, a.page, a.settingsSection)
		}
		if a.primaryFilePane != pane || a.fileLayout != layout || a.sessions[0] != first || a.sessions[1] != second || pane.follow || pane.endpoint != "c" || pane.browser().dir != "/srv/c" || a.sidebarWidth != width || !a.fileExpanded {
			t.Fatal("opening settings discarded workspace state", next)
		}
	}
}

func TestWorkspaceRecoveryLivesInSettingsAndLockIsDirect(t *testing.T) {
	a := fileFollowDemo()
	a.sidebarHidden, a.toolsHidden, a.toolsRight = true, true, true
	tt := ui.NewTester(a.workspaceHeader, 1000, 70)
	if err := tt.Click("设置"); err != nil || !a.settingsOpen || a.settingsSection != settingsWorkspace || a.page != 1 {
		t.Fatal("settings did not open independently", err, a.settingsSection, a.page)
	}
	if !a.sidebarHidden || !a.toolsHidden || !a.toolsRight {
		t.Fatal("opening settings changed workspace layout")
	}
	if _, found := tt.Find("锁定"); found {
		t.Fatal("lock remains duplicated in the title bar")
	}
}

func TestPrimaryNavigationSupportsKeyboardWithoutAnAppMenu(t *testing.T) {
	a := fileFollowDemo()
	tt := ui.NewTester(a.workspaceHeader, 1000, 70)
	if err := tt.Click("终端 A"); err != nil {
		t.Fatal(err)
	}
	tt.Key(0, ui.KeyRight)
	if a.currentTerminal() != a.sessions[1] {
		t.Fatal("keyboard did not select the next terminal")
	}
	tt.Key(0, ui.KeyLeft)
	if a.currentTerminal() != a.sessions[0] {
		t.Fatal("keyboard did not select the previous terminal")
	}
}

func TestWorkspaceHeaderStaysSingleAndControlsFollowTheirPage(t *testing.T) {
	for _, width := range []int{560, 1280} {
		for _, mode := range []string{"terminal", "expanded", "sidebar-hidden", "terminal-split"} {
			t.Run(fmt.Sprintf("%d/%s", width, mode), func(t *testing.T) {
				a := fileFollowDemo()
				switch mode {
				case "expanded":
					a.fileExpanded = true
				case "sidebar-hidden":
					a.sidebarHidden = true
				case "terminal-split":
					a.ensureTerminalGroups()
					a.splitExistingTerminal(a.sessions[1])
				}
				tt := ui.NewTester(a.workspaceHeader, width, 70)
				for _, dark := range []bool{false, true} {
					tt.SetDark(dark)
					header, ok := tt.Find("全局导航栏")
					if !ok || header.Y != 0 || header.H != 48 || header.W != float32(width) {
						t.Fatal("global header moved", header)
					}
					for _, label := range []string{"设置", "传输任务", "隐藏工具面板", a.pageSidebarLabel(1)} {
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
	a.connection.returnPage = 1
	a.form.Name, a.form.Password = "未保存连接", "synthetic secret"
	tt := ui.NewTester(func(c *ui.Context) { ui.Column(c).Fill().Children(func() { a.workspaceHeader(c); a.promptView(c) }) }, 1000, 760)
	if err := tt.Click("设置"); err != nil {
		t.Fatal(err)
	}
	if a.page != 0 || !a.prompt.open || a.form.Password == "" {
		t.Fatal("settings bypassed unsaved connection protection")
	}
	if err := tt.Click("取消"); err != nil {
		t.Fatal(err)
	}
	if a.page != 0 || a.form.Name != "未保存连接" || a.form.Password == "" {
		t.Fatal("cancel lost connection draft")
	}
	if err := tt.Click("设置"); err != nil {
		t.Fatal(err)
	}
	if err := tt.Click("放弃并继续"); err != nil {
		t.Fatal(err)
	}
	if !a.settingsOpen || a.page != 1 || a.form.Password != "" {
		t.Fatal("confirmed navigation did not clean draft and open settings")
	}

	a = fileFollowDemo()
	a.edit = &editor.State{Buffer: editor.NewBuffer("saved")}
	a.editPath = "/synthetic/example.txt"
	a.edit.Insert("changed")
	a.confirmLock()
	if !a.online || !a.prompt.open || a.prompt.title != "锁定前处理未保存修改" {
		t.Fatal("lock bypassed modified-editor confirmation")
	}
	tt = ui.NewTester(a.promptView, 1280, 900)
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
	tt := ui.NewTester(a.workspaceHeader, 560, 70)
	r, ok := tt.Find("设置")
	if !ok {
		t.Fatal("expanded files lost settings")
	}
	tt.Press(r.X+r.W/2, r.Y+r.H/2)
	a.status = "updated while pressed"
	tt.Frame()
	tt.Release(r.X+r.W/2, r.Y+r.H/2)
	if !a.settingsOpen || a.settingsSection != settingsWorkspace || a.page != 1 {
		t.Fatal("background update lost pressed settings action")
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
	if a.page != 1 || !a.settingsOpen || !a.toolsDetached {
		t.Fatal("settings disturbed the detached workspace")
	}
	tools := ui.NewTester(a.detachedToolsView, 960, 720)
	if err := tools.Click("命令"); err != nil {
		t.Fatal(err)
	}
	if !a.toolsDetached || a.dock != toolCommands || a.page != 1 {
		t.Fatal("independent tool selection changed the workspace")
	}
}

func TestPrimaryPageChangePreservesGlobalLockFocus(t *testing.T) {
	for _, bar := range []ui.TitleBar{{}, {Height: 32, Left: 78}, {Height: 54, Left: 78}} {
		a := fileFollowDemo()
		tt := ui.NewTester(a.workspaceHeader, 560, 70)
		tt.SetTitleBar(bar)
		if err := tt.Click("设置"); err != nil {
			t.Fatal(err)
		}
		if !tt.Focused("设置") {
			t.Fatal("settings action did not take focus")
		}
		a.filesTabOpen = true
		tt.Frame()
		if !tt.Focused("设置") || tt.Focused("文件") {
			t.Fatal("file tab inherited settings focus")
		}
		a.filesTabOpen = false
		tt.Frame()
		if !tt.Focused("设置") {
			t.Fatal("settings lost focus when file tab closed")
		}
	}
}

func TestPrimaryPagesShareSidebarStateAndKeepContentSelection(t *testing.T) {
	a := fileFollowDemo()
	width, pane, terminal := a.sidebarWidth, a.primaryFilePane, a.sessions[0]
	tt := ui.NewTester(a.workspaceHeader, 1000, 70)
	if err := tt.Click("收窄连接侧栏"); err != nil || !a.sidebarHidden {
		t.Fatal("sidebar did not collapse", err)
	}
	a.navigatePrimaryPage(3)
	if !a.settingsOpen || a.settingsSection != settingsApprovals || !a.sidebarHidden || a.sidebarWidth != width || a.primaryFilePane != pane || a.sessions[0] != terminal {
		t.Fatal("approval window disturbed shared workspace state")
	}
}

func TestApprovalCollapseKeepsNarrowSelectionAndClearsPasswordOnNewTarget(t *testing.T) {
	a, first := reviewFixture("full")
	a.ctx, a.online, a.page = context.Background(), true, 1
	a.settingsOpen, a.settingsSection = true, settingsApprovals
	second := first
	second.ID, second.Operation.Endpoint = "second-narrow-sidebar", "deploy@192.0.2.20:2222"
	a.requests = []approval.Request{first, second}
	a.selectedRequest = first.ID
	tt := ui.NewTester(a.settingsWindowView, 720, 760)
	if err := tt.Click("允许"); err != nil {
		t.Fatal(err)
	}
	if err := tt.Click("保险库主密码"); err != nil {
		t.Fatal(err)
	}
	tt.Type("synthetic-review-password")
	a.sidebarHidden = true
	tt.Frame()
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
	for _, label := range []string{"拒绝", "允许"} {
		r, found := tt.Find(label)
		if !found || r.X < 0 || r.X+r.W > 720 || r.Y+r.H > 760 {
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
