package protocol

import (
	"encoding/json"
	"sshclient/internal/approval"
	"sshclient/internal/auditlog"
	"sshclient/internal/connectcfg"
	"sshclient/internal/vault"
)

func ProfileProof(challenge string, p vault.Profile, prove func([]byte) ([]byte, error)) ([]byte, error) {
	b, e := json.Marshal(p)
	if e != nil {
		return nil, e
	}
	return prove(append([]byte("ssh-client/profile-put/v1\x00"+challenge+"\x00"), b...))
}

func ProfileChangeProof(challenge string, p vault.ProfileChange, prove func([]byte) ([]byte, error)) ([]byte, error) {
	b, e := json.Marshal(p)
	if e != nil {
		return nil, e
	}
	return prove(append([]byte("ssh-client/profile-change/v1\x00"+challenge+"\x00"), b...))
}

func OwnerRequestProof(challenge string, op approval.Operation, prove func([]byte) ([]byte, error)) ([]byte, error) {
	b, e := json.Marshal(op)
	if e != nil {
		return nil, e
	}
	return prove(append([]byte("ssh-client/owner-request/v1\x00"+challenge+"\x00"), b...))
}

func OwnerQueryAction(action string) bool {
	switch action {
	case "hosts", "pending", "status", "sessionStatus", "probeHost", "agentIdentities", "measureRoutes", "workspaceRead", "workspaceWrite":
		return true
	}
	return false
}

func OwnerQueryProof(challenge string, q Message, prove func([]byte) ([]byte, error)) ([]byte, error) {
	b, err := json.Marshal(struct {
		Action, ID, Target, After string
		Port, Limit               int
		Network                   *connectcfg.Network
		KeepProxyCredential       bool
		ExpectedFingerprint       string
		Workspace                 *WorkspaceQuery            `json:",omitempty"`
		WorkspaceChange           *WorkspaceChange           `json:",omitempty"`
		ConnectionRoute           *connectcfg.RouteSelection `json:",omitempty"`
	}{q.Action, q.ID, q.Target, q.After, q.Port, q.Limit, q.Network, q.KeepProxyCredential, q.ExpectedFingerprint, q.Workspace, q.WorkspaceChange, q.ConnectionRoute})
	if err != nil {
		return nil, err
	}
	return prove(append([]byte("ssh-client/owner-query/v1\x00"+challenge+"\x00"), b...))
}

func AuditHistoryProof(challenge string, q auditlog.Query, prove func([]byte) ([]byte, error)) ([]byte, error) {
	b, err := json.Marshal(q)
	if err != nil {
		return nil, err
	}
	return prove(append([]byte("ssh-client/audit-history/v1\x00"+challenge+"\x00"), b...))
}
