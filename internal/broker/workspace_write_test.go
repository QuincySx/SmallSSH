package broker_test

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"

	"sshclient/internal/approval"
	"sshclient/internal/auditlog"
	"sshclient/internal/broker"
	"sshclient/internal/client"
	"sshclient/internal/protocol"
	"sshclient/internal/snippet"
	"sshclient/internal/vault"
)

func signedWorkspaceWrite(t *testing.T, cli client.Client, change *protocol.WorkspaceChange) broker.Message {
	t.Helper()
	return signedWorkspaceQuery(t, cli, cli.Owner, broker.Message{
		Action:          "workspaceWrite",
		WorkspaceChange: change,
	})
}

func requireWorkspaceHistory(t *testing.T, session *vault.Session, commandID, target, system string, parameter int, want ...string) {
	t.Helper()
	got := session.ParameterHistory(commandID, target, system, parameter)
	if !slices.Equal(got, want) {
		t.Fatalf("history %s/%s/%s/%d = %#v, want %#v", commandID, target, system, parameter, got, want)
	}
}

func requireSeedWorkspaceUnchanged(t *testing.T, server *broker.Server, seed workspaceSeed, profile vault.Profile) {
	t.Helper()
	commands := server.Vault.Snippets()
	if len(commands) != 2 {
		t.Fatalf("command count = %d, want 2", len(commands))
	}
	byID := make(map[string]snippet.Command, len(commands))
	for _, command := range commands {
		byID[command.ID] = command
	}
	if !reflect.DeepEqual(byID[seed.commandA.ID], seed.commandA) || !reflect.DeepEqual(byID[seed.commandB.ID], seed.commandB) {
		t.Fatalf("commands changed after rejected writes: %#v", commands)
	}

	firstNote, err := server.Vault.ServerNote(profile.ID)
	if err != nil || firstNote.Manual != "first note" {
		t.Fatalf("first note = %#v, %v", firstNote, err)
	}
	secondNote, err := server.Vault.ServerNote(seed.secondProfile.ID)
	if err != nil || secondNote.Manual != "second note" {
		t.Fatalf("second note = %#v, %v", secondNote, err)
	}

	requireWorkspaceHistory(t, server.Vault, seed.commandA.ID, seed.targetA, snippet.OSDefault, 0, "alpha")
	requireWorkspaceHistory(t, server.Vault, seed.commandA.ID, seed.targetA, snippet.OSDefault, 1, "one")
	requireWorkspaceHistory(t, server.Vault, seed.commandA.ID, seed.targetB, snippet.OSDebian, 0, "bravo")
	requireWorkspaceHistory(t, server.Vault, seed.commandA.ID, seed.targetB, snippet.OSDebian, 1, "two")
	requireWorkspaceHistory(t, server.Vault, seed.commandB.ID, seed.targetA, snippet.OSDefault, 0, "charlie")
}

func TestWorkspaceWriteRequiresVerifiedOwnerAndExactPayload(t *testing.T) {
	cli, server, profile := newWorkspaceHarness(t)
	seed := seedWorkspaceReads(t, cli, server, profile)
	ctx := context.Background()

	anonymous := client.Client{Dir: cli.Dir}
	changes := []*protocol.WorkspaceChange{
		{Kind: "snippet-save", Command: &snippet.Command{ID: "anonymous-command", Name: "Anonymous", Body: "echo anonymous"}},
		{Kind: "snippet-delete", CommandID: seed.commandA.ID},
		{Kind: "server-note-save", HostID: profile.ID, Note: &vault.ServerNote{Manual: "anonymous note"}},
		{Kind: "parameter-history-save", Command: &seed.commandA, Target: seed.targetA, System: snippet.OSDefault, Inputs: []string{"anonymous input", "one"}},
		{Kind: "parameter-history-clear", Command: &seed.commandA, Target: seed.targetA, System: snippet.OSDefault, Parameter: 0},
	}
	for _, change := range changes {
		if _, err := anonymous.Call(ctx, broker.Message{
			Action:          "workspaceWrite",
			WorkspaceChange: change,
			Operation:       approval.Operation{Client: "所有者"},
		}); err == nil {
			t.Fatalf("anonymous %s write accepted", change.Kind)
		}
	}
	requireSeedWorkspaceUnchanged(t, server, seed, profile)

	wrong := signedWorkspaceWrite(t, cli, &protocol.WorkspaceChange{
		Kind:    "snippet-save",
		Command: &snippet.Command{ID: "wrong-proof-command", Name: "Wrong proof", Body: "echo wrong"},
	})
	wrong.Proof[0] ^= 0xff
	if _, err := cli.Call(ctx, wrong); err == nil {
		t.Fatal("write accepted with a tampered proof")
	}
	requireSeedWorkspaceUnchanged(t, server, seed, profile)

	tamperTests := []struct {
		name   string
		change func() *protocol.WorkspaceChange
		mutate func(*protocol.WorkspaceChange)
	}{
		{
			"command body",
			func() *protocol.WorkspaceChange {
				command := seed.commandA.Clone()
				return &protocol.WorkspaceChange{Kind: "snippet-save", Command: &command}
			},
			func(change *protocol.WorkspaceChange) { change.Command.Body = "echo changed after signing" },
		},
		{
			"note manual",
			func() *protocol.WorkspaceChange {
				return &protocol.WorkspaceChange{Kind: "server-note-save", HostID: profile.ID, Note: &vault.ServerNote{Manual: "signed note"}}
			},
			func(change *protocol.WorkspaceChange) { change.Note.Manual = "changed after signing" },
		},
		{
			"target",
			func() *protocol.WorkspaceChange {
				command := seed.commandA.Clone()
				return &protocol.WorkspaceChange{Kind: "parameter-history-save", Command: &command, Target: seed.targetA, System: snippet.OSDefault, Inputs: []string{"signed input"}}
			},
			func(change *protocol.WorkspaceChange) { change.Target = seed.targetB },
		},
		{
			"inputs",
			func() *protocol.WorkspaceChange {
				command := seed.commandA.Clone()
				return &protocol.WorkspaceChange{Kind: "parameter-history-save", Command: &command, Target: seed.targetA, System: snippet.OSDefault, Inputs: []string{"signed input"}}
			},
			func(change *protocol.WorkspaceChange) { change.Inputs = []string{"changed input"} },
		},
		{
			"kind",
			func() *protocol.WorkspaceChange {
				command := seed.commandA.Clone()
				return &protocol.WorkspaceChange{Kind: "snippet-save", Command: &command}
			},
			func(change *protocol.WorkspaceChange) { change.Kind = "snippet-delete" },
		},
		{
			"parameter",
			func() *protocol.WorkspaceChange {
				command := seed.commandA.Clone()
				return &protocol.WorkspaceChange{Kind: "parameter-history-clear", Command: &command, Target: seed.targetA, System: snippet.OSDefault, Parameter: 0}
			},
			func(change *protocol.WorkspaceChange) { change.Parameter = 1 },
		},
	}
	for _, test := range tamperTests {
		t.Run(test.name, func(t *testing.T) {
			change := test.change()
			query := signedWorkspaceWrite(t, cli, change)
			test.mutate(query.WorkspaceChange)
			if _, err := cli.Call(ctx, query); err == nil {
				t.Fatal("write accepted after its signed payload changed")
			}
		})
	}
	requireSeedWorkspaceUnchanged(t, server, seed, profile)

	replay := signedWorkspaceWrite(t, cli, &protocol.WorkspaceChange{
		Kind:    "snippet-save",
		Command: &snippet.Command{ID: "replay-command", Name: "Replay", Body: "echo replay"},
	})
	if _, err := cli.Call(ctx, replay); err != nil {
		t.Fatal(err)
	}
	if _, err := cli.Call(ctx, replay); err == nil {
		t.Fatal("replayed workspace write accepted")
	}
}

func TestWorkspaceWriteSDKPersistsChangesAndScopes(t *testing.T) {
	cli, server, profile := newWorkspaceHarness(t)
	seed := seedWorkspaceReads(t, cli, server, profile)
	ctx := context.Background()
	targetC := strings.Repeat("c", 64)

	saved := snippet.Command{ID: "sdk-save", Name: "SDK save", Body: "printf '%s' $1", Args: []snippet.Arg{{Name: "value"}}}
	if err := cli.SaveSnippet(ctx, saved, false, cli.Owner); err != nil {
		t.Fatal(err)
	}
	if got := server.Vault.ParameterHistory(saved.ID, targetC, snippet.OSDefault, 0); len(got) != 0 {
		t.Fatalf("new snippet carried history: %#v", got)
	}
	if err := cli.SaveParameterHistory(ctx, saved, targetC, snippet.OSDefault, []string{"remembered"}, -1, cli.Owner); err != nil {
		t.Fatal(err)
	}
	saved.Name = "SDK save renamed"
	if err := cli.SaveSnippet(ctx, saved, false, cli.Owner); err != nil {
		t.Fatal(err)
	}
	requireWorkspaceHistory(t, server.Vault, saved.ID, targetC, snippet.OSDefault, 0, "remembered")

	injectedTarget := strings.Repeat("d", 64)
	injected := snippet.Command{
		ID:   "injected-history",
		Name: "Injected history",
		Body: "echo $1",
		Args: []snippet.Arg{{Name: "value"}},
		History: []snippet.InputHistory{{
			Parameter: 0, Target: injectedTarget, System: snippet.OSDefault,
			Definition: "forged", Values: []string{"forged-history-value"},
		}},
	}
	if _, err := cli.Call(ctx, signedWorkspaceWrite(t, cli, &protocol.WorkspaceChange{Kind: "snippet-save", Command: &injected})); err != nil {
		t.Fatal(err)
	}
	if got := server.Vault.ParameterHistory(injected.ID, injectedTarget, snippet.OSDefault, 0); len(got) != 0 {
		t.Fatalf("server accepted injected command history: %#v", got)
	}

	withHistory := saved.Clone()
	withHistory.History = []snippet.InputHistory{{
		Parameter: 0, Target: injectedTarget, System: snippet.OSDefault,
		Definition: "forged-history-snapshot", Values: []string{"forged-history-snapshot-value"},
	}}
	if _, err := cli.Call(ctx, signedWorkspaceWrite(t, cli, &protocol.WorkspaceChange{
		Kind: "parameter-history-save", Command: &withHistory, Target: injectedTarget,
		System: snippet.OSDefault, Inputs: []string{"real-history-value"},
	})); err != nil {
		t.Fatal(err)
	}
	requireWorkspaceHistory(t, server.Vault, saved.ID, injectedTarget, snippet.OSDefault, 0, "real-history-value")

	if err := cli.SaveServerNote(ctx, profile.ID, vault.ServerNote{Manual: "updated note"}, cli.Owner); err != nil {
		t.Fatal(err)
	}
	note, err := server.Vault.ServerNote(profile.ID)
	if err != nil || note.Manual != "updated note" {
		t.Fatalf("updated note = %#v, %v", note, err)
	}
	if err := cli.SaveServerNote(ctx, profile.ID, vault.ServerNote{}, cli.Owner); err != nil {
		t.Fatal(err)
	}
	note, err = server.Vault.ServerNote(profile.ID)
	if err != nil || note.Manual != "" {
		t.Fatalf("cleared note = %#v, %v", note, err)
	}

	if err = cli.SaveParameterHistory(ctx, seed.commandA, seed.targetA, snippet.OSDefault, []string{"new-alpha", "new-one"}, -1, cli.Owner); err != nil {
		t.Fatal(err)
	}
	if err = cli.SaveParameterHistory(ctx, seed.commandA, seed.targetA, snippet.OSDefault, []string{"new-alpha", "new-one"}, -1, cli.Owner); err != nil {
		t.Fatal(err)
	}
	requireWorkspaceHistory(t, server.Vault, seed.commandA.ID, seed.targetA, snippet.OSDefault, 0, "new-alpha", "alpha")
	requireWorkspaceHistory(t, server.Vault, seed.commandA.ID, seed.targetA, snippet.OSDefault, 1, "new-one", "one")
	if err = cli.SaveParameterHistory(ctx, seed.commandA, seed.targetA, snippet.OSDefault, nil, 0, cli.Owner); err != nil {
		t.Fatal(err)
	}
	requireWorkspaceHistory(t, server.Vault, seed.commandA.ID, seed.targetA, snippet.OSDefault, 0)
	requireWorkspaceHistory(t, server.Vault, seed.commandA.ID, seed.targetA, snippet.OSDefault, 1, "new-one", "one")
	requireWorkspaceHistory(t, server.Vault, seed.commandA.ID, seed.targetB, snippet.OSDebian, 0, "bravo")
	requireWorkspaceHistory(t, server.Vault, seed.commandB.ID, seed.targetA, snippet.OSDefault, 0, "charlie")

	noHistory := snippet.Command{
		ID: "no-history", Name: "No history", Body: "echo $1 $2",
		Args: []snippet.Arg{{Name: "visible"}, {Name: "private", NoHistory: true}},
	}
	if err = cli.SaveSnippet(ctx, noHistory, false, cli.Owner); err != nil {
		t.Fatal(err)
	}
	noHistoryTarget := strings.Repeat("e", 64)
	if err = cli.SaveParameterHistory(ctx, noHistory, noHistoryTarget, snippet.OSDefault, []string{"visible-value", "private-value"}, -1, cli.Owner); err != nil {
		t.Fatal(err)
	}
	requireWorkspaceHistory(t, server.Vault, noHistory.ID, noHistoryTarget, snippet.OSDefault, 0, "visible-value")
	requireWorkspaceHistory(t, server.Vault, noHistory.ID, noHistoryTarget, snippet.OSDefault, 1)

	stale := snippet.Command{ID: "stale-command", Name: "Stale", Body: "echo $1", Args: []snippet.Arg{{Name: "value"}}}
	if err = cli.SaveSnippet(ctx, stale, false, cli.Owner); err != nil {
		t.Fatal(err)
	}
	staleTarget := strings.Repeat("f", 64)
	if err = cli.SaveParameterHistory(ctx, stale, staleTarget, snippet.OSDefault, []string{"old-value"}, -1, cli.Owner); err != nil {
		t.Fatal(err)
	}
	edited := stale.Clone()
	edited.Body = "echo changed $1"
	if err = cli.SaveSnippet(ctx, edited, false, cli.Owner); err != nil {
		t.Fatal(err)
	}
	if err = cli.SaveParameterHistory(ctx, stale, staleTarget, snippet.OSDefault, []string{"stale-secret"}, -1, cli.Owner); err == nil {
		t.Fatal("stale command snapshot saved parameter history")
	}

	deleteMe := snippet.Command{ID: "delete-me", Name: "Delete me", Body: "echo delete"}
	if err = cli.SaveSnippet(ctx, deleteMe, false, cli.Owner); err != nil {
		t.Fatal(err)
	}
	if err = cli.SaveSnippet(ctx, deleteMe, true, cli.Owner); err != nil {
		t.Fatal(err)
	}

	reopened, err := vault.Read(broker.VaultPath(cli.Dir), pass)
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	commands := reopened.Snippets()
	byID := make(map[string]snippet.Command, len(commands))
	for _, command := range commands {
		byID[command.ID] = command
	}
	if command, ok := byID[saved.ID]; !ok || command.Name != saved.Name || command.Body != saved.Body || command.History != nil {
		t.Fatalf("saved command did not persist correctly: %#v", command)
	}
	if command, ok := byID[injected.ID]; !ok || command.History != nil {
		t.Fatalf("injected command did not persist safely: %#v", command)
	}
	if _, ok := byID[deleteMe.ID]; ok {
		t.Fatal("deleted command returned after reopening the vault")
	}
	requireWorkspaceHistory(t, reopened, saved.ID, targetC, snippet.OSDefault, 0, "remembered")
	requireWorkspaceHistory(t, reopened, saved.ID, injectedTarget, snippet.OSDefault, 0, "real-history-value")
	requireWorkspaceHistory(t, reopened, seed.commandA.ID, seed.targetA, snippet.OSDefault, 0)
	requireWorkspaceHistory(t, reopened, seed.commandA.ID, seed.targetA, snippet.OSDefault, 1, "new-one", "one")
	requireWorkspaceHistory(t, reopened, seed.commandA.ID, seed.targetB, snippet.OSDebian, 0, "bravo")
	requireWorkspaceHistory(t, reopened, seed.commandB.ID, seed.targetA, snippet.OSDefault, 0, "charlie")
	requireWorkspaceHistory(t, reopened, noHistory.ID, noHistoryTarget, snippet.OSDefault, 0, "visible-value")
	requireWorkspaceHistory(t, reopened, noHistory.ID, noHistoryTarget, snippet.OSDefault, 1)
	reopenedNote, err := reopened.ServerNote(profile.ID)
	if err != nil || reopenedNote.Manual != "" {
		t.Fatalf("cleared note did not persist: %#v, %v", reopenedNote, err)
	}
	secondNote, err := reopened.ServerNote(seed.secondProfile.ID)
	if err != nil || secondNote.Manual != "second note" {
		t.Fatalf("other note did not persist: %#v, %v", secondNote, err)
	}
}

func TestWorkspaceWriteRejectsInvalidChanges(t *testing.T) {
	cli, server, profile := newWorkspaceHarness(t)
	seed := seedWorkspaceReads(t, cli, server, profile)
	ctx := context.Background()
	target := seed.targetA
	longID := strings.Repeat("x", 129)
	commandA := seed.commandA.Clone()
	commandB := seed.commandB.Clone()
	tooManyInputs := make([]string, snippet.MaxArgs+1)
	for i := range tooManyInputs {
		tooManyInputs[i] = "x"
	}

	tests := []struct {
		name   string
		change *protocol.WorkspaceChange
	}{
		{"nil change", nil},
		{"unknown kind", &protocol.WorkspaceChange{Kind: "unknown"}},
		{"snippet missing command", &protocol.WorkspaceChange{Kind: "snippet-save"}},
		{"snippet long ID", &protocol.WorkspaceChange{Kind: "snippet-save", Command: &snippet.Command{ID: longID, Name: "Long", Body: "echo long"}}},
		{"snippet invalid command", &protocol.WorkspaceChange{Kind: "snippet-save", Command: &snippet.Command{ID: "invalid-command", Name: "Invalid"}}},
		{"delete missing ID", &protocol.WorkspaceChange{Kind: "snippet-delete"}},
		{"delete long ID", &protocol.WorkspaceChange{Kind: "snippet-delete", CommandID: longID}},
		{"note missing host", &protocol.WorkspaceChange{Kind: "server-note-save", Note: &vault.ServerNote{Manual: "note"}}},
		{"note long host", &protocol.WorkspaceChange{Kind: "server-note-save", HostID: longID, Note: &vault.ServerNote{Manual: "note"}}},
		{"note missing payload", &protocol.WorkspaceChange{Kind: "server-note-save", HostID: profile.ID}},
		{"note unknown host", &protocol.WorkspaceChange{Kind: "server-note-save", HostID: "missing-host", Note: &vault.ServerNote{Manual: "note"}}},
		{"history missing command", &protocol.WorkspaceChange{Kind: "parameter-history-save", Target: target, System: snippet.OSDefault}},
		{"history long command ID", &protocol.WorkspaceChange{Kind: "parameter-history-save", Command: &snippet.Command{ID: longID, Body: "echo"}, Target: target, System: snippet.OSDefault}},
		{"history short target", &protocol.WorkspaceChange{Kind: "parameter-history-save", Command: &commandA, Target: "abc", System: snippet.OSDefault}},
		{"history uppercase target", &protocol.WorkspaceChange{Kind: "parameter-history-save", Command: &commandA, Target: strings.Repeat("A", 64), System: snippet.OSDefault}},
		{"history unsupported system", &protocol.WorkspaceChange{Kind: "parameter-history-save", Command: &commandA, Target: target, System: "plan9"}},
		{"history unsupported command system", &protocol.WorkspaceChange{Kind: "parameter-history-save", Command: &commandB, Target: target, System: snippet.OSDebian}},
		{"history too many inputs", &protocol.WorkspaceChange{Kind: "parameter-history-save", Command: &commandA, Target: target, System: snippet.OSDefault, Inputs: tooManyInputs}},
		{"history oversized input", &protocol.WorkspaceChange{Kind: "parameter-history-save", Command: &commandA, Target: target, System: snippet.OSDefault, Inputs: []string{strings.Repeat("x", 513)}}},
		{"clear missing command", &protocol.WorkspaceChange{Kind: "parameter-history-clear", Target: target, System: snippet.OSDefault, Parameter: 0}},
		{"clear negative parameter", &protocol.WorkspaceChange{Kind: "parameter-history-clear", Command: &commandA, Target: target, System: snippet.OSDefault, Parameter: -1}},
		{"clear parameter too large", &protocol.WorkspaceChange{Kind: "parameter-history-clear", Command: &commandA, Target: target, System: snippet.OSDefault, Parameter: snippet.MaxArgs}},
		{"clear parameter outside snapshot", &protocol.WorkspaceChange{Kind: "parameter-history-clear", Command: &commandB, Target: target, System: snippet.OSDefault, Parameter: 1}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			query := signedWorkspaceWrite(t, cli, test.change)
			if _, err := cli.Call(ctx, query); err == nil {
				t.Fatal("invalid workspace write accepted")
			}
		})
	}
	if err := cli.SaveParameterHistory(ctx, seed.commandA, target, snippet.OSDefault, nil, -2, cli.Owner); err == nil {
		t.Fatal("SDK accepted a clear parameter below -1")
	}
	requireSeedWorkspaceUnchanged(t, server, seed, profile)
}

func TestWorkspaceWriteAuditKeepsOnlyFixedMetadata(t *testing.T) {
	cli, server, profile := newWorkspaceHarness(t)
	seed := seedWorkspaceReads(t, cli, server, profile)
	ctx := context.Background()
	bodySecret := "synthetic-body-secret-b02b2"
	noteSecret := "synthetic-note-secret-b02b2"
	inputSecret := "synthetic-input-secret-b02b2"
	staleSecret := "synthetic-stale-secret-b02b2"

	command := snippet.Command{ID: "audit-command", Name: "Audit command", Body: "printf '%s' $1", Args: []snippet.Arg{{Name: "value"}}}
	if err := cli.SaveSnippet(ctx, command, false, cli.Owner); err != nil {
		t.Fatal(err)
	}
	command.Body = "printf '%s' $1 # " + bodySecret
	if err := cli.SaveSnippet(ctx, command, false, cli.Owner); err != nil {
		t.Fatal(err)
	}
	if err := cli.SaveServerNote(ctx, profile.ID, vault.ServerNote{Manual: noteSecret}, cli.Owner); err != nil {
		t.Fatal(err)
	}
	if err := cli.SaveParameterHistory(ctx, command, seed.targetA, snippet.OSDefault, []string{inputSecret}, -1, cli.Owner); err != nil {
		t.Fatal(err)
	}

	edited := seed.commandB.Clone()
	edited.Body = "echo changed $1"
	if err := cli.SaveSnippet(ctx, edited, false, cli.Owner); err != nil {
		t.Fatal(err)
	}
	if err := cli.SaveParameterHistory(ctx, seed.commandB, seed.targetA, snippet.OSDefault, []string{staleSecret}, -1, cli.Owner); err == nil {
		t.Fatal("stale snapshot unexpectedly succeeded")
	}

	path := filepath.Join(cli.Dir, "audit.jsonl")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	for _, secret := range []string{bodySecret, noteSecret, inputSecret, staleSecret} {
		if bytes.Contains(data, []byte(secret)) {
			t.Fatalf("audit log contains private workspace content %q", secret)
		}
	}

	started, successEnded, failedEnded := 0, 0, 0
	for _, line := range bytes.Split(data, []byte{'\n'}) {
		if len(line) == 0 {
			continue
		}
		var event auditlog.Event
		if err := json.Unmarshal(line, &event); err != nil {
			t.Fatalf("decode audit event: %v", err)
		}
		switch event.Event {
		case "workspace_write":
			started++
			requireWorkspaceAuditMetadata(t, event, profile.ID)
		case "ended":
			requireWorkspaceAuditMetadata(t, event, profile.ID)
			switch event.Outcome {
			case "success":
				successEnded++
			case "failed":
				failedEnded++
			}
		}
	}
	if started < 5 || successEnded < 4 || failedEnded < 1 {
		t.Fatalf("audit counts started=%d success=%d failed=%d", started, successEnded, failedEnded)
	}
}

func requireWorkspaceAuditMetadata(t *testing.T, event auditlog.Event, noteHostID string) {
	t.Helper()
	if !event.OwnerIssued || event.Client != "所有者" {
		t.Fatalf("workspace audit lost owner metadata: %+v", event)
	}
	switch event.Kind {
	case "snippet-save", "snippet-delete", "server-note-save", "parameter-history-save", "parameter-history-clear":
	default:
		t.Fatalf("workspace audit has unexpected kind %q", event.Kind)
	}
	if event.Kind == "server-note-save" {
		if event.Host != noteHostID {
			t.Fatalf("note audit host = %q, want %q", event.Host, noteHostID)
		}
	} else if event.Host != "" {
		t.Fatalf("non-note workspace audit carried host %q", event.Host)
	}
	if event.Command != "" || event.Path != "" || event.Destination != "" || event.Object != "" {
		t.Fatalf("workspace audit carried operation content: %+v", event)
	}
}
