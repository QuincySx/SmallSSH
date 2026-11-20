package fileattrs

import (
	"errors"
	"os"
	"strconv"

	"golang.org/x/sys/unix"
)

type linuxAttributeHandle struct{ *os.File }

func openAttributeHandle(name string) (localAttributeHandle, error) {
	fd, err := unix.Open(name, unix.O_PATH|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0)
	if err != nil {
		return nil, &os.PathError{Op: "open attributes", Path: name, Err: err}
	}
	return linuxAttributeHandle{os.NewFile(uintptr(fd), name)}, nil
}

// Control pins the descriptor through the syscall, including concurrent Close
// from context cancellation, so /proc fallback never observes a reused fd.
func (f linuxAttributeHandle) control(name string, action func(int) error) error {
	raw, err := f.SyscallConn()
	if err != nil {
		return err
	}
	var result error
	if err := raw.Control(func(fd uintptr) { result = action(int(fd)) }); err != nil {
		return err
	}
	return os.NewSyscallError(name, result)
}

func (f linuxAttributeHandle) Chmod(mode os.FileMode) error {
	return f.control("fchmodat", func(fd int) error {
		err := unix.Fchmodat(fd, "", ModeBits(mode), unix.AT_EMPTY_PATH)
		if errors.Is(err, unix.ENOSYS) || errors.Is(err, unix.EOPNOTSUPP) || errors.Is(err, unix.EINVAL) {
			// Older kernels lack fchmodat2. This kernel-owned link refers to the
			// held inode, even if its original directory entry has been replaced.
			return unix.Chmod("/proc/self/fd/"+strconv.Itoa(fd), ModeBits(mode))
		}
		return err
	})
}

func (f linuxAttributeHandle) Chown(uid, gid int) error {
	return f.control("fchownat", func(fd int) error {
		return unix.Fchownat(fd, "", uid, gid, unix.AT_EMPTY_PATH)
	})
}
