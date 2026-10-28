//go:build linux

package localipc

import "golang.org/x/sys/unix"

// peerUIDOfFD reads SO_PEERCRED, which the kernel fills in for a connected Unix
// socket.
func peerUIDOfFD(fd int) (uint32, error) {
	cred, err := unix.GetsockoptUcred(fd, unix.SOL_SOCKET, unix.SO_PEERCRED)
	if err != nil {
		return 0, err
	}
	return cred.Uid, nil
}
