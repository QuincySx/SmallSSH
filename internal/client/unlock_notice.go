package client

import (
	"context"
	"errors"
	"time"
)

var ErrVaultLocked = errors.New("保险库已锁定，请在 SmallSSH 解锁后重试")

func (c Client) notifyVaultLocked(ctx context.Context, original error) error {
	if ctx.Err() != nil {
		return original
	}
	sdk := ServiceClient{Dir: c.Dir, Executable: c.ServiceExecutable}
	if sdk.Executable == "" {
		local, err := NewLocalService(c.Dir)
		if err != nil {
			return original
		}
		sdk = local.Client()
	}
	ctx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	if locked, err := sdk.RequestUnlock(ctx); err == nil && locked {
		return ErrVaultLocked
	}
	return original
}
