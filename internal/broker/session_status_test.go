package broker_test

import (
	"context"
	"runtime"
	"strings"
	"testing"
	"time"

	"sshclient/internal/approval"
	"sshclient/internal/broker"
	"sshclient/internal/client"
	"sshclient/internal/monitor"
)

func TestTerminalStatusSharesGrantAndTransportAndStopsOnRevoke(t *testing.T) {
	c, fixture := setup(t)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	r, ticket := request(t, c, approval.Operation{Host: "fixture", Kind: "shell", ServerStatus: true, Client: "session integration"})
	if !strings.Contains(client.Describe(r), "自动读取服务器状态") {
		t.Fatal("approval does not explain the combined scope")
	}
	if _, err := c.Start(ctx, r.ID, ticket, 80, 24); err == nil || fixture.Auths.Load() != 0 {
		t.Fatal("unapproved session reached SSH", err)
	}
	approve(t, c, r)
	stream, err := c.Start(ctx, r.ID, ticket, 80, 24)
	if err != nil {
		t.Fatal(err)
	}
	defer stream.Close()
	status := make(chan string, 1)
	conn := &client.TerminalConn{
		Stream: stream,
		OnStats: func(s *monitor.Stats) {
			value := "stats"
			if s.MemoryTotal == 0 || len(s.Processes) == 0 || len(s.Disks) == 0 {
				value = "invalid stats"
			}
			select {
			case status <- value:
			default:
			}
		},
		OnMonitorError: func(message string) {
			select {
			case status <- message:
			default:
			}
		},
	}
	echo := make(chan string, 4)
	readError := make(chan error, 1)
	go func() {
		buf := make([]byte, 1024)
		for {
			n, err := conn.Read(buf)
			if err != nil {
				readError <- err
				return
			}
			select {
			case echo <- string(buf[:n]):
			case <-ctx.Done():
				return
			}
		}
	}()
	if _, err = conn.Write([]byte("SSH and status share one connection")); err != nil {
		t.Fatal(err)
	}
	var output, state string
	for !strings.Contains(output, "SSH and status share one connection") || state == "" {
		select {
		case outputChunk := <-echo:
			output += outputChunk
		case state = <-status:
		case err := <-readError:
			t.Fatal("status failure terminated the terminal", err)
		case <-ctx.Done():
			t.Fatal("missing automatic status or terminal echo", output, state)
		}
	}
	if runtime.GOOS == "linux" && state != "stats" {
		t.Fatal("real Linux stats unavailable", state)
	}
	if runtime.GOOS != "linux" && !strings.Contains(state, "Linux 状态接口") {
		t.Fatal("unsupported system status not reported", state)
	}
	if fixture.Auths.Load() != 1 {
		t.Fatal("monitor opened another SSH transport", fixture.Auths.Load())
	}
	pending, err := c.Call(ctx, broker.Message{Action: "pending"})
	if err != nil || len(pending.Requests) != 1 {
		t.Fatal("monitor requested separate approval", pending, err)
	}
	if err = c.Decide(ctx, r.ID, false, true, pass); err != nil {
		t.Fatal(err)
	}
	select {
	case <-readError:
	case <-ctx.Done():
		t.Fatal("session continued after revocation")
	}
}
