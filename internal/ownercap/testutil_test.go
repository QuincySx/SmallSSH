package ownercap

import (
	"bufio"
	"context"
	"net"
	"os"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"

	"sshclient/internal/localipc"
)

const (
	testEpoch  = "0123456789abcdef0123456789abcdef"
	testEpoch2 = "fedcba9876543210fedcba9876543210"
)

type identityPair struct {
	ctx        context.Context
	cancel     context.CancelFunc
	caller     *localipc.ProcessIdentity
	peer       *localipc.ProcessIdentity
	callerConn net.Conn
	peerConn   net.Conn
}

func newIdentityPair(t *testing.T) *identityPair {
	t.Helper()
	if runtime.GOOS != "darwin" && runtime.GOOS != "linux" && runtime.GOOS != "windows" {
		t.Skip("kernel process identity is unavailable on this platform")
	}
	dir := shortTestDir(t)
	listener, err := localipc.Listen(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	type accepted struct {
		conn     net.Conn
		identity *localipc.ProcessIdentity
		err      error
	}
	acceptedCh := make(chan accepted)
	acceptDone := make(chan struct{})
	defer close(acceptDone)
	go func() {
		conn, err := listener.Accept()
		if err != nil {
			select {
			case acceptedCh <- accepted{err: err}:
			case <-acceptDone:
			}
			return
		}
		identity, err := localipc.Identify(conn)
		result := accepted{conn: conn, identity: identity, err: err}
		select {
		case acceptedCh <- result:
		case <-acceptDone:
			if identity != nil {
				identity.Close()
			}
			_ = conn.Close()
		}
	}()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	peerConn, err := localipc.Dial(ctx, dir)
	cancel()
	if err != nil {
		t.Fatal(err)
	}
	got := receiveWithin(t, acceptedCh, "local identity accept")
	if got.err != nil {
		_ = peerConn.Close()
		if got.conn != nil {
			_ = got.conn.Close()
		}
		if got.identity != nil {
			got.identity.Close()
		}
		t.Fatal(got.err)
	}
	peer, err := localipc.IdentifyServer(peerConn)
	if err != nil {
		_ = peerConn.Close()
		_ = got.conn.Close()
		got.identity.Close()
		t.Fatal(err)
	}
	if got.identity == nil || got.identity.Verify() != nil || peer.Verify() != nil || !got.identity.Same(peer) {
		_ = peerConn.Close()
		_ = got.conn.Close()
		if got.identity != nil {
			got.identity.Close()
		}
		peer.Close()
		t.Fatal("local socket did not yield the same live process instance on both ends")
	}
	pair := &identityPair{caller: got.identity, peer: peer, callerConn: got.conn, peerConn: peerConn}
	pair.ctx, pair.cancel = context.WithCancel(context.Background())
	t.Cleanup(func() {
		pair.cancel()
		pair.caller.Close()
		pair.peer.Close()
		_ = pair.callerConn.Close()
		_ = pair.peerConn.Close()
	})
	return pair
}

func shortTestDir(t *testing.T) string {
	t.Helper()
	base := ""
	if runtime.GOOS != "windows" {
		base = "/tmp"
	}
	dir, err := os.MkdirTemp(base, "ownercap-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(dir) })
	if err := os.Chmod(dir, 0700); err != nil {
		t.Fatal(err)
	}
	return dir
}

type fakeClock struct {
	mu  sync.Mutex
	now time.Time
}

func installFakeClock(t *testing.T, start time.Time) *fakeClock {
	t.Helper()
	clock := &fakeClock{now: start}
	previous := now
	now = clock.current
	t.Cleanup(func() { now = previous })
	return clock
}

func (c *fakeClock) current() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.now
}

func (c *fakeClock) Advance(duration time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.now = c.now.Add(duration)
}

func receiveWithin[T any](t *testing.T, ch <-chan T, operation string) T {
	t.Helper()
	select {
	case value := <-ch:
		return value
	case <-time.After(10 * time.Second):
		t.Fatalf("timed out waiting for %s", operation)
		var zero T
		return zero
	}
}

func readLineWithin(ctx context.Context, reader *bufio.Reader) (string, error) {
	type result struct {
		line string
		err  error
	}
	lines := make(chan result, 1)
	go func() {
		line, err := reader.ReadString('\n')
		lines <- result{line: strings.TrimSpace(line), err: err}
	}()
	select {
	case <-ctx.Done():
		return "", ctx.Err()
	case got := <-lines:
		return got.line, got.err
	}
}
