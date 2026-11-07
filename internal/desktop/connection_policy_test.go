package desktop

import (
	"context"
	"reflect"
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
	tt := ui.NewTester(a.connectionPolicyForm, 430, 120)
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
	a.editProtectedPaths()
	if a.prompt.value == "" || !a.prompt.multiline || a.prompt.validate == nil {
		t.Fatal("default paths were not accessible")
	}
	if a.prompt.validate("relative/path") == nil {
		t.Fatal("invalid protected path was accepted")
	}
	a.prompt.confirm("/srv/private\n\n/etc")
	if !reflect.DeepEqual(a.form.Policy.ProtectedPaths, []string{"/srv/private", "/etc"}) {
		t.Fatal("valid editor paths were not applied")
	}
	policy, err := protectedPathsInput("")
	if err != nil || len(policy.ProtectedPaths) != 0 || len(policy.Paths()) == 0 {
		t.Fatal("empty editor did not restore defaults")
	}
}

// The path editor has to be tall and wide enough to read several paths at
// once, and a rejected path must stay editable instead of closing the dialog.
func TestProtectedPathsEditorStaysReadableAndEditableAtSmallWindow(t *testing.T) {
	a := &app{online: true}
	a.newConnection()
	a.editProtectedPaths()
	tt := ui.NewTester(a.promptView, 560, 760)
	label, ok := tt.Find("保护路径输入")
	apply, applyOK := tt.Find("应用")
	if !ok || label.W < 400 || label.Y < 0 {
		t.Fatal("protected paths field did not use the available dialog width", label)
	}
	if !applyOK || apply.Y < 0 || apply.Y+apply.H > 760 || apply.Y-(label.Y+label.H) < 160 {
		t.Fatal("multiline path area is not readable above the visible dialog actions", label, apply)
	}
	a.prompt.value = "/srv/private\nrelative/path"
	tt.Frame()
	if err := tt.Click("应用"); err != nil {
		t.Fatal(err)
	}
	if !a.prompt.open || a.prompt.value != "/srv/private\nrelative/path" || a.prompt.error == "" {
		t.Fatal("an invalid protected path closed the editor or lost the draft", a.prompt.open, a.prompt.value, a.prompt.error)
	}
	if !tt.HasText("规范绝对路径") {
		t.Fatal("the invalid protected path was not explained next to the editable area", tt.Texts())
	}
	if len(a.form.Policy.ProtectedPaths) != 0 {
		t.Fatal("an invalid protected path was stored", a.form.Policy.ProtectedPaths)
	}
	a.prompt.value = "/srv/private\n/etc/ssh"
	tt.Frame()
	if err := tt.Click("应用"); err != nil {
		t.Fatal(err)
	}
	if a.prompt.open || !reflect.DeepEqual(a.form.Policy.ProtectedPaths, []string{"/srv/private", "/etc/ssh"}) {
		t.Fatal("corrected protected paths were not applied", a.prompt.open, a.form.Policy.ProtectedPaths)
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

func TestConnectionPolicyKeepsSaveAfterConnectOptionInVisibleFooter(t *testing.T) {
	for _, size := range [][2]int{{560, 760}, {820, 1600}} {
		a := &app{online: true}
		a.newConnection()
		tt := ui.NewTester(a.connectionView, size[0], size[1])
		dialog, dialogOK := tt.Find("新建连接对话框")
		option, optionOK := tt.Find("保存后连接")
		save, saveOK := tt.Find("保存并连接")
		if !dialogOK || !optionOK || !saveOK || option.X < dialog.X || option.X+option.W > dialog.X+dialog.W || option.Y < dialog.Y || option.Y+option.H > dialog.Y+dialog.H || option.Y > save.Y+save.H || option.Y+option.H < save.Y {
			t.Fatal("save behavior option escaped the visible footer", size, dialog, option, save)
		}
		if err := tt.Click("保存后连接"); err != nil {
			t.Fatal(err)
		}
		if a.connection.connect || !tt.HasText("保存连接") {
			t.Fatal("visible save-only option did not change the submit action")
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
