package broker_test

import (
	"bytes"
	"testing"

	"sshclient/internal/broker"
	"sshclient/internal/protocol"
	"sshclient/internal/snippet"
	"sshclient/internal/vault"
)

const ownerQueryChallenge = "synthetic-challenge"

func captureOwnerQueryProof(t *testing.T, q broker.Message) []byte {
	t.Helper()
	var signed []byte
	got, err := broker.OwnerQueryProof(ownerQueryChallenge, q, func(payload []byte) ([]byte, error) {
		signed = append([]byte(nil), payload...)
		return payload, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, signed) {
		t.Fatal("OwnerQueryProof returned different bytes from the callback")
	}
	return signed
}

func TestOwnerQueryProofWithoutWorkspaceKeepsLegacyBytes(t *testing.T) {
	const want = "ssh-client/owner-query/v1\x00synthetic-challenge\x00" +
		`{"Action":"hosts","ID":"","Target":"","After":"","Port":0,"Limit":0,"Network":null,"KeepProxyCredential":false,"ExpectedFingerprint":""}`

	got := captureOwnerQueryProof(t, broker.Message{Action: "hosts"})
	if !bytes.Equal(got, []byte(want)) {
		t.Fatalf("owner-query bytes =\n%q\nwant\n%q", got, want)
	}
}

func TestOwnerQueryProofCoversEveryWorkspaceField(t *testing.T) {
	q := broker.Message{
		Action: "hosts",
		Workspace: &protocol.WorkspaceQuery{
			Kind:      "snippet-list",
			HostID:    "host-a",
			CommandID: "command-a",
			Target:    "target-a",
			System:    "linux",
			Parameter: 7,
			After:     "after-a",
			Limit:     13,
		},
	}
	baseline := captureOwnerQueryProof(t, q)

	tests := []struct {
		name   string
		change func(*protocol.WorkspaceQuery)
	}{
		{"Kind", func(workspace *protocol.WorkspaceQuery) { workspace.Kind = "server-note" }},
		{"HostID", func(workspace *protocol.WorkspaceQuery) { workspace.HostID = "host-b" }},
		{"CommandID", func(workspace *protocol.WorkspaceQuery) { workspace.CommandID = "command-b" }},
		{"Target", func(workspace *protocol.WorkspaceQuery) { workspace.Target = "target-b" }},
		{"System", func(workspace *protocol.WorkspaceQuery) { workspace.System = "darwin" }},
		{"Parameter", func(workspace *protocol.WorkspaceQuery) { workspace.Parameter = 8 }},
		{"After", func(workspace *protocol.WorkspaceQuery) { workspace.After = "after-b" }},
		{"Limit", func(workspace *protocol.WorkspaceQuery) { workspace.Limit = 14 }},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			workspace := *q.Workspace
			tt.change(&workspace)
			changed := q
			changed.Workspace = &workspace
			if got := captureOwnerQueryProof(t, changed); bytes.Equal(got, baseline) {
				t.Fatalf("changing Workspace.%s did not change the signed bytes", tt.name)
			}
		})
	}
}

func TestOwnerQueryProofCoversEveryWorkspaceChangeField(t *testing.T) {
	q := broker.Message{
		Action: "workspaceWrite",
		WorkspaceChange: &protocol.WorkspaceChange{
			Kind:      "parameter-history-save",
			HostID:    "host-a",
			CommandID: "command-a",
			Command:   &snippet.Command{ID: "command-a", Name: "Command A", Body: "echo $1"},
			Note:      &vault.ServerNote{Manual: "note-a"},
			Target:    "target-a",
			System:    "linux",
			Inputs:    []string{"input-a"},
			Parameter: 7,
		},
	}
	baseline := captureOwnerQueryProof(t, q)

	tests := []struct {
		name   string
		change func(*protocol.WorkspaceChange)
	}{
		{"Kind", func(change *protocol.WorkspaceChange) { change.Kind = "parameter-history-clear" }},
		{"HostID", func(change *protocol.WorkspaceChange) { change.HostID = "host-b" }},
		{"CommandID", func(change *protocol.WorkspaceChange) { change.CommandID = "command-b" }},
		{"Command", func(change *protocol.WorkspaceChange) {
			change.Command = &snippet.Command{ID: "command-a", Name: "Command A", Body: "echo changed"}
		}},
		{"Note", func(change *protocol.WorkspaceChange) { change.Note = &vault.ServerNote{Manual: "note-b"} }},
		{"Target", func(change *protocol.WorkspaceChange) { change.Target = "target-b" }},
		{"System", func(change *protocol.WorkspaceChange) { change.System = "darwin" }},
		{"Inputs", func(change *protocol.WorkspaceChange) { change.Inputs = []string{"input-b"} }},
		{"Parameter", func(change *protocol.WorkspaceChange) { change.Parameter = 8 }},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			change := *q.WorkspaceChange
			tt.change(&change)
			changed := q
			changed.WorkspaceChange = &change
			if got := captureOwnerQueryProof(t, changed); bytes.Equal(got, baseline) {
				t.Fatalf("changing WorkspaceChange.%s did not change the signed bytes", tt.name)
			}
		})
	}
}
