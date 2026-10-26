package client

import (
	"context"
	"errors"
	"sshclient/internal/auditlog"
	"sshclient/internal/broker"
	"sshclient/internal/vault"
)

// History reads a bounded page with the already-unlocked owner's exact proof.
func (c Client) History(ctx context.Context, q auditlog.Query, owner *vault.Session) (auditlog.Page, error) {
	if owner == nil {
		return auditlog.Page{}, errors.New("保险库已锁定")
	}
	challenge, err := c.Call(ctx, broker.Message{Action: "profileChallenge"})
	if err != nil {
		return auditlog.Page{}, err
	}
	proof, err := broker.AuditHistoryProof(challenge.ID, q, owner.Proof)
	if err != nil {
		return auditlog.Page{}, err
	}
	r, err := c.Call(ctx, broker.Message{Action: "auditHistory", ID: challenge.ID, Proof: proof, AuditQuery: &q})
	if err != nil {
		return auditlog.Page{}, err
	}
	if r.AuditPage == nil {
		return auditlog.Page{}, errors.New("代理未返回操作历史")
	}
	return *r.AuditPage, nil
}
