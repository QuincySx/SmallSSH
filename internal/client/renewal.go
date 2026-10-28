package client

import (
	"context"
	"crypto/ed25519"
	"errors"

	"sshclient/internal/approval"
	"sshclient/internal/broker"
)

func (d *Delegation) renewalScopeMatches(op approval.Operation, requested approval.Operation) bool {
	return op.Kind == "grant" && op.Host == d.Host && op.Grant.PublicKey == d.PublicKey && op.Grant.Permission == d.Permission && op.Grant.ReadRoot == d.ReadRoot && op.Grant.WriteRoot == d.WriteRoot && op.Grant.Validate() == nil &&
		(requested.Endpoint == "" || requested.Endpoint == op.Endpoint) && (requested.HostFingerprint == "" || requested.HostFingerprint == op.HostFingerprint)
}

func (c Client) renewDelegation(ctx context.Context, d *Delegation, op approval.Operation) error {
	r, err := c.Call(ctx, broker.Message{Action: "renewalChallenge", ID: d.ParentID, Ticket: d.Ticket})
	if err != nil {
		return err
	}
	if r.Renewal != nil {
		challenge := *r.Renewal
		if challenge.SessionID != d.ParentID || !d.renewalScopeMatches(challenge.Operation, op) {
			return errors.New("续期申请的客户端、服务器或权限已变化")
		}
		sig := ed25519.Sign(d.Key, approval.RenewalProofMessage(challenge))
		r, err = c.Call(ctx, broker.Message{Action: "renewSession", ID: challenge.ID, Proof: sig})
		if err != nil {
			return err
		}
	}
	if r.Request == nil || r.Request.ID != d.ParentID || !d.renewalScopeMatches(r.Request.Operation, op) {
		return errors.New("代理未返回相同范围的会话续期申请")
	}
	if r.Request.Status == "pending" && r.Request.Renewal {
		if c.OnSessionRenewal != nil {
			c.OnSessionRenewal(*r.Request)
		}
		return c.Wait(ctx, d.ParentID, d.Ticket)
	}
	if r.Request.Status != "approved" {
		return approval.ErrDenied
	}
	return nil
}
