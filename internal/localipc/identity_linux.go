//go:build linux

package localipc

import (
	"fmt"
	"net"
	"os"
	"strings"

	"golang.org/x/sys/unix"
)

func identify(c net.Conn) (*ProcessIdentity, error) {
	pid, err := PeerPID(c)
	if err != nil || pid <= 0 {
		return nil, ErrIdentityUnavailable
	}
	conn, ok := c.(*net.UnixConn)
	if !ok {
		return nil, ErrIdentityUnavailable
	}
	raw, err := conn.SyscallConn()
	if err != nil {
		return nil, ErrIdentityUnavailable
	}
	var fd int
	var peerErr error
	if err := raw.Control(func(socket uintptr) {
		fd, peerErr = unix.GetsockoptInt(int(socket), unix.SOL_SOCKET, unix.SO_PEERPIDFD)
	}); err != nil || peerErr != nil {
		return nil, ErrIdentityUnavailable
	}
	// Use the socket's actual peer handle; a numeric PID lookup races reuse.
	unix.CloseOnExec(fd)
	process := os.NewFile(uintptr(fd), "peer process")
	fail := func() (*ProcessIdentity, error) { process.Close(); return nil, ErrIdentityUnavailable }
	start, err := linuxStart(pid)
	if err != nil {
		return fail()
	}
	imagePath := fmt.Sprintf("/proc/%d/exe", pid)
	image, err := os.Open(imagePath)
	if err != nil {
		return fail()
	}
	code, info, err := executableDigest(image)
	image.Close()
	if err != nil {
		return fail()
	}
	p := &ProcessIdentity{PID: pid, Instance: fmt.Sprintf("linux:%d:%s", pid, start), CodeID: code, close: func() { process.Close() }}
	p.verify = func() error {
		fds := []unix.PollFd{{Fd: int32(fd), Events: unix.POLLIN}}
		if n, err := unix.Poll(fds, 0); err != nil || n != 0 {
			return ErrIdentityUnavailable
		}
		current, err := linuxStart(pid)
		if err != nil || current != start {
			return ErrIdentityUnavailable
		}
		currentImage, err := os.Stat(imagePath)
		if err != nil || !sameImage(info, currentImage) {
			return ErrIdentityUnavailable
		}
		return nil
	}
	if err := p.Verify(); err != nil {
		p.Close()
		return nil, err
	}
	return p, nil
}

func linuxStart(pid int) (string, error) {
	data, err := os.ReadFile(fmt.Sprintf("/proc/%d/stat", pid))
	if err != nil || len(data) > 16384 {
		return "", ErrIdentityUnavailable
	}
	end := strings.LastIndex(string(data), ") ")
	if end < 0 {
		return "", ErrIdentityUnavailable
	}
	fields := strings.Fields(string(data[end+2:]))
	if len(fields) <= 19 {
		return "", ErrIdentityUnavailable
	}
	return fields[19], nil
}
