//go:build !windows

package localipc

import (
	"context"
	"golang.org/x/sys/unix"
	"net"
	"time"
)

// WatchDisconnect observes hangup without reading from the connection. This
// also works while a TLS handler is awaiting a native authentication decision.
func WatchDisconnect(ctx context.Context, c net.Conn, cancel context.CancelFunc) {
	u, ok := c.(*net.UnixConn)
	if !ok {
		return
	}
	raw, err := u.SyscallConn()
	if err != nil {
		return
	}
	t := time.NewTicker(100 * time.Millisecond)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			gone := false
			if raw.Control(func(fd uintptr) {
				p := []unix.PollFd{{Fd: int32(fd), Events: unix.POLLIN}}
				_, err := unix.Poll(p, 0)
				gone = err != nil || p[0].Revents&(unix.POLLHUP|unix.POLLERR|unix.POLLNVAL) != 0
				if !gone && p[0].Revents&unix.POLLIN != 0 {
					var peek [1]byte
					n, _, err := unix.Recvfrom(int(fd), peek[:], unix.MSG_PEEK|unix.MSG_DONTWAIT)
					gone = err == nil && n == 0
				}
			}) != nil {
				gone = true
			}
			if gone {
				cancel()
				return
			}
		}
	}
}
