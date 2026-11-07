//go:build terminal_integration

package desktop

import (
	"reflect"
	"strings"
	"testing"

	"github.com/egoist/mygo/plugins/terminal"
	"github.com/egoist/mygo/ui"
	"sshclient/internal/broker"
	"sshclient/internal/monitor"
	"sshclient/internal/snippet"
)

// The headless UI tests stop at the dialog and the closed-terminal guard. This
// one exercises the real Ghostty input path used by a double click.
func TestSavedCommandsRunInTheActiveTerminal(t *testing.T) {
	recorder := newTerminalRecorder()
	term, err := terminal.New(terminal.Options{Conn: recorder, ClipboardRead: terminal.ClipboardDeny, NoClipboardWrite: true})
	if err != nil {
		t.Fatal(err)
	}
	defer term.Close()
	a := &app{online: true, sessions: []*session{{host: "web", name: "Web", term: term}}}
	a.runSnippet(snippet.Command{ID: "uptime", Name: "uptime", Body: "uptime", SkipConfirm: true})
	expectTerminalInput(t, recorder, "uptime\r")

	a.runSnippet(snippet.Command{ID: "restart", Name: "restart", Body: "systemctl restart $1",
		Args: []snippet.Arg{{Type: snippet.TypeText, Preset: "nginx"}}})
	if a.prompt.snippet == nil || recorder.take() != "" {
		t.Fatal("the confirmation dialog sent the command before it was confirmed")
	}
	a.prompt.snippet.values[0] = "curl"
	a.executeSnippetRun(a.prompt.snippet)
	expectTerminalInput(t, recorder, "systemctl restart curl\r")

	a.sessions[0].stats = &monitor.Stats{Inventory: &monitor.Inventory{OS: "Fedora Linux 41"}}
	a.runSnippet(snippet.Command{ID: "update", Name: "update", Body: "sudo apt upgrade -y",
		Variants: []snippet.Variant{{OS: snippet.OSRedHat, Body: "sudo dnf upgrade -y"}}})
	if a.prompt.snippet == nil || a.prompt.snippet.system != snippet.OSRedHat {
		t.Fatal("the detected system did not select the Red Hat variant")
	}
	a.executeSnippetRun(a.prompt.snippet)
	expectTerminalInput(t, recorder, "sudo dnf upgrade -y\r")
}

func TestCommandHistorySelectionEnterAndFillNeverExecute(t *testing.T) {
	recorder := newTerminalRecorder()
	term, err := terminal.New(terminal.Options{Conn: recorder, ClipboardRead: terminal.ClipboardDeny, NoClipboardWrite: true})
	if err != nil {
		t.Fatal(err)
	}
	defer term.Close()
	target := &session{host: "web", name: "Web", term: term}
	a := &app{online: true, sessions: []*session{target}}
	a.runSnippet(snippet.Command{ID: "container", Name: "查看容器", Body: "pct config $1", Args: []snippet.Arg{{Name: "ContainerID", Type: snippet.TypeInt, Preset: "100"}}})
	state := a.prompt.snippet
	state.histories = [][]string{{"101"}}
	tt := ui.NewTester(a.promptView, 720, 620)
	if err := tt.Click("参数 1 历史"); err != nil {
		t.Fatal(err)
	}
	if err := tt.Click("101"); err != nil {
		t.Fatal(err)
	}
	if err := tt.Click("参数 1 输入"); err != nil {
		t.Fatal(err)
	}
	tt.Key(0, ui.KeyEnter)
	if recorder.take() != "" || a.prompt.snippet != state {
		t.Fatal("history or parameter Enter executed a command")
	}
	if err := tt.Click("填入终端"); err != nil {
		t.Fatal(err)
	}
	expectTerminalInput(t, recorder, "pct config 101")
	state.command.Body = "echo one\necho two"
	a.submitSnippetRun(state, false)
	if state.error == "" || recorder.take() != "" {
		t.Fatal("multiline fill could execute a line")
	}
}

func TestExplicitCommandTargetResetsInputsAndUsesOnlyThatTerminal(t *testing.T) {
	one, two := newTerminalRecorder(), newTerminalRecorder()
	first, err := terminal.New(terminal.Options{Conn: one, ClipboardRead: terminal.ClipboardDeny, NoClipboardWrite: true})
	if err != nil {
		t.Fatal(err)
	}
	defer first.Close()
	second, err := terminal.New(terminal.Options{Conn: two, ClipboardRead: terminal.ClipboardDeny, NoClipboardWrite: true})
	if err != nil {
		t.Fatal(err)
	}
	defer second.Close()
	a := &app{online: true, sessions: []*session{{host: "a", name: "Server A", term: first}, {host: "b", name: "Server B", term: second}}}
	a.runSnippet(snippet.Command{ID: "container", Name: "查看容器", Body: "pct config $1", Args: []snippet.Arg{{Name: "ContainerID", Preset: "100"}}})
	state := a.prompt.snippet
	state.values[0], state.histories = "200", [][]string{{"200"}}
	a.active = 1
	if state.target != a.sessions[0] {
		t.Fatal("changing focus silently redirected the command")
	}
	tt := ui.NewTester(a.promptView, 720, 620)
	if err := tt.Click("命令目标会话"); err != nil {
		t.Fatal(err)
	}
	if err := tt.Click("Server B"); err != nil {
		t.Fatal(err)
	}
	if state.target != a.sessions[1] || state.values[0] != "100" || len(state.histories[0]) != 0 {
		t.Fatal("explicit target change retained another server's input/history")
	}
	a.executeSnippetRun(state)
	expectTerminalInput(t, two, "pct config 100\r")
	if one.take() != "" {
		t.Fatal("command also reached the original terminal")
	}
}

func TestSnippetRunConfirmationSavesOnlyAfterExecution(t *testing.T) {
	for _, tc := range []struct {
		name      string
		initial   bool
		wantSkip  bool
		retryBusy bool
		action    string
		wantInput string
		wantError string
	}{
		{name: "disable confirmation", wantSkip: true, action: "发送并执行", wantInput: "echo 100\r"},
		{name: "restore confirmation", initial: true, action: "发送并执行", wantInput: "echo 100\r"},
		{name: "disable confirmation after busy", wantSkip: true, retryBusy: true, action: "发送并执行", wantInput: "echo 100\r"},
		{name: "restore confirmation after busy", initial: true, retryBusy: true, action: "发送并执行", wantInput: "echo 100\r"},
		{name: "unchanged confirmation", action: "发送并执行", wantInput: "echo 100\r"},
		{name: "fill only", wantSkip: true, action: "填入终端", wantInput: "echo 100"},
		{name: "cancel", wantSkip: true, action: "取消"},
		{name: "invalid parameter", wantSkip: true, action: "发送并执行", wantError: "需要整数"},
		{name: "closed target", wantSkip: true, action: "发送并执行", wantError: "原终端已关闭"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			h := newConnectionFlowHarness(t)
			a := h.a
			t.Cleanup(a.stopSnippetHistoryWrites)
			recorder := newTerminalRecorder()
			term, err := terminal.New(terminal.Options{Conn: recorder, ClipboardRead: terminal.ClipboardDeny, NoClipboardWrite: true})
			if err != nil {
				t.Fatal(err)
			}
			target := &session{host: "confirmation-fixture", name: "Confirmation fixture", term: term}
			a.sessions = []*session{target}
			command := snippet.Command{ID: "confirmation-run", Name: "confirmation run", Body: "echo $1", SkipConfirm: tc.initial,
				Args: []snippet.Arg{{Type: snippet.TypeInt, Preset: "100", NoHistory: true}}}
			other := snippet.Command{ID: "confirmation-other", Name: "other command", Body: "uptime", SkipConfirm: !tc.initial}
			for _, saved := range []snippet.Command{command, other} {
				if err := h.server.Vault.SaveSnippet(broker.VaultPath(h.dir), saved, false); err != nil {
					t.Fatal(err)
				}
			}
			a.snippets = []snippet.Command{command, other}
			a.editSnippet(snippet.Command{ID: "draft", Name: "unrelated draft", Body: "echo draft"})
			draft := a.snippetForm.Clone()
			a.runSnippet(command)
			state := a.prompt.snippet
			if state == nil || recorder.take() != "" {
				t.Fatal("the parameter guard did not keep the command in its dialog")
			}
			tt := ui.NewTester(a.promptView, 720, 620)
			if tc.wantSkip != tc.initial {
				if err := tt.Click("发送时询问"); err != nil {
					t.Fatal(err)
				}
			}
			if state.skip != tc.wantSkip {
				t.Fatal("run dialog confirmation did not change", state.skip, tc.wantSkip)
			}
			if tc.retryBusy {
				a.busy = true
				a.submitSnippetRun(state, true)
				if !a.busy || a.prompt.snippet != state || !a.prompt.open || state.skip != tc.wantSkip || state.values[0] != "100" {
					t.Fatal("busy submission lost the pending confirmation or inputs", a.prompt, state)
				}
				if !strings.Contains(state.error, "正在保存其他更改") || recorder.take() != "" {
					t.Fatal("busy submission sent input or lost its explanation", state.error)
				}
				stored, ok := snippetWithID(h.server.Vault.Snippets(), command.ID)
				if !ok || stored.SkipConfirm != tc.initial {
					t.Fatal("busy submission persisted the confirmation before execution", stored)
				}
				a.busy = false
				// The direct submit added an error row; refresh button bounds before clicking.
				tt.Frame()
			}
			if tc.name == "invalid parameter" {
				state.values[0] = "invalid"
			}
			if tc.name == "closed target" {
				target.closed = true
			}
			if err := tt.Click(tc.action); err != nil {
				t.Fatal(err)
			}
			shouldSave := tc.action == "发送并执行" && tc.wantError == "" && tc.wantSkip != tc.initial
			if a.busy != shouldSave {
				t.Fatal("confirmation persistence did not match successful execution", a.busy, shouldSave)
			}
			if shouldSave {
				h.wait(func() bool { return !a.busy })
			}
			wantStored := tc.initial
			if shouldSave {
				wantStored = tc.wantSkip
			}
			stored, ok := snippetWithID(h.server.Vault.Snippets(), command.ID)
			if !ok || stored.SkipConfirm != wantStored || state.command.SkipConfirm != tc.initial {
				t.Fatal("run confirmation changed the wrong command state", stored, state.command.SkipConfirm, wantStored)
			}
			untouched, ok := snippetWithID(h.server.Vault.Snippets(), other.ID)
			if !ok || !reflect.DeepEqual(untouched, other) || !a.snippetOpen || !reflect.DeepEqual(a.snippetForm, draft) {
				t.Fatal("run confirmation changed another command or editor draft", untouched, a.snippetForm)
			}
			if tc.wantError != "" {
				if !strings.Contains(state.error, tc.wantError) || a.prompt.snippet != state || !a.prompt.open {
					t.Fatal("failed execution lost the dialog or its error", state.error)
				}
			} else if a.prompt.snippet != nil || a.prompt.open {
				t.Fatal("completed action left the run dialog open")
			}
			if tc.wantInput != "" {
				expectTerminalInput(t, recorder, tc.wantInput)
			} else if got := recorder.take(); got != "" {
				t.Fatal("unsuccessful action sent terminal input", got)
			}
		})
	}
}
