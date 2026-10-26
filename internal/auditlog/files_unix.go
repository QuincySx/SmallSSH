//go:build !windows

package auditlog

import (
	"errors"
	"fmt"
	"os"

	"golang.org/x/sys/unix"
)

func openPrivate(path string, create bool) (*os.File, error) {
	flags := unix.O_RDWR | unix.O_APPEND | unix.O_NOFOLLOW | unix.O_NONBLOCK | unix.O_CLOEXEC
	if create {
		flags |= unix.O_CREAT
	}
	fd, err := unix.Open(path, flags, 0600)
	if err != nil {
		return nil, err
	}
	f := os.NewFile(uintptr(fd), path)
	var st unix.Stat_t
	if err = unix.Fstat(fd, &st); err != nil {
		f.Close()
		return nil, err
	}
	if st.Mode&unix.S_IFMT != unix.S_IFREG || st.Mode&0077 != 0 || st.Uid != uint32(os.Getuid()) || st.Nlink != 1 {
		f.Close()
		return nil, errors.New("历史文件必须为当前用户私有的普通文件（权限0600）")
	}
	return f, nil
}

func fileIdentity(f *os.File) (string, error) {
	var st unix.Stat_t
	if err := unix.Fstat(int(f.Fd()), &st); err != nil {
		return "", err
	}
	return fmt.Sprintf("%d:%d", st.Dev, st.Ino), nil
}
