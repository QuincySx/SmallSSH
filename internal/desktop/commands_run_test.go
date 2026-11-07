package desktop

import (
	"reflect"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"
	"sshclient/internal/approval"
	"sshclient/internal/broker"
	"sshclient/internal/snippet"
)

func waitParameterHistoryBarrier(t *testing.T, started <-chan struct{}, message string) {
	t.Helper()
	timer := time.NewTimer(5 * time.Second)
	defer timer.Stop()
	select {
	case <-started:
	case <-timer.C:
		t.Fatal(message)
	}
}

func newParameterHistoryFixture(t *testing.T) (*connectionFlowHarness, snippet.Command, *session) {
	t.Helper()
	h := newConnectionFlowHarness(t)
	t.Cleanup(h.a.stopSnippetHistoryWrites)
	command := snippet.Command{
		ID: "parameter-history-fixture", Name: "parameter history fixture", Body: "echo $1 $2",
		Args: []snippet.Arg{{Name: "first"}, {Name: "second"}},
	}
	if err := h.server.Vault.SaveSnippet(broker.VaultPath(h.dir), command, false); err != nil {
		t.Fatal(err)
	}
	return h, command, &session{host: "parameter-history-host"}
}

func saveParameterHistoryFixture(t *testing.T, h *connectionFlowHarness, command snippet.Command, target *session, values []string) {
	t.Helper()
	if err := h.server.Vault.SaveParameterHistory(broker.VaultPath(h.dir), command, snippetHistoryTarget(target), snippet.OSDefault, values, -1); err != nil {
		t.Fatal(err)
	}
}

func newParameterHistoryState(command snippet.Command, target *session, values ...string) *snippetRunState {
	return &snippetRunState{command: command, target: target, system: snippet.OSDefault, values: values}
}

func TestCommandParameterHistoriesRefillOnlyTheirOwnInput(t *testing.T) {
	c := snippet.Command{ID: "container", Name: "查看容器", Body: "pct config $1 && echo $2", Args: []snippet.Arg{{Name: "ContainerID", Type: snippet.TypeInt, Preset: "100"}, {Name: "Label", Preset: "default"}}}
	a := &app{online: true}
	state := &snippetRunState{command: c, label: "Web", system: snippet.OSDefault}
	a.fillSnippetValues(state)
	state.histories = [][]string{{"101", "200"}, {"web", "database"}}
	a.prompt = promptState{open: true, snippet: state, target: state.label}
	tt := ui.NewTester(a.promptView, 720, 650)
	if !tt.HasText("pct config [$1 · ContainerID] && echo [$2 · Label]") || !tt.HasText("发送到") || !tt.HasText("发送并执行") {
		t.Fatal("parameter hints or explicit target/send actions are missing", tt.Texts())
	}
	if err := tt.Click("参数 1 历史"); err != nil {
		t.Fatal(err)
	}
	if tt.HasText("database") {
		t.Fatal("another parameter's values appeared in this history")
	}
	if err := tt.Click("200"); err != nil {
		t.Fatal(err)
	}
	if state.values[0] != "200" || state.values[1] != "default" || !a.prompt.open || state.error != "" || !tt.HasText("pct config 200 && echo default") {
		t.Fatal("history selection executed or filled the wrong parameter", state.values, state.error)
	}
	if err := tt.Click("参数 2 历史"); err != nil {
		t.Fatal(err)
	}
	if err := tt.Click("database"); err != nil {
		t.Fatal(err)
	}
	if state.values[0] != "200" || state.values[1] != "database" || !a.prompt.open {
		t.Fatal("parameter histories were not independent", state.values)
	}
}

func TestParameterHistoryWritesAndClearAreFIFO(t *testing.T) {
	h, command, target := newParameterHistoryFixture(t)
	a := h.a
	state := newParameterHistoryState(command, target, "fifo-first", "fifo-second")
	a.prompt = promptState{open: true, snippet: state}
	firstStarted, firstRelease := make(chan struct{}), make(chan struct{})
	secondStarted, secondRelease := make(chan struct{}), make(chan struct{})
	releaseBarrier := func(barrier chan struct{}) {
		select {
		case <-barrier:
		default:
			close(barrier)
		}
	}
	t.Cleanup(func() {
		releaseBarrier(firstRelease)
		releaseBarrier(secondRelease)
		a.stopSnippetHistoryWrites()
	})
	if !a.queueSnippetHistory(func() error {
		close(firstStarted)
		<-firstRelease
		return nil
	}, func(error) {}) {
		t.Fatal("history queue rejected the FIFO barrier")
	}
	waitParameterHistoryBarrier(t, firstStarted, "FIFO barrier did not block the history queue head")
	a.recordSnippetHistory(state)
	a.loadSnippetHistory(state)
	if got := len(a.snippetHistoryWrites); got != 2 {
		t.Fatalf("expected record and load to wait behind the FIFO barrier, got %d pending jobs", got)
	}
	releaseBarrier(firstRelease)
	key := snippetHistoryTarget(target)
	h.wait(func() bool {
		return reflect.DeepEqual(state.histories, [][]string{{"fifo-first"}, {"fifo-second"}}) &&
			reflect.DeepEqual(h.server.Vault.ParameterHistory(command.ID, key, snippet.OSDefault, 0), []string{"fifo-first"}) &&
			reflect.DeepEqual(h.server.Vault.ParameterHistory(command.ID, key, snippet.OSDefault, 1), []string{"fifo-second"})
	})
	if !a.queueSnippetHistory(func() error {
		close(secondStarted)
		<-secondRelease
		return nil
	}, func(error) {}) {
		t.Fatal("history queue rejected the second FIFO barrier")
	}
	waitParameterHistoryBarrier(t, secondStarted, "second FIFO barrier did not block the history queue head")
	state.values = []string{"fifo-new-first", "fifo-new-second"}
	a.recordSnippetHistory(state)
	a.clearSnippetHistory(state, 0)
	a.loadSnippetHistory(state)
	if got := len(a.snippetHistoryWrites); got != 3 {
		t.Fatalf("expected record, clear, and load to wait behind the FIFO barrier, got %d pending jobs", got)
	}
	releaseBarrier(secondRelease)
	h.wait(func() bool {
		return len(state.histories[0]) == 0 &&
			reflect.DeepEqual(state.histories[1], []string{"fifo-new-second", "fifo-second"}) &&
			len(h.server.Vault.ParameterHistory(command.ID, key, snippet.OSDefault, 0)) == 0 &&
			reflect.DeepEqual(h.server.Vault.ParameterHistory(command.ID, key, snippet.OSDefault, 1), []string{"fifo-new-second", "fifo-second"})
	})
	if len(h.server.Vault.ParameterHistory(command.ID, key, snippet.OSDefault, 0)) != 0 || len(h.server.Vault.ParameterHistory(command.ID, key, snippet.OSDefault, 1)) != 2 {
		t.Fatal("a queued old write restored cleared history or cleared another input")
	}
	if len(state.histories[0]) != 0 || !reflect.DeepEqual(state.histories[1], []string{"fifo-new-second", "fifo-second"}) {
		t.Fatal("clear completion did not leave the latest UI history for the other input", state.histories)
	}
	if !reflect.DeepEqual(state.values, []string{"fifo-new-first", "fifo-new-second"}) {
		t.Fatal("history write or clear changed user input", state.values)
	}
}

func TestAsyncParameterHistoryLoadCompletesAtomically(t *testing.T) {
	h, command, target := newParameterHistoryFixture(t)
	a := h.a
	saveParameterHistoryFixture(t, h, command, target, []string{"load-first", "load-second"})
	decoy := &session{host: "decoy-history-host"}
	saveParameterHistoryFixture(t, h, command, decoy, []string{"decoy-first", "decoy-second"})
	state := newParameterHistoryState(command, target, "typed-first", "typed-second")
	a.prompt = promptState{open: true, snippet: state}
	busy := a.busy
	a.loadSnippetHistory(state)
	done := queuedSnippetUpdate(t, h)
	if len(state.histories) != 2 || len(state.histories[0]) != 0 || len(state.histories[1]) != 0 {
		t.Fatal("history became visible before its UI completion ran", state.histories)
	}
	if a.busy != busy {
		t.Fatal("a pending history load changed busy state", busy, a.busy)
	}
	if !reflect.DeepEqual(state.values, []string{"typed-first", "typed-second"}) {
		t.Fatal("a pending history load changed user input", state.values)
	}
	done()
	if !reflect.DeepEqual(state.histories, [][]string{{"load-first"}, {"load-second"}}) {
		t.Fatalf("history load returned the wrong context: %#v", state.histories)
	}
	if !reflect.DeepEqual(state.values, []string{"typed-first", "typed-second"}) || state.target != target || state.system != snippet.OSDefault {
		t.Fatal("history completion changed input or context", state.values, state.target, state.system)
	}
}

func TestStaleParameterHistoryLoadResultsAreRejected(t *testing.T) {
	cases := []struct {
		name    string
		mutate  func(*app, *snippetRunState)
		applied bool
	}{
		{name: "unchanged state", applied: true},
		{name: "replaced prompt state", mutate: func(a *app, _ *snippetRunState) {
			a.prompt.snippet = &snippetRunState{}
		}},
		{name: "replaced target", mutate: func(_ *app, state *snippetRunState) {
			state.target = &session{host: "replacement-history-host"}
		}},
		{name: "changed target endpoint", mutate: func(_ *app, state *snippetRunState) {
			state.target.target.Endpoint = "mutated@127.0.0.1:22"
		}},
		{name: "changed target fingerprint", mutate: func(_ *app, state *snippetRunState) {
			state.target.target.HostFingerprint = "SHA256:mutated"
		}},
		{name: "changed system", mutate: func(_ *app, state *snippetRunState) {
			state.system = "mutated-system"
		}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			h, command, target := newParameterHistoryFixture(t)
			a := h.a
			saveParameterHistoryFixture(t, h, command, target, []string{"stale-first", "stale-second"})
			state := newParameterHistoryState(command, target, "typed-first", "typed-second")
			state.error = "existing error"
			a.prompt = promptState{open: true, snippet: state}
			a.loadSnippetHistory(state)
			done := queuedSnippetUpdate(t, h)
			if tc.mutate != nil {
				tc.mutate(a, state)
			}
			done()
			if tc.applied {
				if !reflect.DeepEqual(state.histories, [][]string{{"stale-first"}, {"stale-second"}}) {
					t.Fatalf("current history result was not applied: %#v", state.histories)
				}
			} else if len(state.histories) != 2 || len(state.histories[0]) != 0 || len(state.histories[1]) != 0 {
				t.Fatalf("stale history result polluted UI state: %#v", state.histories)
			}
			if !reflect.DeepEqual(state.values, []string{"typed-first", "typed-second"}) || state.error != "existing error" {
				t.Fatal("history completion changed values or error", state.values, state.error)
			}
		})
	}
}

func TestClearParameterHistoryRejectsLateLoadCompletion(t *testing.T) {
	h, command, target := newParameterHistoryFixture(t)
	a := h.a
	saveParameterHistoryFixture(t, h, command, target, []string{"clear-first", "clear-second"})
	state := newParameterHistoryState(command, target, "typed-first", "typed-second")
	a.prompt = promptState{open: true, snippet: state}
	a.loadSnippetHistory(state)
	lateLoad := queuedSnippetUpdate(t, h)
	state.histories = [][]string{{"clear-first"}, {"clear-second"}}
	a.clearSnippetHistory(state, 0)
	clearDone := queuedSnippetUpdate(t, h)
	clearDone()
	key := snippetHistoryTarget(target)
	if len(state.histories[0]) != 0 || !reflect.DeepEqual(state.histories[1], []string{"clear-second"}) {
		t.Fatal("clear completion did not update only the requested UI parameter", state.histories)
	}
	if len(h.server.Vault.ParameterHistory(command.ID, key, snippet.OSDefault, 0)) != 0 ||
		!reflect.DeepEqual(h.server.Vault.ParameterHistory(command.ID, key, snippet.OSDefault, 1), []string{"clear-second"}) {
		t.Fatal("clear completion did not update only the requested broker parameter", h.server.Vault.ParameterHistory(command.ID, key, snippet.OSDefault, 0))
	}
	lateLoad()
	if len(state.histories[0]) != 0 || !reflect.DeepEqual(state.histories[1], []string{"clear-second"}) {
		t.Fatal("a late load completion restored cleared UI history", state.histories)
	}
	if len(h.server.Vault.ParameterHistory(command.ID, key, snippet.OSDefault, 0)) != 0 ||
		!reflect.DeepEqual(h.server.Vault.ParameterHistory(command.ID, key, snippet.OSDefault, 1), []string{"clear-second"}) {
		t.Fatal("a late load completion changed broker history", h.server.Vault.ParameterHistory(command.ID, key, snippet.OSDefault, 0))
	}
	if !reflect.DeepEqual(state.values, []string{"typed-first", "typed-second"}) {
		t.Fatal("history clear changed user input", state.values)
	}
}

func TestParameterHistoryTargetUsesActualConnectionIdentity(t *testing.T) {
	a := &session{host: "same-profile", name: "same label", target: approval.Operation{Endpoint: "user@127.0.0.1:22", HostFingerprint: "SHA256:a"}}
	b := *a
	b.name = "renamed display label"
	if snippetHistoryTarget(a) != snippetHistoryTarget(&b) {
		t.Fatal("renaming a label changed server identity")
	}
	b.target.Endpoint = "other@127.0.0.1:22"
	if snippetHistoryTarget(a) == snippetHistoryTarget(&b) {
		t.Fatal("different accounts shared parameter history")
	}
	b.target.Endpoint, b.target.HostFingerprint = a.target.Endpoint, "SHA256:b"
	if snippetHistoryTarget(a) == snippetHistoryTarget(&b) {
		t.Fatal("different pinned host identities shared history")
	}
}

func TestSnippetEditorCanOptOutOfParameterHistory(t *testing.T) {
	a := &app{online: true}
	a.newSnippet()
	a.snippetForm.Name, a.snippetForm.Body = "test", "echo $1"
	a.syncSnippetArgs()
	tt := ui.NewTester(a.snippetDialog, 720, 760)
	if err := tt.Click("记录输入历史"); err != nil {
		t.Fatal(err)
	}
	if !a.snippetForm.Args[0].NoHistory {
		t.Fatal("history preference did not reach the command definition")
	}
}
