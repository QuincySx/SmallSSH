package desktop

import (
	"encoding/json"
	"runtime"
	"strings"
	"testing"

	"github.com/egoist/mygo/ui"
	"sshclient/internal/monitor"
	"sshclient/internal/snippet"
)

func TestDeletingSystemVariantsKeepsRemainingBodiesAndParameters(t *testing.T) {
	variants := []snippet.Variant{
		{OS: snippet.OSDebian, Body: "echo debian $3"},
		{OS: snippet.OSRedHat, Body: "echo redhat $2"},
		{OS: snippet.OSAlpine, Body: "echo alpine $1"},
	}
	a := &app{online: true, snippetOpen: true, snippetForm: snippet.Command{
		ID: "variants", Name: "variants", Body: "echo $1",
		Variants: append([]snippet.Variant(nil), variants...),
		Args:     []snippet.Arg{{Preset: "keep"}, {}, {}},
	}}
	tt := ui.NewTester(a.snippetDialog, 760, 900)
	for i := range variants {
		if err := tt.Click("删除系统命令"); err != nil {
			t.Fatal(err)
		}
		tt.Frame()
		remaining := variants[i+1:]
		if len(a.snippetForm.Variants) != len(remaining) {
			t.Fatal("deleted the wrong number of variants", a.snippetForm.Variants)
		}
		for j, want := range remaining {
			if got := a.snippetForm.Variants[j]; got != want {
				t.Fatal("deletion changed another variant", got, want)
			}
		}
		if len(a.snippetForm.Args) != max(1, len(remaining)) || a.snippetForm.Args[0].Preset != "keep" {
			t.Fatal("parameters did not follow the remaining bodies", a.snippetForm.Args)
		}
	}
}

func commandFixture() []snippet.Command {
	return []snippet.Command{
		{ID: "deploy", Name: "部署前端", Group: "发布", Body: "cd /srv/app && ./deploy.sh"},
		{ID: "update", Name: "系统更新", Group: "运维", Body: "sudo apt update && sudo apt upgrade -y $1",
			Args: []snippet.Arg{{Type: snippet.TypeText, Preset: "nginx"}}},
		{ID: "misc", Name: "查看磁盘", Body: "df -h"},
	}
}

func TestSavedCommandsFilterByCategoryWithoutTheDraftFooter(t *testing.T) {
	a := &app{snippets: commandFixture(), snippetSelected: -1}
	tt := ui.NewTester(a.commandsView, 760, 360)
	for _, name := range []string{"部署前端", "系统更新", "查看磁盘"} {
		if !tt.HasText(name) {
			t.Fatal("missing command", name, tt.Texts())
		}
	}
	if tt.HasText("待执行命令") || tt.HasText("目标: ") {
		t.Fatal("the full-width draft row is still present")
	}
	if err := tt.Click("分组：运维"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if a.snippetGroup != "运维" {
		t.Fatal("category did not change", a.snippetGroup)
	}
	if !tt.HasText("系统更新") || tt.HasText("部署前端") || tt.HasText("查看磁盘") {
		t.Fatal("category did not filter the list", tt.Texts())
	}
	if err := tt.Click("系统更新"); err != nil {
		t.Fatal(err)
	}
	if err := tt.Click("系统更新"); err != nil {
		t.Fatal(err)
	}
	if a.status != "请先打开终端" || a.prompt.snippet != nil {
		t.Fatal("double click did not attempt to run in the active terminal", a.status)
	}
}

func TestCommandRunNeedsAValueOrASystemChoiceUnlessConfirmationWasDropped(t *testing.T) {
	plain := snippet.Command{ID: "p", Name: "p", Body: "uptime"}
	parameterized := snippet.Command{ID: "q", Name: "q", Body: "systemctl restart $1", Args: []snippet.Arg{{Preset: "nginx"}}}
	fixed := snippet.Command{ID: "f", Name: "f", Body: "systemctl restart $1", Args: []snippet.Arg{{Preset: "nginx", SkipPrompt: true}}}
	withVariants := snippet.Command{ID: "r", Name: "r", Body: "uname -a", Variants: []snippet.Variant{{OS: snippet.OSDebian, Body: "uname -a"}}}
	legacy := snippet.Command{ID: "s", Name: "s", Body: "awk '{print $1}'"}
	for _, test := range []struct {
		name     string
		command  snippet.Command
		detected string
		skip     bool
		want     bool
	}{
		{"confirm plain", plain, "", false, true},
		{"skip plain", plain, "", true, false},
		{"skip with a missing value", parameterized, "", true, true},
		{"skip with a fixed default", fixed, "", true, false},
		{"skip with systems", withVariants, "", true, true},
		{"skip with a legacy dollar placeholder", legacy, "", true, false},
		{"skip with the matching system", withVariants, snippet.OSDebian, true, true},
	} {
		if got := snippetRunNeedsDialog(test.command, snippet.ChooseSystem(test.command, test.detected), test.detected, test.skip); got != test.want {
			t.Fatalf("%s: got %v, want %v", test.name, got, test.want)
		}
	}
	variantOnly := snippet.Command{ID: "v", Name: "v", Variants: []snippet.Variant{{OS: snippet.OSDebian, Body: "apt update"}}}
	if !snippetRunNeedsDialog(variantOnly, snippet.OSDebian, snippet.OSRedHat, true) {
		t.Fatal("a Debian-only command ran on a Red Hat server without asking")
	}
	if snippetRunNeedsDialog(variantOnly, snippet.OSDebian, snippet.OSDebian, true) {
		t.Fatal("a matching variant still asked after confirmation was dropped")
	}
}

func TestSnippetParametersCanUseDefaultsOrAskInTheDialog(t *testing.T) {
	fixed := snippet.Command{ID: "restart", Name: "重启服务", Body: "systemctl restart $1",
		Args: []snippet.Arg{{Name: "服务名", Preset: "nginx", SkipPrompt: true}}}
	if snippetRunNeedsDialog(fixed, snippet.OSDefault, "", true) {
		t.Fatal("a parameter that does not ask still opened a value dialog")
	}
	body, err := snippet.Render(fixed, snippet.OSDefault, snippet.PresetValues(fixed, snippet.OSDefault))
	if err != nil || body != "systemctl restart nginx" {
		t.Fatal("the fixed default was not used", body, err)
	}
	a := &app{online: true}
	state := &snippetRunState{command: fixed, label: "Web", system: snippet.OSDefault}
	a.fillSnippetValues(state)
	a.prompt = promptState{open: true, snippet: state, target: "Web"}
	tt := ui.NewTester(a.promptView, 560, 520)
	if tt.HasText("服务名（$1）") || !tt.HasText("systemctl restart nginx") {
		t.Fatal("a fixed parameter still showed an input or lost its default", tt.Texts())
	}

	asked := fixed
	asked.Args = []snippet.Arg{{Name: "服务名", Preset: "nginx"}}
	if !snippetRunNeedsDialog(asked, snippet.OSDefault, "", true) {
		t.Fatal("a parameter marked for input did not request the dialog")
	}
	b := &app{online: true}
	draft := &snippetRunState{command: asked, label: "Web", system: snippet.OSDefault}
	b.fillSnippetValues(draft)
	b.prompt = promptState{open: true, snippet: draft, target: "Web"}
	tt = ui.NewTester(b.promptView, 560, 520)
	if !tt.HasText("服务名（$1）") || !tt.HasText("nginx") {
		t.Fatal("the prompt did not prefill the default", tt.Texts())
	}
	if err := tt.Click("参数 1 输入"); err != nil {
		t.Fatal(err)
	}
	mod := ui.Ctrl
	if runtime.GOOS == "darwin" {
		mod = ui.Super
	}
	tt.Key(mod, ui.KeyA)
	tt.Type("httpd")
	tt.Frame()
	if draft.values[0] != "httpd" || !tt.HasText("systemctl restart httpd") {
		t.Fatal("the edited value did not reach the preview", draft.values, tt.Texts())
	}
}

func TestSnippetDetectedSystemUsesTheServerInventory(t *testing.T) {
	a := &app{}
	if got := a.snippetDetectedSystem(&session{stats: &monitor.Stats{Inventory: &monitor.Inventory{OS: "Fedora Linux 41"}}}); got != snippet.OSRedHat {
		t.Fatal("inventory did not select the Red Hat family", got)
	}
	if got := a.snippetDetectedSystem(&session{}); got != "" {
		t.Fatal("a session without an inventory claimed a system", got)
	}
}

func TestDroppedCommandConfirmationIsPerCommand(t *testing.T) {
	quiet := snippet.Command{ID: "quiet", Name: "quiet", Body: "uptime", SkipConfirm: true}
	loud := snippet.Command{ID: "loud", Name: "loud", Body: "systemctl restart nginx"}
	if !quiet.SkipConfirm || loud.SkipConfirm {
		t.Fatal("one command's dropped reminder leaked to another", quiet.SkipConfirm, loud.SkipConfirm)
	}
	if snippetRunNeedsDialog(quiet, snippet.OSDefault, "", quiet.SkipConfirm) {
		t.Fatal("a command that dropped its reminder still asked")
	}
	if !snippetRunNeedsDialog(loud, snippet.OSDefault, "", loud.SkipConfirm) {
		t.Fatal("an untouched command stopped asking")
	}
}

func TestSnippetConfirmationCheckboxDefaultsAndBothDirections(t *testing.T) {
	var legacy snippet.Command
	if err := json.Unmarshal([]byte(`{"id":"legacy","name":"legacy","body":"uptime"}`), &legacy); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name    string
		command *snippet.Command
	}{
		{name: "new command"},
		{name: "legacy command", command: &legacy},
		{name: "confirmation disabled", command: &snippet.Command{ID: "quiet", Name: "quiet", Body: "uptime", SkipConfirm: true}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			a := &app{}
			if tc.command == nil {
				a.newSnippet()
			} else {
				a.editSnippet(*tc.command)
			}
			initial := a.snippetForm.SkipConfirm
			if tc.name != "confirmation disabled" && initial {
				t.Fatal("a new or legacy command stopped asking by default")
			}
			tt := ui.NewTester(a.snippetDialog, 700, 720)
			for _, want := range []bool{!initial, initial} {
				if err := tt.Click("发送时询问"); err != nil {
					t.Fatal(err)
				}
				if a.snippetForm.SkipConfirm != want {
					t.Fatal("editor confirmation did not toggle", a.snippetForm.SkipConfirm, want)
				}
			}
		})
	}
	for _, initial := range []bool{false, true} {
		command := snippet.Command{ID: "run", Name: "run", Body: "echo $1", SkipConfirm: initial, Args: []snippet.Arg{{Preset: "value"}}}
		state := &snippetRunState{command: command, label: "Web", system: snippet.OSDefault, values: []string{"value"}, skip: initial}
		a := &app{prompt: promptState{open: true, snippet: state}}
		tt := ui.NewTester(a.promptView, 720, 620)
		for _, want := range []bool{!initial, initial} {
			if err := tt.Click("发送时询问"); err != nil {
				t.Fatal(err)
			}
			if state.skip != want || state.command.SkipConfirm != initial {
				t.Fatal("run confirmation did not toggle independently", state.skip, state.command.SkipConfirm, want)
			}
		}
	}
}

func TestSnippetRunDialogPrefillsPresetsAndBlocksInvalidValues(t *testing.T) {
	command := snippet.Command{ID: "kill", Name: "结束进程", Body: "kill -$1 $2",
		Args: []snippet.Arg{{Type: snippet.TypeInt, Preset: "9"}, {Type: snippet.TypeInt, Preset: "4242"}}}
	state := &snippetRunState{command: command, label: "Web", system: snippet.OSDefault, skip: false}
	a := &app{online: true, sessions: []*session{{name: "Web"}}}
	a.fillSnippetValues(state)
	a.prompt = promptState{open: true, snippet: state, target: state.label}
	tt := ui.NewTester(a.promptView, 560, 560)
	if !tt.HasText("kill -9 4242") {
		t.Fatal("preview did not use the presets", tt.Texts())
	}
	if !tt.HasText("发送时询问") {
		t.Fatal("the confirmation checkbox is missing")
	}
	if err := tt.Click("参数 1 输入"); err != nil {
		t.Fatal(err)
	}
	mod := ui.Ctrl
	if runtime.GOOS == "darwin" {
		mod = ui.Super
	}
	tt.Key(mod, ui.KeyA)
	tt.Type("many")
	tt.Frame()
	if state.values[0] != "many" {
		t.Fatal("parameter edit was not kept", state.values)
	}
	if err := tt.Click("发送并执行"); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(state.error, "需要整数") || !a.prompt.open {
		t.Fatal("invalid value was executed or lost its explanation", state.error)
	}
	state.values[0] = "15"
	if err := tt.Click("发送时询问"); err != nil {
		t.Fatal(err)
	}
	if !state.skip {
		t.Fatal("confirmation checkbox did not change")
	}
	if err := tt.Click("发送并执行"); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(state.error, "原终端已关闭") || !a.prompt.open {
		t.Fatal("a closed terminal was treated as a successful send", state.error)
	}
}

func TestSnippetEditorTypingAPlaceholderCreatesItsInput(t *testing.T) {
	a := &app{online: true}
	a.newSnippet()
	a.snippetForm.Name = "更新"
	tt := ui.NewTester(a.snippetDialog, 700, 720)
	if err := tt.Click("命令内容"); err != nil {
		t.Fatal(err)
	}
	tt.Type("sudo apt upgrade -y $1")
	tt.Frame()
	if a.snippetForm.Body != "sudo apt upgrade -y $1" || len(a.snippetForm.Args) != 1 {
		t.Fatal("typing a placeholder did not create its parameter", a.snippetForm)
	}
	if !tt.HasText("参数") || !tt.HasText("$1") || !tt.HasText("发送时填写") {
		t.Fatal("the parameter row is not visible", tt.Texts())
	}
	if err := tt.Click("发送时填写"); err != nil {
		t.Fatal(err)
	}
	if !a.snippetForm.Args[0].SkipPrompt {
		t.Fatal("unchecking the prompt did not use the default")
	}
}

func TestSnippetEditorDetectsPlaceholdersOnDemandAndKeepsSystemVariants(t *testing.T) {
	a := &app{online: true, snippets: commandFixture()}
	a.newSnippet()
	a.snippetForm.Name = "更新"
	a.snippetForm.Body = "sudo apt upgrade -y $1"
	tt := ui.NewTester(a.snippetDialog, 700, 720)
	if tt.HasText("参数") {
		t.Fatal("opening a command created parameter rows before any body edit")
	}
	if err := tt.Click("识别占位符"); err != nil {
		t.Fatal(err)
	}
	if !tt.HasText("参数") || !tt.HasText("$1") || !tt.HasText("文本") {
		t.Fatal("placeholder did not create a parameter row", tt.Texts())
	}
	if err := tt.Click("选择分类"); err != nil {
		t.Fatal(err)
	}
	if err := tt.Click("运维"); err != nil {
		t.Fatal(err)
	}
	if a.snippetForm.Group != "运维" {
		t.Fatal("the category dropdown did not select a group", a.snippetForm.Group)
	}
	if err := tt.Click("添加系统命令"); err != nil {
		t.Fatal(err)
	}
	if len(a.snippetForm.Variants) != 1 || a.snippetForm.Variants[0].OS != snippet.OSDebian {
		t.Fatal("system variant was not added", a.snippetForm.Variants)
	}
	if err := tt.Click("系统选择 1"); err != nil {
		t.Fatal(err)
	}
	if err := tt.Click("RHEL / Fedora（dnf 或 yum）"); err != nil {
		t.Fatal(err)
	}
	if a.snippetForm.Variants[0].OS != snippet.OSRedHat {
		t.Fatal("the system dropdown did not change the variant", a.snippetForm.Variants[0].OS)
	}
	a.snippetForm.Variants[0].Body = "sudo dnf upgrade -y $1"
	a.snippetForm.Args[0].Preset = "nginx"
	a.snippetForm.Variants = append(a.snippetForm.Variants, snippet.Variant{OS: snippet.OSRedHat})
	a.saveSnippetForm()
	if a.snippetError != "" {
		t.Fatal("a valid command was rejected", a.snippetError)
	}
	command := a.snippetForm
	command.Variants = command.Variants[:1]
	if err := snippet.Validate(command); err != nil {
		t.Fatal("a system-specific command was rejected", err)
	}
	a.snippetForm.Name = " "
	if err := snippet.Validate(a.snippetForm); err == nil {
		t.Fatal("an unnamed command was accepted")
	}
}
