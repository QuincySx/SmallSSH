package fileattrs

import (
	"crypto/rand"
	"errors"
	"os"
	"path/filepath"
	"sync"

	"golang.org/x/sys/unix"
)

func openAttributeHandle(name string) (localAttributeHandle, error) {
	fd, err := unix.Open(name, unix.O_RDONLY|unix.O_NONBLOCK|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0)
	if err == nil {
		return os.NewFile(uintptr(fd), name), nil
	}
	if !errors.Is(err, unix.EACCES) {
		return nil, &os.PathError{Op: "open attributes", Path: name, Err: err}
	}
	// Darwin has no public O_PATH equivalent. Keep a fixed parent directory
	// and never follow the final component while repairing an unreadable item.
	root, err := os.OpenRoot(filepath.Dir(name))
	if err != nil {
		return nil, err
	}
	directory, err := root.Open(".")
	if err != nil {
		root.Close()
		return nil, err
	}
	base := filepath.Base(name)
	st, err := root.Lstat(base)
	if err != nil || !st.Mode().IsRegular() && !st.IsDir() {
		directory.Close()
		root.Close()
		if err == nil {
			err = errors.New("仅修改普通文件和目录的属性")
		}
		return nil, err
	}
	if st.Mode().IsRegular() {
		if pinned, err := pinDarwinFile(root, directory, base, st); err == nil {
			directory.Close()
			return pinned, nil
		}
	}
	// Directories cannot be hard-linked, and a read-only parent can prevent a
	// private file pin. Pre/post checks detect replacement, but another process
	// can still replace a regular entry between a check and the *at syscall.
	return &darwinAttributeHandle{root: root, directory: directory, base: base, expected: st}, nil
}

type darwinAttributeHandle struct {
	mu        sync.Mutex
	root      *os.Root
	directory *os.File
	base      string
	expected  os.FileInfo
	container *os.Root
	pin       string
	closed    bool
	closeErr  error
}

func pinDarwinFile(parent *os.Root, directory *os.File, base string, expected os.FileInfo) (_ *darwinAttributeHandle, err error) {
	pin := ".smallssh-attributes-" + rand.Text()
	if err := parent.Mkdir(pin, 0700); err != nil {
		return nil, err
	}
	root, err := parent.OpenRoot(pin)
	if err != nil {
		parent.Remove(pin)
		return nil, err
	}
	pinDirectory, err := root.Open(".")
	if err != nil {
		root.Close()
		parent.Remove(pin)
		return nil, err
	}
	defer func() {
		if err != nil {
			root.Remove("file")
			pinDirectory.Close()
			root.Close()
			parent.Remove(pin)
		}
	}()
	if err = unix.Linkat(int(directory.Fd()), base, int(pinDirectory.Fd()), "file", 0); err != nil {
		return nil, err
	}
	st, err := root.Lstat("file")
	if err != nil {
		return nil, err
	}
	if !st.Mode().IsRegular() || !os.SameFile(expected, st) {
		return nil, errors.New("文件已被替换，请重新打开属性")
	}
	return &darwinAttributeHandle{root: root, directory: pinDirectory, base: "file", expected: st, container: parent, pin: pin}, nil
}

func (f *darwinAttributeHandle) stat() (os.FileInfo, error) {
	if f.closed {
		return nil, os.ErrClosed
	}
	st, err := f.root.Lstat(f.base)
	if err != nil {
		return nil, err
	}
	if !os.SameFile(f.expected, st) || !st.Mode().IsRegular() && !st.IsDir() {
		return nil, errors.New("文件已被替换，请重新打开属性")
	}
	return st, nil
}

func (f *darwinAttributeHandle) Stat() (os.FileInfo, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.stat()
}

func (f *darwinAttributeHandle) mutate(action func(int, string) error) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if _, err := f.stat(); err != nil {
		return err
	}
	err := action(int(f.directory.Fd()), f.base)
	_, afterErr := f.stat()
	return errors.Join(err, afterErr)
}

func (f *darwinAttributeHandle) Chmod(mode os.FileMode) error {
	return f.mutate(func(fd int, name string) error {
		return unix.Fchmodat(fd, name, ModeBits(mode), unix.AT_SYMLINK_NOFOLLOW)
	})
}

func (f *darwinAttributeHandle) Chown(uid, gid int) error {
	return f.mutate(func(fd int, name string) error {
		return unix.Fchownat(fd, name, uid, gid, unix.AT_SYMLINK_NOFOLLOW)
	})
}

func (f *darwinAttributeHandle) Close() error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.closed {
		return f.closeErr
	}
	f.closed = true
	if f.pin != "" {
		f.closeErr = f.root.Remove(f.base)
	}
	f.closeErr = errors.Join(f.closeErr, f.directory.Close(), f.root.Close())
	if f.container != nil {
		f.closeErr = errors.Join(f.closeErr, f.container.Remove(f.pin), f.container.Close())
	}
	return f.closeErr
}
