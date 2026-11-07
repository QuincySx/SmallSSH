//go:build !windows

package localipc

import (
	"net"
	"runtime"
)

func identifyServer(c net.Conn) (*ProcessIdentity, error) {
	switch runtime.GOOS {
	case "darwin", "linux":
		return identify(c)
	default:
		return nil, ErrIdentityUnavailable
	}
}
