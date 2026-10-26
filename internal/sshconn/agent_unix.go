//go:build !windows

package sshconn

import (
	"context"
	"errors"
	"io"
	"net"
	"os"
)

func dialAgent(ctx context.Context) (io.ReadWriteCloser, error) {
	p := os.Getenv("SSH_AUTH_SOCK")
	if p == "" {
		return nil, errors.New("未配置SSH代理")
	}
	return (&net.Dialer{}).DialContext(ctx, "unix", p)
}
