package localipc

import (
	"context"
	"net"
)

// The native Secure Enclave agent is macOS-only.
func WatchDisconnect(context.Context, net.Conn, context.CancelFunc) {}
