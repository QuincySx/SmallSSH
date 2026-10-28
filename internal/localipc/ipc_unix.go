//go:build !windows

package localipc

import (
	"context"
	"errors"
	"golang.org/x/sys/unix"
	"net"
	"os"
	"path/filepath"
)

func Prepare(dir string) error {
	if e := os.MkdirAll(dir, 0700); e != nil {
		return e
	}
	st, e := os.Lstat(dir)
	if e != nil {
		return e
	}
	if !st.IsDir() || st.Mode()&os.ModeSymlink != 0 || st.Mode().Perm()&0077 != 0 {
		return errors.New("数据目录必须为当前用户私有目录（权限0700）")
	}
	var stat unix.Stat_t
	if e = unix.Stat(dir, &stat); e != nil {
		return e
	}
	if stat.Uid != uint32(os.Getuid()) {
		return errors.New("数据目录不属于当前用户")
	}
	return nil
}

// Acquire holds a kernel lock, released automatically even after a crash.
func Acquire(dir string) (func(), error) {
	if e := Prepare(dir); e != nil {
		return nil, e
	}
	f, e := os.OpenFile(filepath.Join(dir, "vault.lock"), os.O_CREATE|os.O_RDWR, 0600)
	if e != nil {
		return nil, e
	}
	if e = unix.Flock(int(f.Fd()), unix.LOCK_EX|unix.LOCK_NB); e != nil {
		f.Close()
		return nil, errors.New("运维代理或另一写入程序已在运行")
	}
	return func() { unix.Flock(int(f.Fd()), unix.LOCK_UN); f.Close() }, nil
}
func Listen(dir string) (net.Listener, error) {
	if e := Prepare(dir); e != nil {
		return nil, e
	}
	// Callers must hold Acquire. A crashed agent may leave this socket behind.
	p := filepath.Join(dir, "agent.sock")
	if st, e := os.Lstat(p); e == nil {
		if st.Mode()&os.ModeSocket == 0 {
			return nil, errors.New("本地套接字路径被其他文件占用")
		}
		if e = os.Remove(p); e != nil {
			return nil, e
		}
	}
	l, e := net.Listen("unix", p)
	if e != nil {
		return nil, e
	}
	if e = os.Chmod(p, 0600); e != nil {
		l.Close()
		return nil, e
	}
	return sameUserListener{l}, nil
}

// sameUserListener drops connections that come from another user before the
// broker reads a single frame. The 0700 directory and 0600 socket already keep
// other users out on their own; this makes the boundary explicit instead of
// inferring it from file modes, and it is the only place the local peer is
// actually identified.
type sameUserListener struct{ net.Listener }

func (l sameUserListener) Accept() (net.Conn, error) {
	for {
		c, e := l.Listener.Accept()
		if e != nil {
			return nil, e
		}
		uid, e := peerUID(c)
		if e != nil || uid != uint32(os.Getuid()) {
			c.Close()
			continue
		}
		return c, nil
	}
}

// peerUID asks the kernel who is on the other end of a local connection.
func peerUID(c net.Conn) (uint32, error) {
	sc, ok := c.(*net.UnixConn)
	if !ok {
		return 0, errors.New("本地连接不是 Unix 套接字")
	}
	raw, e := sc.SyscallConn()
	if e != nil {
		return 0, e
	}
	var uid uint32
	var credErr error
	if e = raw.Control(func(fd uintptr) { uid, credErr = peerUIDOfFD(int(fd)) }); e != nil {
		return 0, e
	}
	return uid, credErr
}
func Dial(ctx context.Context, dir string) (net.Conn, error) {
	if e := Prepare(dir); e != nil {
		return nil, e
	}
	return (&net.Dialer{}).DialContext(ctx, "unix", filepath.Join(dir, "agent.sock"))
}
