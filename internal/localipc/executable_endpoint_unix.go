//go:build !windows

package localipc

import (
	"errors"
	"net"
	"os"
	"syscall"
)

func executableEndpointUnavailable(err error) bool {
	var operation *net.OpError
	if !errors.As(err, &operation) || operation.Op != "dial" || operation.Net != "unix" {
		return false
	}
	return errors.Is(operation.Err, os.ErrNotExist) || errors.Is(operation.Err, syscall.ECONNREFUSED)
}
