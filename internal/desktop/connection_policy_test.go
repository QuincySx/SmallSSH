package desktop

import (
	"context"
	"reflect"
	"runtime"
	"strings"
	"testing"

	"github.com/egoist/mygo/ui"
	"sshclient/internal/accesspolicy"
	"sshclient/internal/vault"
)

func TestConnectionPolicyDefaultsAndExplicitRiskConfirmation(t *testing.T) {
	a := &app{ctx: context.Background(), online: true}
	a.newConnection()
	if a.form.Policy.Disabled || a.form.Policy.Unrestricted || a.connectionHasChanges() {
		t.Fatal("new connection did not default to protected AI access")
	}
	tt := ui.NewTester(a.connectionPolicyForm, 430, 500)
	if tt.HasText("保护开启时，AI 不开放任意命令和终端") || tt.HasText("受保护路径禁止读取、下载和修改") || !tt.HasText("保护关键操作") {
		t.Fatal("AI access did not stay a plain allow/deny choice", tt.Texts())
	}
	if err := tt.Click("保护关键操作"); err != nil {
		t.Fatal(err)
	}
	if a.form.Policy.Unrestricted || !a.prompt.open || !a.prompt.danger || a.prompt.accept != "关闭保护" {
		t.Fatal("unrestricted access was enabled without reviewing the risk")
	}
	confirm := a.prompt.confirm
	a.prompt = promptState{}
	confirm("")
	if !a.form.Policy.Unrestricted || !a.connectionHasChanges() {
		t.Fatal("explicit risk confirmation was not applied")
	}
	a.confirmUnrestrictedPolicy()
	stale := a.prompt.confirm
	a.newConnection()
	stale("")
	if a.form.Policy.Unrestricted {
		t.Fatal("stale confirmation changed another editor")
	}
	tt.Frame()
	if err := tt.Click("允许 AI 访问此服务器"); err != nil {
		t.Fatal(err)
	}
	if !a.form.Policy.Disabled || !a.connectionHasChanges() {
		t.Fatal("disabled access was not represented")
	}
	if a.startup.activations != 0 || a.authSequence != 0 {
		t.Fatal("policy editing activated a window or system authentication")
	}
}

func TestConnectionPolicyPreservedAcrossCredentialFormsAndMetadataEdits(t *testing.T) {
	for _, auth := range []string{"password", "key", "agent"} {
		t.Run(auth, func(t *testing.T) {
			policy := accesspolicy.Policy{Disabled: true, Unrestricted: true, ProtectedPaths: []string{"/srv/private"}}
			d := vault.Summary{ID: "example", Name: "Example", Host: "192.0.2.1", Port: 22, User: "deploy", Auth: auth, Fingerprint: "SHA256:FIXTURE", Policy: policy}
			a := &app{online: true, selected: d.ID, connections: []vault.Summary{d}}
			a.editConnection()
			if a.connectionHasChanges() || !reflect.DeepEqual(a.form.Policy, policy) {
				t.Fatal("loading a saved connection changed its policy")
			}
			a.form.Name = "Renamed"
			change, ok := a.connectionDraft()
			if !ok || !change.KeepCredential || !reflect.DeepEqual(change.Profile.Policy, policy) {
				t.Fatal("metadata save discarded policy", change, a.connection.errors)
			}
			a.form.Policy.ProtectedPaths[0] = "/srv/changed"
			if a.details.Policy.ProtectedPaths[0] != "/srv/private" || a.connections[0].Policy.ProtectedPaths[0] != "/srv/private" {
				t.Fatal("editing policy mutated the saved baseline")
			}
			a.form.Name = d.Name
			if !a.connectionHasChanges() {
				t.Fatal("policy-only edits bypassed unsaved-change handling")
			}
		})
	}
}

func TestProtectedPathsEditorUsesValidatedPolicyDefaults(t *testing.T) {
	a := &app{online: true}
	a.newConnection()
	tt := ui.NewTester(a.connectionPolicyForm, 560, 520)
	if _, ok := tt.Find("保护路径输入"); !ok || a.prompt.open || !a.connection.pathsReady || a.connection.protectedPaths != strings.Join(a.form.Policy.Paths(), "\n") || a.connectionHasChanges() {
		t.Fatal("inline defaults missing, opened a dialog or changed the clean draft")
	}
	a.updateProtectedPaths("/srv/private\n\n/etc")
	if !reflect.DeepEqual(a.form.Policy.ProtectedPaths, []string{"/srv/private", "/etc"}) {
		t.Fatal("valid inline paths were not applied")
	}
	a.updateProtectedPaths("")
	if len(a.form.Policy.ProtectedPaths) != 0 || len(a.form.Policy.Paths()) == 0 || a.connectionHasChanges() {
		t.Fatal("clearing the inline field did not restore unchanged defaults")
	}
}

func replaceProtectedPaths(t *testing.T, tt *ui.Tester, value string) {
	t.Helper()
	if err := tt.Click("保护路径输入"); err != nil {
		t.Fatal(err)
	}
	mod := ui.Ctrl
	if runtime.GOOS == "darwin" {
		mod = ui.Super
	}
	tt.Key(mod, ui.KeyA)
	tt.Key(0, ui.KeyBackspace)
	tt.Type(value)
	tt.Frame()
}

func TestProtectedPathsEditorStaysReadableAndEditableAtSmallWindow(t *testing.T) {
	for _, width := range []int{560, 1000} {
		a := &app{online: true}
		a.newConnection()
		a.form.Name, a.form.Host, a.form.User = "Example", "192.0.2.10", "demo"
		a.connection.section = connectionAccess
		tt := ui.NewTester(a.connectionView, width, 760)
		area, ok := tt.Find("保护路径输入")
		content, contentOK := tt.Find("连接分类内容")
		if !ok || !contentOK || area.W < content.W-24 || area.H != 210 || area.Y < content.Y || area.Y+area.H > content.Y+content.H || a.prompt.open || tt.HasText("保护路径…") {
			t.Fatal("paths did not use the current form's available area", width, area, content)
		}
		replaceProtectedPaths(t, tt, "/srv/private\nrelative/path")
		if a.connection.protectedPaths != "/srv/private\nrelative/path" || !a.connectionHasChanges() || a.prompt.open || !tt.HasText("规范绝对路径") {
			t.Fatal("invalid inline paths lost the draft, error or unsaved state", a.connection.protectedPaths, tt.Texts())
		}
		if err := tt.Click("保存连接"); err != nil {
			t.Fatal(err)
		}
		if a.connection.errors["policy"] == "" || a.connection.phase != "" || a.prompt.open {
			t.Fatal("invalid protected paths passed save or started a connection")
		}
		tt.Frame()
		if a.connection.section != connectionAccess || !tt.Focused("保护路径输入") {
			t.Fatal("save validation did not return focus to the inline error")
		}
		if err := tt.Click("连接分类：常规"); err != nil {
			t.Fatal(err)
		}
		if err := tt.Click("连接分类：访问策略"); err != nil {
			t.Fatal(err)
		}
		if a.connection.protectedPaths != "/srv/private\nrelative/path" {
			t.Fatal("category navigation lost the inline draft")
		}
		replaceProtectedPaths(t, tt, "/srv/private\n/etc/ssh")
		change, ok := a.connectionDraft()
		if !ok || a.connection.errors["policy"] != "" || !reflect.DeepEqual(change.Profile.Policy.ProtectedPaths, []string{"/srv/private", "/etc/ssh"}) || a.prompt.open {
			t.Fatal("corrected inline policy was not included in the connection draft", change.Profile.Policy, a.connection.errors)
		}
	}
}

func TestInlineProtectedPathErrorsCannotHideBehindDisabledAccess(t *testing.T) {
	a := &app{online: true}
	a.newConnection()
	a.form.Name, a.form.Host, a.form.User = "Example", "192.0.2.10", "demo"
	a.updateProtectedPaths("relative/path")
	a.form.Policy.Disabled, a.form.Policy.Unrestricted = true, true
	tt := ui.NewTester(a.connectionPolicyForm, 560, 520)
	if _, ok := tt.Find("保护路径输入"); !ok {
		t.Fatal("disabled policy hid an invalid editable draft")
	}
	if _, ok := a.connectionDraft(); ok {
		t.Fatal("disabling access or protection bypassed path validation")
	}
	replaceProtectedPaths(t, tt, "/srv/private")
	if _, ok := a.connectionDraft(); !ok {
		t.Fatal("disabled policy left its invalid path impossible to correct", a.connection.errors)
	}
	a.newConnection()
	if a.connection.pathsReady || a.connection.protectedPaths != "" || len(a.form.Policy.ProtectedPaths) != 0 {
		t.Fatal("inline draft leaked into another connection")
	}
	tt.Frame()
	if a.connection.protectedPaths != strings.Join(a.form.Policy.Paths(), "\n") || !tt.HasText("保护路径输入") {
		t.Fatal("existing inline control did not rebind to the new connection defaults")
	}
}

func TestConnectionPolicyShortcutScrollsToServerAccessSettings(t *testing.T) {
	a := &app{online: true, page: 1, selected: "example", sidebarWidth: 190, toolsHidden: true, groups: map[string]*bool{},
		connections: []vault.Summary{{ID: "example", Name: "Example", Host: "192.0.2.1", Port: 22, User: "deploy", Auth: "password"}}}
	tt := ui.NewTester(a.view, 1000, 760)
	if err := tt.RightClick("Example"); err != nil {
		t.Fatal(err)
	}
	if err := tt.ChooseMenuItem("AI 访问…"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	bounds, ok := tt.Find("允许 AI 访问此服务器")
	if !ok || a.page != 0 || a.form.ID != "example" || bounds.Y < 0 || bounds.Y+bounds.H > 760 {
		t.Fatal("server policy shortcut did not reveal the editable setting", bounds, tt.Texts())
	}
}

func TestConnectionFooterOnlySaves(t *testing.T) {
	for _, size := range [][2]int{{560, 760}, {820, 1600}} {
		a := &app{online: true}
		a.newConnection()
		tt := ui.NewTester(a.connectionView, size[0], size[1])
		dialog, ok := tt.Find("新建连接对话框")
		save, found := tt.Find("保存连接")
		if !ok || !found || save.X < dialog.X || save.X+save.W > dialog.X+dialog.W || save.Y+save.H > dialog.Y+dialog.H || tt.HasText("保存后连接") {
			t.Fatal("save-only footer violated", dialog, save)
		}
	}
}

func TestProtectedFullGrantNeverClaimsUnlimitedAccountAccess(t *testing.T) {
	a, r := reviewFixture("full")
	r.Operation.PathProtection = true
	label, _, caution := approvalPermission(r)
	if label != "读写（受服务器策略限制）" || caution != "护栏只拦常见破坏性命令，不能替代核对；交互终端不开放" || approvalActionLabel(r) != "允许读写" {
		t.Fatal("protected grant claimed complete control", label, caution)
	}
	r.Renewal = true
	if approvalActionLabel(r) != "续期读写" {
		t.Fatal("protected renewal claimed complete control")
	}
	state := &approvalPromptState{}
	tt := ui.NewTester(func(c *ui.Context) { a.approvalPrompt(c, r, state, func(bool, bool) {}, func() {}) }, 380, 560)
	if tt.HasText("完整控制") || tt.HasText("可使用此 SSH 账户的全部权限") || !tt.HasText(label) {
		t.Fatal("compact prompt hid server policy", tt.Texts())
	}
}
