package client

import (
	"context"
	"errors"
	"sshclient/internal/identitymodel"
	"sshclient/internal/protocol"
)

func (c Client) Identities(ctx context.Context, owner OwnerProof) (identitymodel.State, error) {
	r, err := c.CallWithOwner(ctx, protocol.Message{Action: "workspaceRead", Workspace: &protocol.WorkspaceQuery{Kind: "ssh-identities"}}, owner)
	if err != nil {
		return identitymodel.State{}, err
	}
	if r.WorkspaceResult == nil || r.WorkspaceResult.Identities == nil {
		return identitymodel.State{}, errors.New("未返回 SSH 密钥状态")
	}
	return *r.WorkspaceResult.Identities, nil
}
func (c Client) ChangeIdentity(ctx context.Context, change identitymodel.Change, owner OwnerProof) error {
	return c.workspaceWrite(ctx, &protocol.WorkspaceChange{Kind: "ssh-identity-change", Identity: &change}, owner)
}
