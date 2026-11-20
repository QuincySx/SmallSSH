package broker_test

import (
	"context"
	"fmt"
	"os"
	"slices"
	"strings"
	"testing"

	"sshclient/internal/broker"
	"sshclient/internal/client"
	"sshclient/internal/localipc"
	"sshclient/internal/protocol"
	"sshclient/internal/snippet"
	"sshclient/internal/testserver"
	"sshclient/internal/vault"
)

type workspaceSeed struct {
	secondProfile vault.Profile
	commandA      snippet.Command
	commandB      snippet.Command
	targetA       string
	targetB       string
}

func newWorkspaceHarness(t *testing.T) (client.Client, *broker.Server, vault.Profile) {
	t.Helper()
	fixture := testserver.New(t)
	dir, err := os.MkdirTemp("", "ssh-workspace-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(dir) })
	if err = os.Chmod(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err = localipc.Prepare(dir); err != nil {
		t.Fatal(err)
	}
	owner, err := vault.New(pass)
	if err != nil {
		t.Fatal(err)
	}
	profile := fixture.Profile()
	if err = owner.Put(profile); err != nil {
		owner.Close()
		t.Fatal(err)
	}
	if err = vault.Save(broker.VaultPath(dir), owner, true); err != nil {
		owner.Close()
		t.Fatal(err)
	}
	owner.Close()
	server, err := broker.Open(dir, pass)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(server.Close)
	return client.Client{Dir: dir, Owner: server.Vault}, server, profile
}

func seedWorkspaceReads(t *testing.T, cli client.Client, server *broker.Server, profile vault.Profile) workspaceSeed {
	t.Helper()
	second := profile
	second.ID = "fixture-two"
	second.Name = "Second workspace fixture"
	second.Host = "192.0.2.10"
	if err := server.Vault.SaveProfile(broker.VaultPath(cli.Dir), second); err != nil {
		t.Fatal(err)
	}
	file := broker.VaultPath(cli.Dir)
	if err := server.Vault.SaveServerNote(file, profile.ID, vault.ServerNote{Manual: "first note"}); err != nil {
		t.Fatal(err)
	}
	if err := server.Vault.SaveServerNote(file, second.ID, vault.ServerNote{Manual: "second note"}); err != nil {
		t.Fatal(err)
	}

	commandA := snippet.Command{
		ID:       "cmd-a",
		Name:     "First command",
		Body:     "echo $1 $2",
		Variants: []snippet.Variant{{OS: snippet.OSDebian, Body: "printf '%s %s' $1 $2"}},
		Args:     []snippet.Arg{{Name: "first"}, {Name: "second"}},
	}
	commandB := snippet.Command{ID: "cmd-b", Name: "Second command", Body: "echo $1", Args: []snippet.Arg{{Name: "only"}}}
	if err := server.Vault.SaveSnippet(file, commandB, false); err != nil {
		t.Fatal(err)
	}
	if err := server.Vault.SaveSnippet(file, commandA, false); err != nil {
		t.Fatal(err)
	}
	targetA := strings.Repeat("a", 64)
	targetB := strings.Repeat("b", 64)
	if err := server.Vault.SaveParameterHistory(file, commandA, targetA, snippet.OSDefault, []string{"alpha", "one"}, -1); err != nil {
		t.Fatal(err)
	}
	if err := server.Vault.SaveParameterHistory(file, commandA, targetB, snippet.OSDebian, []string{"bravo", "two"}, -1); err != nil {
		t.Fatal(err)
	}
	if err := server.Vault.SaveParameterHistory(file, commandB, targetA, snippet.OSDefault, []string{"charlie"}, -1); err != nil {
		t.Fatal(err)
	}
	return workspaceSeed{secondProfile: second, commandA: commandA, commandB: commandB, targetA: targetA, targetB: targetB}
}

func signedWorkspaceQuery(t *testing.T, cli client.Client, owner client.OwnerProof, query broker.Message) broker.Message {
	t.Helper()
	challenge, err := cli.Call(context.Background(), broker.Message{Action: "profileChallenge"})
	if err != nil {
		t.Fatal(err)
	}
	query.Challenge = challenge.ID
	query.Proof, err = broker.OwnerQueryProof(challenge.ID, query, owner.Proof)
	if err != nil {
		t.Fatal(err)
	}
	return query
}

func TestWorkspaceReadsRequireOwner(t *testing.T) {
	cli, _, profile := newWorkspaceHarness(t)
	untrusted := client.Client{Dir: cli.Dir}
	queries := []*protocol.WorkspaceQuery{
		{Kind: "snippet-list"},
		{Kind: "server-note", HostID: profile.ID},
		{Kind: "parameter-history", CommandID: "cmd-a", Target: strings.Repeat("a", 64), System: snippet.OSDefault},
	}
	for _, workspace := range queries {
		if _, err := untrusted.Call(context.Background(), broker.Message{Action: "workspaceRead", Workspace: workspace}); err == nil {
			t.Fatalf("anonymous %s read succeeded", workspace.Kind)
		}
	}
}

func TestWorkspaceOwnerReadsAndScopes(t *testing.T) {
	cli, server, profile := newWorkspaceHarness(t)
	seed := seedWorkspaceReads(t, cli, server, profile)
	ctx := context.Background()

	raw, err := cli.Call(ctx, signedWorkspaceQuery(t, cli, cli.Owner, broker.Message{
		Action:    "workspaceRead",
		Workspace: &protocol.WorkspaceQuery{Kind: "snippet-list"},
	}))
	if err != nil || raw.WorkspaceResult == nil || len(raw.WorkspaceResult.Commands) != 2 {
		t.Fatalf("raw command list = %#v, %v", raw, err)
	}
	for _, command := range raw.WorkspaceResult.Commands {
		if command.History != nil {
			t.Fatalf("raw command list leaked history for %s", command.ID)
		}
	}

	commands, err := cli.ListSnippets(ctx, cli.Owner)
	if err != nil {
		t.Fatal(err)
	}
	if len(commands) != 2 || commands[0].ID != seed.commandA.ID || commands[1].ID != seed.commandB.ID {
		t.Fatalf("commands = %#v", commands)
	}
	for _, command := range commands {
		if command.History != nil {
			t.Fatalf("command list leaked history for %s", command.ID)
		}
	}

	firstNote, err := cli.ServerNote(ctx, profile.ID, cli.Owner)
	if err != nil || firstNote.Manual != "first note" {
		t.Fatalf("first note = %#v, %v", firstNote, err)
	}
	secondNote, err := cli.ServerNote(ctx, seed.secondProfile.ID, cli.Owner)
	if err != nil || secondNote.Manual != "second note" {
		t.Fatalf("second note = %#v, %v", secondNote, err)
	}

	history, err := cli.ParameterHistory(ctx, seed.commandA.ID, seed.targetA, snippet.OSDefault, 0, cli.Owner)
	if err != nil || !slices.Equal(history, []string{"alpha"}) {
		t.Fatalf("history = %#v, %v", history, err)
	}
	secondParam, err := cli.ParameterHistory(ctx, seed.commandA.ID, seed.targetA, snippet.OSDefault, 1, cli.Owner)
	if err != nil || !slices.Equal(secondParam, []string{"one"}) {
		t.Fatalf("second parameter history = %#v, %v", secondParam, err)
	}
	if crossTarget, err := cli.ParameterHistory(ctx, seed.commandA.ID, seed.targetB, snippet.OSDefault, 0, cli.Owner); err != nil || len(crossTarget) != 0 {
		t.Fatalf("target history leaked: %#v, %v", crossTarget, err)
	}
	if crossSystem, err := cli.ParameterHistory(ctx, seed.commandA.ID, seed.targetA, snippet.OSDebian, 0, cli.Owner); err != nil || len(crossSystem) != 0 {
		t.Fatalf("system history leaked: %#v, %v", crossSystem, err)
	}
	if otherCommand, err := cli.ParameterHistory(ctx, seed.commandB.ID, seed.targetA, snippet.OSDefault, 0, cli.Owner); err != nil || !slices.Equal(otherCommand, []string{"charlie"}) {
		t.Fatalf("other command history = %#v, %v", otherCommand, err)
	}

	cancelled, cancel := context.WithCancel(ctx)
	cancel()
	if _, err = cli.ListSnippets(cancelled, cli.Owner); err == nil {
		t.Fatal("cancelled command list continued")
	}
}

func TestWorkspaceProofBindingAndReplay(t *testing.T) {
	cli, server, profile := newWorkspaceHarness(t)
	seed := seedWorkspaceReads(t, cli, server, profile)
	ctx := context.Background()
	base := broker.Message{
		Action:    "workspaceRead",
		Workspace: &protocol.WorkspaceQuery{Kind: "server-note", HostID: profile.ID},
	}
	challenge, err := cli.Call(ctx, broker.Message{Action: "profileChallenge"})
	if err != nil {
		t.Fatal(err)
	}
	wrongProof := base
	wrongProof.Challenge = challenge.ID
	wrongProof.Proof = []byte("forged workspace proof")
	if _, err = cli.Call(ctx, wrongProof); err == nil {
		t.Fatal("forged workspace proof accepted")
	}

	signed := signedWorkspaceQuery(t, cli, cli.Owner, base)
	changedKind := signed
	workspace := *signed.Workspace
	workspace.Kind = "snippet-list"
	changedKind.Workspace = &workspace
	if _, err := cli.Call(ctx, changedKind); err == nil {
		t.Fatal("changed kind accepted with proof for another kind")
	}

	signed = signedWorkspaceQuery(t, cli, cli.Owner, base)
	changedHost := signed
	workspace = *signed.Workspace
	workspace.HostID = seed.secondProfile.ID
	changedHost.Workspace = &workspace
	if _, err := cli.Call(ctx, changedHost); err == nil {
		t.Fatal("changed host accepted with proof for another host")
	}

	signed = signedWorkspaceQuery(t, cli, cli.Owner, broker.Message{
		Action:    "workspaceRead",
		Workspace: &protocol.WorkspaceQuery{Kind: "snippet-list", Limit: 1},
	})
	changedAfter := signed
	workspace = *signed.Workspace
	workspace.After = seed.commandA.ID
	changedAfter.Workspace = &workspace
	if _, err := cli.Call(ctx, changedAfter); err == nil {
		t.Fatal("changed cursor accepted with proof for another cursor")
	}

	signed = signedWorkspaceQuery(t, cli, cli.Owner, base)
	first, err := cli.Call(ctx, signed)
	if err != nil || first.WorkspaceResult == nil || first.WorkspaceResult.Note == nil {
		t.Fatalf("valid signed read failed: %#v, %v", first, err)
	}
	if _, err = cli.Call(ctx, signed); err == nil {
		t.Fatal("workspace challenge replay accepted")
	}
}

func TestWorkspaceSnippetBytePagination(t *testing.T) {
	cli, server, _ := newWorkspaceHarness(t)
	const count = 24
	for i := 0; i < count; i++ {
		command := snippet.Command{
			ID:   fmt.Sprintf("bulk-%02d", i),
			Name: fmt.Sprintf("Bulk command %02d", i),
			Body: fmt.Sprintf("%02d-", i) + strings.Repeat("x", 24<<10),
		}
		if err := server.Vault.SaveSnippet(broker.VaultPath(cli.Dir), command, false); err != nil {
			t.Fatal(err)
		}
	}
	ctx := context.Background()
	first, err := cli.Call(ctx, signedWorkspaceQuery(t, cli, cli.Owner, broker.Message{
		Action:    "workspaceRead",
		Workspace: &protocol.WorkspaceQuery{Kind: "snippet-list", Limit: 32},
	}))
	if err != nil {
		t.Fatal(err)
	}
	if first.WorkspaceResult == nil || len(first.WorkspaceResult.Commands) == 0 || len(first.WorkspaceResult.Commands) >= count || first.WorkspaceResult.Next == "" {
		t.Fatalf("expected a byte-budget page, got %#v", first.WorkspaceResult)
	}
	last := first.WorkspaceResult.Commands[len(first.WorkspaceResult.Commands)-1].ID
	if first.WorkspaceResult.Next != last {
		t.Fatalf("next = %q, want last page ID %q", first.WorkspaceResult.Next, last)
	}
	second, err := cli.Call(ctx, signedWorkspaceQuery(t, cli, cli.Owner, broker.Message{
		Action: "workspaceRead",
		Workspace: &protocol.WorkspaceQuery{
			Kind:  "snippet-list",
			After: first.WorkspaceResult.Next,
			Limit: 2,
		},
	}))
	if err != nil || second.WorkspaceResult == nil || len(second.WorkspaceResult.Commands) == 0 {
		t.Fatalf("next page failed: %#v, %v", second, err)
	}
	if second.WorkspaceResult.Commands[0].ID <= first.WorkspaceResult.Next {
		t.Fatal("exclusive cursor repeated or moved backwards")
	}

	all, err := cli.ListSnippets(ctx, cli.Owner)
	if err != nil {
		t.Fatal(err)
	}
	if len(all) != count {
		t.Fatalf("SDK read %d commands, want %d", len(all), count)
	}
	for i, command := range all {
		if want := fmt.Sprintf("bulk-%02d", i); command.ID != want {
			t.Fatalf("command %d = %q, want %q", i, command.ID, want)
		}
	}
}

func TestWorkspaceRejectsInvalidQueries(t *testing.T) {
	cli, _, _ := newWorkspaceHarness(t)
	target := strings.Repeat("a", 64)
	cases := []struct {
		name      string
		workspace *protocol.WorkspaceQuery
	}{
		{"nil query", nil},
		{"unknown kind", &protocol.WorkspaceQuery{Kind: "unknown"}},
		{"limit too large", &protocol.WorkspaceQuery{Kind: "snippet-list", Limit: 33}},
		{"cursor too long", &protocol.WorkspaceQuery{Kind: "snippet-list", After: strings.Repeat("a", 129)}},
		{"missing host", &protocol.WorkspaceQuery{Kind: "server-note"}},
		{"host too long", &protocol.WorkspaceQuery{Kind: "server-note", HostID: strings.Repeat("h", 129)}},
		{"missing command", &protocol.WorkspaceQuery{Kind: "parameter-history", Target: target, System: snippet.OSDefault}},
		{"command too long", &protocol.WorkspaceQuery{Kind: "parameter-history", CommandID: strings.Repeat("c", 129), Target: target, System: snippet.OSDefault}},
		{"short target", &protocol.WorkspaceQuery{Kind: "parameter-history", CommandID: "cmd-a", Target: "abc", System: snippet.OSDefault}},
		{"uppercase target", &protocol.WorkspaceQuery{Kind: "parameter-history", CommandID: "cmd-a", Target: strings.Repeat("A", 64), System: snippet.OSDefault}},
		{"unsupported system", &protocol.WorkspaceQuery{Kind: "parameter-history", CommandID: "cmd-a", Target: target, System: "plan9"}},
		{"negative parameter", &protocol.WorkspaceQuery{Kind: "parameter-history", CommandID: "cmd-a", Target: target, System: snippet.OSDefault, Parameter: -1}},
		{"parameter too large", &protocol.WorkspaceQuery{Kind: "parameter-history", CommandID: "cmd-a", Target: target, System: snippet.OSDefault, Parameter: snippet.MaxArgs}},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			query := signedWorkspaceQuery(t, cli, cli.Owner, broker.Message{Action: "workspaceRead", Workspace: test.workspace})
			if _, err := cli.Call(context.Background(), query); err == nil {
				t.Fatal("invalid workspace query accepted")
			}
		})
	}
}

func TestHostTrustWriteRequiresExactOwnerIntent(t *testing.T) {
	cli, server, p := newWorkspaceHarness(t)
	fingerprint := p.Fingerprint
	p.Fingerprint = ""
	if err := cli.SaveConnection(context.Background(), vault.ProfileChange{Profile: p}, cli.Owner); err != nil {
		t.Fatal(err)
	}
	trust := vault.HostTrustChange{ID: p.ID, Host: p.Host, Port: p.Port, Fingerprint: fingerprint}
	base := broker.Message{Action: "workspaceWrite", WorkspaceChange: &protocol.WorkspaceChange{Kind: "host-trust", HostTrust: &trust}}
	untrusted := client.Client{Dir: cli.Dir}
	if _, err := untrusted.Call(context.Background(), base); err == nil {
		t.Fatal("unsigned trust write accepted")
	}
	signed := signedWorkspaceQuery(t, cli, cli.Owner, base)
	changed := trust
	changed.Host = "private-target.invalid"
	mutation := *signed.WorkspaceChange
	mutation.HostTrust = &changed
	tampered := signed
	tampered.WorkspaceChange = &mutation
	if _, err := cli.Call(context.Background(), tampered); err == nil {
		t.Fatal("trust proof did not bind target")
	}
	if got, _ := server.Vault.Profile(p.ID); got.Fingerprint != "" {
		t.Fatal("rejected intent changed trust")
	}
	if err := cli.SaveHostTrust(context.Background(), trust, cli.Owner); err != nil {
		t.Fatal(err)
	}
	got, err := server.Vault.Profile(p.ID)
	if err != nil || got.Fingerprint != fingerprint || got.Password != p.Password || got.Network != p.Network {
		t.Fatal("pin did not preserve saved configuration", err)
	}
}
