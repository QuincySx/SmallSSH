package protocol

import (
	"sshclient/internal/snippet"
	"sshclient/internal/vault"
)

// WorkspaceQuery describes one owner-only read from the workbench. It carries
// no authority by itself; the broker binds it to a one-use owner challenge.
type WorkspaceQuery struct {
	Kind      string `json:"kind,omitempty"`
	HostID    string `json:"hostID,omitempty"`
	CommandID string `json:"commandID,omitempty"`
	Target    string `json:"target,omitempty"`
	System    string `json:"system,omitempty"`
	Parameter int    `json:"parameter,omitempty"`
	After     string `json:"after,omitempty"`
	Limit     int    `json:"limit,omitempty"`
}

// WorkspaceChange describes one owner-only write to the workbench. It carries
// no authority by itself; the broker binds every field to a one-use owner
// challenge before touching the vault.
type WorkspaceChange struct {
	Kind      string            `json:"kind,omitempty"`
	HostID    string            `json:"hostID,omitempty"`
	CommandID string            `json:"commandID,omitempty"`
	Command   *snippet.Command  `json:"command,omitempty"`
	Note      *vault.ServerNote `json:"note,omitempty"`
	Target    string            `json:"target,omitempty"`
	System    string            `json:"system,omitempty"`
	Inputs    []string          `json:"inputs,omitempty"`
	Parameter int               `json:"parameter,omitempty"`
}

// WorkspaceResult returns only the data selected by a WorkspaceQuery.
type WorkspaceResult struct {
	Commands []snippet.Command `json:"commands,omitempty"`
	Note     *vault.ServerNote `json:"note,omitempty"`
	History  []string          `json:"history,omitempty"`
	Next     string            `json:"next,omitempty"`
}
