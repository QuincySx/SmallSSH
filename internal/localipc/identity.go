package localipc

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"net"
	"os"
	"sync"
)

var ErrIdentityUnavailable = errors.New("无法验证调用进程实例；拒绝授权")

// ProcessIdentity is obtained from the transport and the OS, never a message.
// CodeID identifies this executable, not a trusted publisher or safe behaviour.
type ProcessIdentity struct {
	PID      int
	Instance string
	CodeID   string
	AppID    string
	mu       sync.Mutex
	verify   func() error
	close    func()
	closed   bool
}

func Identify(c net.Conn) (*ProcessIdentity, error) { return identify(c) }

// IdentifyServer identifies the process at the server end of the local connection.
func IdentifyServer(c net.Conn) (*ProcessIdentity, error) { return identifyServer(c) }

func (p *ProcessIdentity) Verify() error {
	if p == nil {
		return ErrIdentityUnavailable
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.closed || p.Instance == "" || p.CodeID == "" || p.verify == nil {
		return ErrIdentityUnavailable
	}
	return p.verify()
}

func (p *ProcessIdentity) Same(other *ProcessIdentity) bool {
	return p != nil && other != nil && p.Instance == other.Instance && p.CodeID == other.CodeID && p.AppID == other.AppID && p.Verify() == nil && other.Verify() == nil
}

func (p *ProcessIdentity) Close() {
	if p == nil {
		return
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	if !p.closed && p.close != nil {
		p.close()
	}
	p.closed = true
}

func executableDigest(file *os.File) (string, os.FileInfo, error) {
	before, err := file.Stat()
	if err != nil || !before.Mode().IsRegular() || before.Size() > 512<<20 {
		return "", nil, ErrIdentityUnavailable
	}
	hash := sha256.New()
	if _, err := io.Copy(hash, io.LimitReader(file, (512<<20)+1)); err != nil {
		return "", nil, err
	}
	after, err := file.Stat()
	if err != nil || !sameImage(before, after) {
		return "", nil, ErrIdentityUnavailable
	}
	return "sha256:" + hex.EncodeToString(hash.Sum(nil)), after, nil
}

func sameImage(a, b os.FileInfo) bool {
	return a != nil && b != nil && os.SameFile(a, b) && a.Size() == b.Size() && a.ModTime() == b.ModTime()
}
