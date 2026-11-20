//go:build !windows

package systemauth

import (
	"errors"
	"os"

	"golang.org/x/sys/unix"
)

func tryKeyringLock(directory *os.File) (func(), error) {
	fd, err := unix.Openat(int(directory.Fd()), ".lock", unix.O_CREAT|unix.O_RDWR|unix.O_NOFOLLOW|unix.O_CLOEXEC|unix.O_NONBLOCK, 0600)
	if err != nil {
		return nil, err
	}
	file := os.NewFile(uintptr(fd), ".lock")
	var stat unix.Stat_t
	if err := unix.Fstat(fd, &stat); err != nil {
		file.Close()
		return nil, err
	}
	if stat.Mode&unix.S_IFMT != unix.S_IFREG || stat.Mode&0077 != 0 || stat.Uid != uint32(os.Getuid()) || stat.Nlink != 1 || stat.Size != 0 {
		file.Close()
		return nil, errors.New("系统钥匙串清理锁无效")
	}
	if err := unix.Flock(fd, unix.LOCK_EX|unix.LOCK_NB); err != nil {
		file.Close()
		if errors.Is(err, unix.EWOULDBLOCK) || errors.Is(err, unix.EAGAIN) {
			return nil, errKeyringLockBusy
		}
		return nil, err
	}
	return func() {
		_ = unix.Flock(fd, unix.LOCK_UN)
		_ = file.Close()
	}, nil
}
