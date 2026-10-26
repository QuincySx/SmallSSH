package broker

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"sshclient/internal/monitor"
	"sshclient/internal/sshconn"
	"sshclient/internal/testserver"
)

func TestMonitorBudgetSerializesAndPacesConcurrentCallers(t *testing.T) {
	b := newMonitorBudget()
	const gap = 12 * time.Millisecond
	var active, calls atomic.Int32
	var lastEnd time.Time // the budget must serialize all access
	var wg sync.WaitGroup
	for range 8 {
		wg.Go(func() {
			_, err := b.run(context.Background(), gap, func() (sshconn.Result, error) {
				if active.Add(1) != 1 {
					t.Error("overlapping sampling")
				}
				if !lastEnd.IsZero() && time.Since(lastEnd) < gap {
					t.Error("sampling exceeded the shared rate limit")
				}
				calls.Add(1)
				active.Add(-1)
				lastEnd = time.Now()
				return sshconn.Result{}, nil
			})
			if err != nil {
				t.Error(err)
			}
		})
	}
	wg.Wait()
	if calls.Load() != 8 {
		t.Fatal(calls.Load())
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := b.run(ctx, gap, func() (sshconn.Result, error) {
		t.Error("cancelled waiter performed remote work")
		return sshconn.Result{}, nil
	}); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
}

func TestMonitorBudgetSharedAcrossSSHConnectionsAndSeparatesProbeKinds(t *testing.T) {
	var commands atomic.Int32
	f := testserver.NewWithExec(t, func(string) (string, string, int) {
		commands.Add(1)
		return "fixture", "", 0
	})
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	one, err := sshconn.Dial(ctx, f.Profile())
	if err != nil {
		t.Fatal(err)
	}
	defer one.Close()
	two, err := sshconn.Dial(ctx, f.Profile())
	if err != nil {
		t.Fatal(err)
	}
	defer two.Close()
	s := &Server{}
	a, b := s.monitorExecutor(one, f.Profile().Fingerprint), s.monitorExecutor(two, f.Profile().Fingerprint)
	if _, err := a(ctx, monitor.Command, time.Second); err != nil {
		t.Fatal(err)
	}
	limited, stop := context.WithTimeout(ctx, 40*time.Millisecond)
	defer stop()
	if _, err := b(limited, monitor.Command, time.Second); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatal("second connection bypassed or ignored budget", err)
	}
	if commands.Load() != 1 {
		t.Fatal("duplicate host sampling reached SSH", commands.Load())
	}
	// Slow inventory and Docker have their own lane, so neither can delay
	// the host's basic counters; a cancelled host waiter consumes no work.
	for _, command := range []string{monitor.InventoryCommand, monitor.DockerStatsCommand} {
		if _, err := b(ctx, command, time.Second); err != nil {
			t.Fatal(err)
		}
	}
	if commands.Load() != 3 {
		t.Fatal(commands.Load())
	}
	// Equal addresses with different users must never share an authority key.
	p := f.Profile()
	p.User = "another-user"
	other, err := sshconn.Dial(ctx, p)
	if err != nil {
		t.Fatal(err)
	}
	defer other.Close()
	if _, err := s.monitorExecutor(other, p.Fingerprint)(ctx, monitor.Command, time.Second); err != nil {
		t.Fatal(err)
	}
	if commands.Load() != 4 {
		t.Fatal("independent SSH user was incorrectly throttled", commands.Load())
	}
}

func TestMonitorExecutorRejectsNonBuiltinProbesBeforeRemoteExecution(t *testing.T) {
	var commands atomic.Int32
	fixture := testserver.NewWithExec(t, func(string) (string, string, int) {
		commands.Add(1)
		return "fixture", "", 0
	})
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	connection, err := sshconn.Dial(ctx, fixture.Profile())
	if err != nil {
		t.Fatal(err)
	}
	defer connection.Close()
	server := &Server{}
	execute := server.monitorExecutor(connection, fixture.Profile().Fingerprint)
	for index, command := range []string{
		"", "uname -a", " " + monitor.InventoryCommand,
		monitor.Command + "\nprintf appended-command\n",
		monitor.DockerStatsCommand + "; printf appended-command",
	} {
		if _, err := execute(ctx, command, time.Second); !errors.Is(err, errUnknownMonitorProbe) {
			t.Errorf("untrusted probe %d was not rejected: %v", index, err)
		}
	}
	if commands.Load() != 0 {
		t.Fatal("an untrusted probe reached SSH", commands.Load())
	}
	server.monitorBudgets.Range(func(_, _ any) bool {
		t.Error("an untrusted probe created a sampling budget")
		return true
	})
	// Rejecting untrusted input does not consume the real telemetry lane.
	if _, err := execute(ctx, monitor.Command, time.Second); err != nil {
		t.Fatal("valid fixed probe was affected by earlier rejection", err)
	}
	if commands.Load() != 1 {
		t.Fatal("fixed probe did not execute exactly once", commands.Load())
	}
}
