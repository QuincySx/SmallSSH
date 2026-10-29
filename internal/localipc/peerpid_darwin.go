//go:build darwin

package localipc

import "golang.org/x/sys/unix"

// peerPIDOfFD reads LOCAL_PEERPID, which the kernel fills in for a connected Unix
// socket.
func peerPIDOfFD(fd int) (int, error) {
	return unix.GetsockoptInt(fd, unix.SOL_LOCAL, unix.LOCAL_PEERPID)
}
