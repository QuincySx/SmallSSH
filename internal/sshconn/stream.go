package sshconn

import (
	"context"
	"errors"
	"io"

	"golang.org/x/crypto/ssh"
)

// ExecStream forwards output as it arrives. Cancellation closes the channel,
// without claiming that a remote mutation was rolled back.
func (c *Client) ExecStream(ctx context.Context, command string, stdout, stderr io.Writer) (int, error) {
	s, err := c.SSH.NewSession()
	if err != nil {
		return 0, err
	}
	defer s.Close()
	stop := context.AfterFunc(ctx, func() { s.Close() })
	defer stop()
	s.Stdout, s.Stderr = stdout, stderr
	err = s.Run(command)
	if err != nil {
		var exit *ssh.ExitError
		if errors.As(err, &exit) {
			return exit.ExitStatus(), nil
		}
		return 0, err
	}
	return 0, nil
}
