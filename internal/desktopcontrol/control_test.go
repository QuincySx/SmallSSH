package desktopcontrol

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"sshclient/internal/localipc"
)

func TestPrivateControlSingleInstanceCommandsAndRestart(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	dir := filepath.Join(t.TempDir(), strings.Repeat("long-checkout-", 15))
	var shows atomic.Int32
	stopped := make(chan struct{}, 1)
	handler := func(_ context.Context, action string) (Reply, error) {
		if action == "show" {
			shows.Add(1)
		}
		r := Reply{State: State{Instance: "first", Ready: true, Visible: shows.Load() > 0, BackgroundStart: true}}
		if action == "quit" {
			r.AfterReply = func() { stopped <- struct{}{} }
		}
		return r, nil
	}
	s, err := Start(ctx, dir, handler)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	p, err := endpoint(dir)
	if err != nil || (runtime.GOOS != "windows" && len(filepath.Join(p, "agent.sock")) >= 104) {
		t.Fatal("control socket path too long", p, err)
	}
	if runtime.GOOS != "windows" {
		st, err := os.Stat(p)
		if err != nil || st.Mode().Perm() != 0700 {
			t.Fatal("control directory is not private", err)
		}
	}
	if other, err := Start(ctx, dir, handler); err == nil {
		other.Close()
		t.Fatal("second server stole control endpoint")
	}
	state, err := Command(ctx, dir, "status")
	if err != nil || !state.Ready || state.Visible || shows.Load() != 0 {
		t.Fatal("status changed visibility", err)
	}
	if _, err = Command(ctx, dir, "unlock"); err == nil {
		t.Fatal("invalid command accepted")
	}
	wire, err := localipc.Connect(ctx, p)
	if err != nil {
		t.Fatal(err)
	}
	if err = wire.Send(message{Action: "request"}); err != nil {
		t.Fatal(err)
	}
	var denied message
	if err = wire.Receive(&denied); err != nil || denied.Error == "" || shows.Load() != 0 {
		t.Fatal("server accepted non-window action", err)
	}
	wire.Conn.Close()
	state, err = Command(ctx, dir, "show")
	if err != nil || !state.Visible || shows.Load() != 1 {
		t.Fatal("explicit show failed", err)
	}
	if _, err = Command(ctx, dir, "quit"); err != nil {
		t.Fatal("quit acknowledgement lost", err)
	}
	select {
	case <-stopped:
	case <-time.After(time.Second):
		t.Fatal("quit callback missing")
	}
	s.Close()
	shows.Store(0)
	next, err := Start(ctx, dir, handler)
	if err != nil {
		t.Fatal("restart failed", err)
	}
	defer next.Close()
	state, err = Command(ctx, dir, "status")
	if err != nil || state.Visible || shows.Load() != 0 {
		t.Fatal("restart restored visibility", err)
	}
}

func TestControlShutdownClosesIncompleteClientAndHandler(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	dir := t.TempDir()
	entered := make(chan struct{})
	s, err := Start(ctx, dir, func(ctx context.Context, _ string) (Reply, error) {
		close(entered)
		<-ctx.Done()
		return Reply{}, ctx.Err()
	})
	if err != nil {
		t.Fatal(err)
	}
	p, _ := endpoint(dir)
	partial, err := localipc.Connect(ctx, p)
	if err != nil {
		t.Fatal(err)
	}
	defer partial.Conn.Close()
	response := make(chan struct{})
	go func() { _, _ = Command(ctx, dir, "status"); close(response) }()
	select {
	case <-entered:
	case <-time.After(time.Second):
		t.Fatal("handler did not start")
	}
	cancel()
	closed := make(chan struct{})
	go func() { s.Close(); close(closed) }()
	select {
	case <-closed:
	case <-time.After(time.Second):
		t.Fatal("shutdown blocked on client")
	}
	select {
	case <-response:
	case <-time.After(time.Second):
		t.Fatal("request not cancelled")
	}
}
