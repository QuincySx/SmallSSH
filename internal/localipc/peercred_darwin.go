//go:build darwin

package localipc

import "golang.org/x/sys/unix"

// peerUIDOfFD reads LOCAL_PEERCRED, which the kernel fills in for a connected
// Unix socket.
func peerUIDOfFD(fd int) (uint32, error) {
	cred, err := unix.GetsockoptXucred(fd, unix.SOL_LOCAL, unix.LOCAL_PEERCRED)
	if err != nil {
		return 0, err
	}
	return cred.Uid, nil
}
