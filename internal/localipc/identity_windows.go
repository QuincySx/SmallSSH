package localipc

import (
	"fmt"
	"net"
	"os"

	"golang.org/x/sys/windows"
)

func identify(c net.Conn) (*ProcessIdentity, error) {
	pid, err := PeerPID(c)
	if err != nil || pid <= 0 {
		return nil, ErrIdentityUnavailable
	}
	return processIdentity(pid)
}

func identifyServer(c net.Conn) (*ProcessIdentity, error) {
	pid, err := ServerPID(c)
	if err != nil || pid <= 0 {
		return nil, ErrIdentityUnavailable
	}
	return processIdentity(pid)
}

func processIdentity(pid int) (*ProcessIdentity, error) {
	handle, err := windows.OpenProcess(windows.PROCESS_QUERY_LIMITED_INFORMATION|windows.SYNCHRONIZE, false, uint32(pid))
	if err != nil {
		return nil, ErrIdentityUnavailable
	}
	fail := func() (*ProcessIdentity, error) { windows.CloseHandle(handle); return nil, ErrIdentityUnavailable }
	var created, exited, kernel, user windows.Filetime
	if err := windows.GetProcessTimes(handle, &created, &exited, &kernel, &user); err != nil {
		return fail()
	}
	name := make([]uint16, 32768)
	n := uint32(len(name))
	if windows.QueryFullProcessImageName(handle, 0, &name[0], &n) != nil {
		return fail()
	}
	path := windows.UTF16ToString(name[:n])
	image, err := os.Open(path)
	if err != nil {
		return fail()
	}
	code, info, err := executableDigest(image)
	image.Close()
	if err != nil {
		return fail()
	}
	p := &ProcessIdentity{PID: pid, Instance: fmt.Sprintf("windows:%d:%08x%08x", pid, created.HighDateTime, created.LowDateTime), CodeID: code, close: func() { windows.CloseHandle(handle) }}
	p.verify = func() error {
		status, err := windows.WaitForSingleObject(handle, 0)
		if err != nil || status != uint32(windows.WAIT_TIMEOUT) {
			return ErrIdentityUnavailable
		}
		current, err := os.Stat(path)
		if err != nil || !sameImage(info, current) {
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
