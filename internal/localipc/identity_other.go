//go:build !darwin && !linux && !windows

package localipc

import "net"

func identify(net.Conn) (*ProcessIdentity, error) { return nil, ErrIdentityUnavailable }
