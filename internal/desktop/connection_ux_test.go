package desktop

import (
	"context"
	"math"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"
	"github.com/egoist/mygo/ui/editor"
	"sshclient/internal/approval"
	"sshclient/internal/broker"
	"sshclient/internal/vault"
)

func TestConnectionModalKeepsWorkspaceAndProtectsDraft(t *testing.T) {
	for _, dark := range []bool{false, true} {
		a := &app{ctx: context.Background(), online: true, page: 1, toolsHidden: true, sidebarWidth: 190,
			groups: map[string]*bool{}, sessions: []*session{{name: "当前终端", host: "current"}}}
		a.newConnection()
		tt := ui.NewTester(a.view, 1000, 760)
		tt.SetDark(dark)
		if !tt.HasText("当前终端") || !tt.Focused("名称输入") {
			t.Fatal("modal replaced the workspace or did not focus its first input", tt.Texts())
		}
		dialog, ok := tt.Find("新建连接对话框")
		save, found := tt.Find("保存并连接")
		name, named := tt.Find("名称输入")
		if !ok || !found || !named || math.Abs(float64(dialog.X+dialog.W/2-500)) > 1 || math.Abs(float64(dialog.Y+dialog.H/2-380)) > 1 || name.Y < dialog.Y || save.Y+save.H > dialog.Y+dialog.H {
			t.Fatal("dialog is not centered or its footer escaped the window", dialog, save, name)
		}
		tt.Type("待保存")
		a.form.Password = "synthetic draft secret"
		tt.Frame()
		for range 14 {
			tt.Key(0, ui.KeyTab)
			if tt.Focused("工作台") || tt.Focused("授权中心") || tt.Focused("设置") || tt.Focused("锁定") || tt.Focused("关闭标签：当前终端") || tt.Focused("搜索连接") {
				t.Fatal("focus escaped to the terminal workspace")
			}
		}
		tt.Key(0, ui.KeyEscape)
		if !a.prompt.open || a.page != 0 || a.form.Password == "" {
			t.Fatal("Escape silently discarded the draft")
		}
		tt.Key(0, ui.KeyEscape)
		if a.prompt.open || a.form.Name != "待保存" || a.form.Password == "" {
			t.Fatal("cancelling discard did not keep the draft")
		}
		if err := tt.Click("关闭新建连接"); err != nil {
			t.Fatal(err)
		}
		if err := tt.Click("放弃并继续"); err != nil {
			t.Fatal(err)
		}
		if a.page != 1 || a.form.Password != "" || len(a.sessions) != 1 || !tt.HasText("当前终端") || tt.HasText("服务器地址输入") {
			t.Fatal("closing did not clear secrets and return to the same terminal")
		}
	}
}

func TestConnectionCloseFocusDoesNotConsumeEscapeBeforeDraftGuard(t *testing.T) {
	a := &app{ctx: context.Background(), online: true, page: 1, toolsHidden: true, groups: map[string]*bool{}}
	a.newConnection()
	tt := ui.NewTester(a.view, 1000, 760)
	tt.Type("未保存的服务器")
	a.form.Password = "synthetic draft secret"
	tt.Frame()
	for attempts := 0; attempts < 32 && !tt.Focused("关闭新建连接"); attempts++ {
		tt.Key(0, ui.KeyTab)
	}
	if !tt.Focused("关闭新建连接") {
		t.Fatal("keyboard navigation could not reach the modal close control")
	}
	tt.Key(0, ui.KeyEscape)
	if !a.prompt.open || a.prompt.title != "放弃未保存的连接？" || a.page != 0 || a.form.Password == "" {
		t.Fatal("focused close control swallowed Escape before protecting the draft")
	}
}

func TestConnectionModalAtomicSaveCannotBeDismissed(t *testing.T) {
	a := &app{ctx: context.Background(), online: true, page: 4, toolsHidden: true, groups: map[string]*bool{}}
	a.newConnection()
	a.form.Name, a.form.Password = "保存中", "synthetic draft secret"
	a.connection.phase = "save"
	tt := ui.NewTester(a.view, 1000, 760)
	for _, dismiss := range []func(){
		func() { tt.Key(0, ui.KeyEscape) },
		func() { tt.Click("关闭新建连接") },
		func() { tt.Press(10, 700); tt.Release(10, 700) },
	} {
		dismiss()
		if a.page != 0 || a.form.Password == "" || a.prompt.open || !tt.HasText("正在保存…") {
			t.Fatal("dismiss interrupted atomic encrypted save")
		}
	}
	a.connection.phase = ""
	a.cancelConnectionEditor()
	tt.Frame()
	if a.page != 4 || a.form.Password != "" || tt.HasText("服务器地址输入") {
		t.Fatal("close did not restore the underlying settings page")
	}
}

func TestConnectionPortAndReusableGroupsAreVisible(t *testing.T) {
	h := newConnectionFlowHarness(t)
	a := h.a
	a.connections = []vault.Summary{{ID: "a", Group: "生产"}, {ID: "b", Group: "测试"}, {ID: "c", Group: "生产"}}
	h.ui.Frame()
	host, ok := h.ui.Find("服务器地址输入")
	if !ok {
		t.Fatal("missing address")
	}
	port, ok := h.ui.Find("端口输入")
	if !ok || port.Y != host.Y || port.X <= host.X || a.port != "22" {
		t.Fatal("port must be visible beside host with default 22", host, port)
	}
	if h.ui.HasText("保险库主密码输入") || h.ui.HasText("端口与分组") {
		t.Fatal("redundant unlock or collapsed essentials")
	}
	h.click("分组输入")
	h.click("分组：生产")
	if a.form.Group != "生产" {
		t.Fatal("existing group was not selected", a.form.Group)
	}
	h.input("分组", "新的分组")
	h.click("分组：新建分组「新的分组」")
	if a.form.Group != "新的分组" {
		t.Fatal("new group not retained")
	}
	h.click("分组输入")
	h.click("分组：未分组")
	if a.form.Group != "" || len(a.connectionGroups()) != 2 {
		t.Fatal("group clear or dedup failed")
	}
}

func TestUnlockedSessionSavesWithoutPasswordAndCannotApproveOtherRequests(t *testing.T) {
	h := newConnectionFlowHarness(t)
	h.fillTarget()
	h.input("服务器密码", h.fixture.Profile().Password)
	h.a.connection.connect = false
	h.ui.Frame()
	h.confirmIdentity()
	if h.a.form.Fingerprint != "" || len(h.server.Vault.Summaries()) != 0 {
		t.Fatal("probe silently trusted or saved")
	}
	h.click("返回修改")
	if h.a.form.Name == "" || h.a.form.Password == "" || h.a.connection.review {
		t.Fatal("back lost form")
	}
	h.click("保存连接")
	if !h.a.connection.review {
		t.Fatal("known candidate must still require explicit trust")
	}
	h.click("信任并保存")
	h.wait(func() bool { return h.a.page == 1 || h.a.connection.phase == "" })
	if h.a.page != 1 || len(h.a.connections) != 1 {
		t.Fatal("first save did not close form", h.a.connection.errors)
	}
	id := h.a.connections[0].ID
	// An unrelated external request must not inherit the desktop session.
	other := h.fixture.Profile()
	other.ID = "unrelated"
	if err := h.a.client.SaveConnection(h.ctx, vault.ProfileChange{Profile: other}, h.server.Vault); err != nil {
		t.Fatal(err)
	}
	pending, _, err := h.a.client.Request(h.ctx, approval.Operation{Host: other.ID, Kind: "exec", Command: "printf external", Client: "desktop spoof"}, time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	h.a.selected = id
	h.a.editConnection()
	h.ui.Frame()
	h.input("名称", "修改后")
	h.click("保存连接")
	h.wait(func() bool { return h.a.page == 1 || h.a.connection.phase == "" })
	if h.a.page != 1 || h.stored(id).Name != "修改后" || h.a.ownerPass != "" || h.a.pass != "" {
		t.Fatal("session reuse failed or retained master input")
	}
	state, err := h.a.client.Call(h.ctx, broker.Message{Action: "status", ID: pending.ID})
	if err != nil || state.Request == nil || state.Request.Status != "pending" {
		t.Fatal("external request was approved", err, state.Request)
	}
	owner, err := h.a.unlockedOwner()
	if err != nil {
		t.Fatal(err)
	}
	h.a.lock()
	if _, err = h.a.unlockedOwner(); err == nil {
		t.Fatal("locked UI returned signing session")
	}
	if _, err = owner.Proof([]byte("after-lock")); err == nil {
		t.Fatal("old signing key survived lock")
	}
	h.server.Close()
}

func TestEveryTabHasAdjacentCloseAndClosingInactivePreservesSelection(t *testing.T) {
	a := &app{ctx: context.Background(), online: true, edit: &editor.State{Buffer: editor.NewBuffer("")}, sessions: []*session{{name: "生产", host: "prod"}, {name: "测试", host: "test"}, {name: "开发", host: "dev"}}, active: 1, selected: "sidebar-only"}
	tt := ui.NewTester(a.sessionStage, 900, 450)
	for _, name := range []string{"生产", "测试", "开发"} {
		label, ok := tt.Find(name)
		if !ok {
			t.Fatal("missing tab", name)
		}
		close, ok := tt.Find("关闭标签：" + name)
		if !ok || close.X < label.X+label.W || close.X-(label.X+label.W) > 6 || close.W < 24 || close.H < 24 {
			t.Fatal("close is not adjacent", name, label, close)
		}
	}
	if err := tt.Click("测试"); err != nil {
		t.Fatal(err)
	}
	tt.Key(0, ui.KeyRight)
	if a.sessions[a.active].host != "dev" {
		t.Fatal("keyboard tab navigation failed")
	}
	tt.Key(0, ui.KeyLeft)
	// Updating live server data while a button is pressed must not recreate
	// the tab and lose the release event.
	r, _ := tt.Find("关闭标签：生产")
	tt.Press(r.X+r.W/2, r.Y+r.H/2)
	a.sessions[0].monitorStatus = "刚刚更新"
	a.sessions[0].draft = "changed command draft"
	tt.Frame()
	tt.Release(r.X+r.W/2, r.Y+r.H/2)
	if len(a.sessions) != 2 || a.sessions[a.active].host != "test" || a.selected != "sidebar-only" {
		t.Fatal("closing inactive tab changed active session")
	}
	if err := tt.Click("开发"); err != nil {
		t.Fatal(err)
	}
	if a.sessions[a.active].host != "dev" || a.selected != "sidebar-only" {
		t.Fatal("tab selection failed")
	}
	if err := tt.Click("关闭标签：开发"); err != nil {
		t.Fatal(err)
	}
	if len(a.sessions) != 1 || a.selected != "sidebar-only" {
		t.Fatal("active close did not choose remaining tab")
	}
	a.editPath = "/tmp/example.txt"
	a.editShown = true
	a.edit.Insert("unsaved")
	tt.Frame()
	if err := tt.Click("关闭标签：测试"); err != nil {
		t.Fatal(err)
	}
	if !a.editShown || len(a.sessions) != 0 {
		t.Fatal("terminal close switched away from editor")
	}
	if err := tt.Click("关闭标签：编辑: example.txt ·"); err != nil {
		t.Fatal(err)
	}
	if !a.prompt.open || a.editPath == "" {
		t.Fatal("modified editor closed without confirmation")
	}
}
