//go:build terminal_integration

package desktop

import (
	"context"
	"github.com/egoist/mygo/plugins/terminal"
	"os"
	"sshclient/internal/approval"
	"sshclient/internal/broker"
	"sshclient/internal/client"
	"sshclient/internal/localipc"
	"sshclient/internal/monitor"
	"sshclient/internal/testserver"
	"sshclient/internal/vault"
	"strings"
	"testing"
	"time"
)

func TestGhosttyThroughApprovedBrokerAndRevoke(t *testing.T) {
	fixture := testserver.New(t)
	dir, e := os.MkdirTemp("", "ssh-ghostty-test-")
	if e != nil {
		t.Fatal(e)
	}
	defer os.RemoveAll(dir)
	if e = localipc.Prepare(dir); e != nil {
		t.Fatal(e)
	}
	pass := []byte("temporary integration-only master password")
	v, _ := vault.New(pass)
	v.Put(fixture.Profile())
	if e = vault.Save(broker.VaultPath(dir), v, true); e != nil {
		t.Fatal(e)
	}
	v.Close()
	s, e := broker.Open(dir, pass)
	if e != nil {
		t.Fatal(e)
	}
	defer s.Close()
	cli := client.Client{Dir: dir}
	ctx := context.Background()
	r, ticket, e := cli.Request(ctx, approval.Operation{Host: "fixture", Kind: "shell", ServerStatus: true, Client: "generated test"}, time.Minute)
	if e != nil {
		t.Fatal(e)
	}
	if e = cli.Decide(ctx, r.ID, true, false, pass); e != nil {
		t.Fatal(e)
	}
	stream, e := cli.Start(ctx, r.ID, ticket, 80, 24)
	if e != nil {
		t.Fatal(e)
	}
	defer stream.Close()
	statusSeen := make(chan struct{}, 1)
	onStatus := func() {
		select {
		case statusSeen <- struct{}{}:
		default:
		}
	}
	conn := &client.TerminalConn{Stream: stream, OnStats: func(*monitor.Stats) { onStatus() }, OnMonitorError: func(string) { onStatus() }}
	term, e := terminal.New(terminal.Options{Conn: conn, ClipboardRead: terminal.ClipboardDeny, NoClipboardWrite: true})
	if e != nil {
		t.Fatal(e)
	}
	defer term.Close()
	term.Send([]byte("Ghostty remote echo"))
	deadline := time.Now().Add(3 * time.Second)
	for !strings.Contains(term.Text(), "Ghostty remote echo") {
		if time.Now().After(deadline) {
			t.Fatal("terminal did not render remote SSH output", term.Text())
		}
		time.Sleep(10 * time.Millisecond)
	}
	select {
	case <-statusSeen:
	case <-time.After(3 * time.Second):
		t.Fatal("Ghostty reader did not dispatch automatic status")
	}
	if strings.Contains(term.Text(), "Linux 状态接口") || strings.Contains(term.Text(), "monitor-error") {
		t.Fatal("status events contaminated terminal output", term.Text())
	}
	if e = cli.Decide(ctx, r.ID, false, true, pass); e != nil {
		t.Fatal(e)
	}
	select {
	case <-term.Done():
	case <-time.After(2 * time.Second):
		t.Fatal("Ghostty session survived revoke")
	}
}
