package client

import (
	"context"
	"errors"

	"sshclient/internal/broker"
	"sshclient/internal/vault"
)

func (c Client) CallWithOwner(ctx context.Context, q broker.Message, owner *vault.Session) (broker.Message, error) {
	if owner == nil || !broker.OwnerQueryAction(q.Action) {
		return broker.Message{}, errors.New("此查询需要所有者证明")
	}
	var result broker.Message
	for {
		challenge, err := c.call(ctx, broker.Message{Action: "profileChallenge"})
		if err != nil {
			return broker.Message{}, err
		}
		q.Challenge = challenge.ID
		q.Proof, err = broker.OwnerQueryProof(q.Challenge, q, owner.Proof)
		if err != nil {
			return broker.Message{}, err
		}
		page, err := c.call(ctx, q)
		if err != nil {
			return broker.Message{}, err
		}
		if q.Action != "pending" {
			return page, nil
		}
		result.Requests = append(result.Requests, page.Requests...)
		if page.Next == "" {
			return result, nil
		}
		if page.Next <= q.After || len(result.Requests) > 128 {
			return broker.Message{}, errors.New("无效授权分页")
		}
		q.After = page.Next
	}
}
