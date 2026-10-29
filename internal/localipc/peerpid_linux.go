//go:build linux

package localipc

import "golang.org/x/sys/unix"

// peerPIDOfFD reads the process id out of SO_PEERCRED.
func peerPIDOfFD(fd int) (int, error) {
	cred, err := unix.GetsockoptUcred(fd, unix.SOL_SOCKET, unix.SO_PEERCRED)
	if err != nil {
		return 0, err
	}
	return int(cred.Pid), nil
}
