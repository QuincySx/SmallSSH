//go:build !windows

package transfer

import (
	"errors"
	"os"

	"golang.org/x/sys/unix"
)

func tryMetadataLock(f *os.File) (bool, error) {
	err := unix.Flock(int(f.Fd()), unix.LOCK_EX|unix.LOCK_NB)
	if errors.Is(err, unix.EWOULDBLOCK) || errors.Is(err, unix.EAGAIN) || errors.Is(err, unix.EINTR) {
		return false, nil
	}
	return err == nil, err
}

func unlockMetadata(f *os.File) { _ = unix.Flock(int(f.Fd()), unix.LOCK_UN) }
