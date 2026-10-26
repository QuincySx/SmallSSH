package client

import (
	"context"
	"errors"
	"time"

	"sshclient/internal/approval"
	"sshclient/internal/broker"
	"sshclient/internal/sshconn"
	"sshclient/internal/vault"
)

func (c Client) SaveConnection(ctx context.Context, change vault.ProfileChange, owner *vault.Session) error {
	p, err := owner.ResolveChange(change)
	if err != nil {
		return err
	}
	if err = sshconn.ValidateTarget(p.Host, p.Port); err != nil {
		return err
	}
	if p.Auth == "key" {
		if _, err = sshconn.InspectPrivateKey(p.PrivateKey, p.KeyPassphrase); err != nil {
			return err
		}
	}
	challenge, err := c.Call(ctx, broker.Message{Action: "profileChallenge"})
	if err != nil {
		return err
	}
	proof, err := broker.ProfileChangeProof(challenge.ID, change, owner.Proof)
	if err != nil {
		return err
	}
	_, err = c.Call(ctx, broker.Message{Action: "profileChange", ID: challenge.ID, ProfileChange: &change, Proof: proof})
	return err
}

// An explicit desktop action uses its already-unlocked in-process owner session
// to approve only this exact request. CLI/AI requests retain their normal queue.
func (c Client) ConnectWithOwner(ctx context.Context, host string, owner *vault.Session) (*Stream, error) {
	return c.StartWithOwner(ctx, approval.Operation{Host: host, Kind: "shell", ServerStatus: true}, owner)
}

// StartWithOwner is an in-process owner frontend helper, never a wire action.
// It signs only the newly returned immutable request, irrespective of its label.
func (c Client) StartWithOwner(ctx context.Context, op approval.Operation, owner *vault.Session) (*Stream, error) {
	if owner == nil {
		return nil, errors.New("保险库已锁定")
	}
	op.Client = "桌面操作（自报名称）"
	r, ticket, err := c.RequestWithOwner(ctx, op, owner)
	if err != nil {
		return nil, err
	}

	success := false
	defer func() {
		if !success {
			cleanup, cancel := context.WithTimeout(context.Background(), 2*time.Second)
			defer cancel()
			_, _ = c.Call(cleanup, broker.Message{Action: "withdraw", ID: r.ID, Ticket: ticket})
		}
	}()
	if err = c.DecideWithOwner(ctx, r.ID, true, false, owner); err != nil {
		return nil, err
	}
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}
	s, err := c.Start(ctx, r.ID, ticket, 100, 30)
	if err != nil {
		return nil, err
	}
	if s == nil {
		return nil, errors.New("代理未返回操作通道")
	}
	success = true
	s.Operation = r.Operation
	return s, nil
}

func (c Client) RequestWithOwner(ctx context.Context, op approval.Operation, owner *vault.Session) (approval.Request, string, error) {
	if owner == nil {
		return approval.Request{}, "", errors.New("保险库已锁定")
	}
	challenge, err := c.Call(ctx, broker.Message{Action: "profileChallenge"})
	if err != nil {
		return approval.Request{}, "", err
	}
	proof, err := broker.OwnerRequestProof(challenge.ID, op, owner.Proof)
	if err != nil {
		return approval.Request{}, "", err
	}
	result, err := c.Call(ctx, broker.Message{Action: "ownerRequest", ID: challenge.ID, Operation: op, Proof: proof})
	if err != nil {
		return approval.Request{}, "", err
	}
	if result.Request == nil {
		return approval.Request{}, "", errors.New("代理未返回操作授权")
	}
	return *result.Request, result.Ticket, nil
}
