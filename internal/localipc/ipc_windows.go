package localipc

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"github.com/Microsoft/go-winio"
	"golang.org/x/sys/windows"
	"net"
	"os"
	"path/filepath"
)

func identity() (string, error) {
	u, e := windows.GetCurrentProcessToken().GetTokenUser()
	if e != nil {
		return "", e
	}
	return u.User.Sid.String(), nil
}
func Prepare(dir string) error {
	if e := os.MkdirAll(dir, 0700); e != nil {
		return e
	}
	st, e := os.Lstat(dir)
	if e != nil {
		return e
	}
	if !st.IsDir() || st.Mode()&os.ModeSymlink != 0 {
		return errors.New("数据目录不合法")
	}
	sid, e := identity()
	if e != nil {
		return e
	}
	sd, e := windows.SecurityDescriptorFromString("D:P(A;OICI;FA;;;" + sid + ")")
	if e != nil {
		return e
	}
	acl, _, e := sd.DACL()
	if e != nil {
		return e
	}
	return windows.SetNamedSecurityInfo(dir, windows.SE_FILE_OBJECT, windows.DACL_SECURITY_INFORMATION|windows.PROTECTED_DACL_SECURITY_INFORMATION, nil, nil, acl, nil)
}
func Acquire(dir string) (func(), error) {
	if e := Prepare(dir); e != nil {
		return nil, e
	}
	f, e := os.OpenFile(filepath.Join(dir, "vault.lock"), os.O_CREATE|os.O_RDWR, 0600)
	if e != nil {
		return nil, e
	}
	o := new(windows.Overlapped)
	if e = windows.LockFileEx(windows.Handle(f.Fd()), windows.LOCKFILE_EXCLUSIVE_LOCK|windows.LOCKFILE_FAIL_IMMEDIATELY, 0, 1, 0, o); e != nil {
		f.Close()
		return nil, errors.New("运维代理或另一写入程序已在运行")
	}
	return func() { windows.UnlockFileEx(windows.Handle(f.Fd()), 0, 1, 0, o); f.Close() }, nil
}
func pipeName(dir string) string {
	p, _ := filepath.Abs(dir)
	s, _ := identity()
	h := sha256.Sum256([]byte(s + "\x00" + p))
	return `\\.\pipe\ssh-client-` + hex.EncodeToString(h[:16])
}
func Listen(dir string) (net.Listener, error) {
	if e := Prepare(dir); e != nil {
		return nil, e
	}
	sid, e := identity()
	if e != nil {
		return nil, e
	}
	return winio.ListenPipe(pipeName(dir), &winio.PipeConfig{SecurityDescriptor: "D:P(A;;GA;;;" + sid + ")", InputBufferSize: 65536, OutputBufferSize: 65536})
}
func Dial(ctx context.Context, dir string) (net.Conn, error) {
	return winio.DialPipeContext(ctx, pipeName(dir))
}

// PeerPID asks the kernel which process is on the other end of the named pipe.
// The pipe's DACL already limits who may connect; this is what counts a caller's
// share of the approval queue by the process rather than by a label it writes.
func PeerPID(c net.Conn) (int, error) {
	f, ok := c.(interface{ Fd() uintptr })
	if !ok {
		return 0, errors.New("本地连接类型异常")
	}
	var pid uint32
	if e := windows.GetNamedPipeClientProcessId(windows.Handle(f.Fd()), &pid); e != nil {
		return 0, e
	}
	return int(pid), nil
}
