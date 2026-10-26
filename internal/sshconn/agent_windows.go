package sshconn

import (
	"context"
	"github.com/Microsoft/go-winio"
	"io"
)

func dialAgent(ctx context.Context) (io.ReadWriteCloser, error) {
	return winio.DialPipeContext(ctx, `\\.\pipe\openssh-ssh-agent`)
}
