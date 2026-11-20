package client

import (
	"context"
	"errors"

	"sshclient/internal/protocol"
)

func (c Client) CallWithOwner(ctx context.Context, q protocol.Message, owner OwnerProof) (protocol.Message, error) {
	if !HasOwnerProof(owner) || !protocol.OwnerQueryAction(q.Action) {
		return protocol.Message{}, errors.New("此查询需要所有者证明")
	}
	var result protocol.Message
	for {
		challenge, err := c.call(ctx, protocol.Message{Action: "profileChallenge"})
		if err != nil {
			return protocol.Message{}, err
		}
		q.Challenge = challenge.ID
		q.Proof, err = protocol.OwnerQueryProof(q.Challenge, q, owner.Proof)
		if err != nil {
			return protocol.Message{}, err
		}
		page, err := c.call(ctx, q)
		if err != nil {
			return protocol.Message{}, err
		}
		if q.Action != "pending" {
			return page, nil
		}
		result.Requests = append(result.Requests, page.Requests...)
		if page.Next == "" {
			return result, nil
		}
		if page.Next <= q.After || len(result.Requests) > 128 {
			return protocol.Message{}, errors.New("无效授权分页")
		}
		q.After = page.Next
	}
}
