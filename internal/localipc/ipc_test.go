package localipc

import (
	"encoding/binary"
	"net"
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func TestOversizedFrameRejected(t *testing.T) {
	a, b := net.Pipe()
	defer a.Close()
	defer b.Close()
	go func() { var h [4]byte; binary.BigEndian.PutUint32(h[:], MaxFrame+1); b.Write(h[:]) }()
	var v any
	if e := (&Wire{Conn: a}).Receive(&v); e == nil {
		t.Fatal("oversized allocation allowed")
	}
}
func TestPrivateDirectoryAndKernelLock(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "private")
	release, e := Acquire(dir)
	if e != nil {
		t.Fatal(e)
	}
	if r, e := Acquire(dir); e == nil {
		r()
		t.Fatal("acquired twice")
	}
	release()
	r, e := Acquire(dir)
	if e != nil {
		t.Fatal("lock not released", e)
	}
	r()
	if runtime.GOOS != "windows" {
		os.Chmod(dir, 0755)
		if e = Prepare(dir); e == nil {
			t.Fatal("accepted public directory")
		}
	}
}
