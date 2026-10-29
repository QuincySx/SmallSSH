package localipc

import (
	"context"
	"encoding/binary"
	"fmt"
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

// The local socket identifies its peer: a connection from this user is accepted
// and the kernel-reported uid is ours.
func TestListenerOnlyAcceptsTheCurrentUser(t *testing.T) {
	// A Unix socket path is short; t.TempDir can push it past the limit.
	dir, err := os.MkdirTemp("", "ipc")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(dir) })
	if err = os.Chmod(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	release, err := Acquire(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer release()
	ln, err := Listen(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()

	accepted := make(chan error, 1)
	go func() {
		c, err := ln.Accept()
		if err != nil {
			accepted <- err
			return
		}
		uid, err := peerUID(c)
		c.Close()
		if err == nil && uid != uint32(os.Getuid()) {
			err = fmt.Errorf("accepted a peer of uid %d", uid)
		}
		accepted <- err
	}()
	c, err := Dial(context.Background(), dir)
	if err != nil {
		t.Fatal(err)
	}
	c.Close()
	if err := <-accepted; err != nil {
		t.Fatal(err)
	}
}

// The process on the other end of the socket is what the broker counts a
// caller's share of the approval queue by; here it is this test process.
func TestListenerReportsThePeerProcess(t *testing.T) {
	dir, err := os.MkdirTemp("", "ipcpid")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(dir) })
	if err = os.Chmod(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	release, err := Acquire(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer release()
	ln, err := Listen(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	pid := make(chan int, 1)
	go func() {
		c, err := ln.Accept()
		if err != nil {
			pid <- 0
			return
		}
		defer c.Close()
		got, err := PeerPID(c)
		if err != nil {
			pid <- 0
			return
		}
		pid <- got
	}()
	c, err := Dial(context.Background(), dir)
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	if got := <-pid; got != os.Getpid() {
		t.Fatalf("peer process reported as %d, want %d", got, os.Getpid())
	}
}
