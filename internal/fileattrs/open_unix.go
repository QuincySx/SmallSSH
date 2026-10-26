//go:build !windows

package fileattrs

import (
	"os"

	"golang.org/x/sys/unix"
)

func openAttributeHandle(name string) (*os.File, error) {
	// Do not follow a link swapped in after Lstat, or block opening a FIFO.
	fd, err := unix.Open(name, unix.O_RDONLY|unix.O_NONBLOCK|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0)
	if err != nil {
		return nil, &os.PathError{Op: "open attributes", Path: name, Err: err}
	}
	return os.NewFile(uintptr(fd), name), nil
}
