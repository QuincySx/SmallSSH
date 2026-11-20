package broker

import (
	"context"
	"errors"
	"net"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"sshclient/internal/localipc"
	"sshclient/internal/monitor"
	"sshclient/internal/sshconn"
	"sshclient/internal/testserver"
)

const windowsCounterFixture = `@@CPU
40|25|15
@@CORES
2
@@CORE_USAGE
0|20|12|8
1|60|38|22
@@MEM
8192
4096
@@PROCESS_COUNT
137
@@PROCESS
812|explorer|204800|42
@@END
`

const windowsInventoryFixture = `@@OS
Microsoft Windows Server
@@HOST
windows-fixture
@@CORES
2
@@MANAGER
Windows 服务管理器
@@END
`

func windowsTelemetryFixture(t *testing.T) (*testserver.Server, *atomic.Int32, *atomic.Int32) {
	t.Helper()
	counters, inventories := &atomic.Int32{}, &atomic.Int32{}
	fixture := testserver.NewWithExec(t, func(command string) (string, string, int) {
		switch command {
		case monitor.Command:
			return "", "Windows has no /proc", 69
		case monitor.InventoryCommand, monitor.DockerStatsCommand:
			return "", "POSIX shell unavailable", 1
		case monitor.PlatformProbe:
			return "@@PROBE Windows_NT $(uname -s)\n", "", 0
		case monitor.WindowsStatsCommand:
			counters.Add(1)
			return windowsCounterFixture, "", 0
		case monitor.WindowsInventoryCommand:
			inventories.Add(1)
			return windowsInventoryFixture, "", 0
		default:
			t.Error("non-builtin command reached Windows telemetry", command)
			return "", "unsupported", 1
		}
	})
	return fixture, counters, inventories
}

func TestSharedWindowsMonitorUsesRealCountersAndCancels(t *testing.T) {
	fixture, counters, inventories := windowsTelemetryFixture(t)
	server := newSharedMonitorTestServer(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	connection, err := sshconn.Dial(ctx, fixture.Profile())
	if err != nil {
		t.Fatal(err)
	}
	defer connection.Close()
	p := fixture.Profile()
	collector, sub, err := server.subscribeMonitor(ctx, connection, p.ID, p.Fingerprint)
	if err != nil {
		t.Fatal(err)
	}
	first := sharedMonitorUpdate(t, sub)
	if first.stats.CPU != 40 || first.stats.ProcessCount != 137 || first.stats.RunningProcesses != -1 || len(first.stats.Cores) != 2 || first.stats.Cores[0].Used != 20 || first.stats.Cores[1].Used != 60 {
		t.Fatal("Windows counters were not sampled through the authorized collector", first.stats)
	}
	waitMonitorCondition(t, func() bool {
		collector.mu.Lock()
		defer collector.mu.Unlock()
		return collector.inventoryWindows && collector.inventory != nil && strings.Contains(collector.inventory.OS, "Windows")
	})
	if counters.Load() != 1 || inventories.Load() != 1 {
		t.Fatal("Windows probe cadence or inventory switch duplicated work", counters.Load(), inventories.Load())
	}
	cancel()
	waitMonitorCondition(t, func() bool {
		server.monitorMu.Lock()
		defer server.monitorMu.Unlock()
		return len(server.monitors) == 0
	})
	select {
	case <-collector.probes.Done():
	case <-time.After(3 * time.Second):
		t.Fatal("revoked Windows collector kept sampling")
	}
}

func TestStandaloneWindowsMonitorSwitchesFixedProbes(t *testing.T) {
	fixture, _, _ := windowsTelemetryFixture(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	connection, err := sshconn.Dial(ctx, fixture.Profile())
	if err != nil {
		t.Fatal(err)
	}
	defer connection.Close()
	left, right := net.Pipe()
	defer left.Close()
	defer right.Close()
	done := make(chan error, 1)
	go func() { done <- streamMonitor(ctx, connection, &localipc.Wire{Conn: left}) }()
	right.SetReadDeadline(time.Now().Add(5 * time.Second))
	var message Message
	if err := (&localipc.Wire{Conn: right}).Receive(&message); err != nil {
		t.Fatal(err)
	}
	if message.Stats == nil || message.Stats.ProcessCount != 137 || len(message.Stats.Cores) != 2 || message.Stats.Cores[1].Used != 60 || message.Stats.DockerStats != nil {
		t.Fatal("standalone stream did not switch to measured Windows stats", message)
	}
	cancel()
	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Fatal(err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("standalone Windows sampling ignored cancellation")
	}
}

func TestWindowsMonitorProbesKeepAllowlistAndBudget(t *testing.T) {
	var calls atomic.Int32
	fixture := testserver.NewWithExec(t, func(string) (string, string, int) {
		calls.Add(1)
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
	for _, command := range []string{monitor.PlatformProbe, monitor.WindowsProbeCommand, monitor.WindowsStatsCommand, monitor.WindowsInventoryCommand} {
		if _, err := execute(ctx, command, time.Second); err != nil {
			t.Fatal(err)
		}
	}
	if calls.Load() != 4 {
		t.Fatal(calls.Load())
	}
	limited, stop := context.WithTimeout(ctx, 40*time.Millisecond)
	defer stop()
	if _, err := execute(limited, monitor.WindowsStatsCommand, time.Second); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatal("Windows counters bypassed the shared pacing budget", err)
	}
	for _, command := range []string{" " + monitor.WindowsStatsCommand, monitor.WindowsStatsCommand + "\necho caller-controlled"} {
		if _, err := execute(ctx, command, time.Second); !errors.Is(err, errUnknownMonitorProbe) {
			t.Fatal("altered Windows probe entered telemetry path", err)
		}
	}
	if calls.Load() != 4 {
		t.Fatal("unapproved or paced command reached SSH", calls.Load())
	}
	if interval, err := monitorProbeInterval(monitor.WindowsInventoryCommand); err != nil || interval != 5*time.Minute {
		t.Fatal("Windows inventory cadence changed", interval, err)
	}
}

func TestWindowsPowerShellDetectionIsOnceAndCancellationSafe(t *testing.T) {
	platform := newMonitorPlatform()
	var calls int
	execute := func(_ context.Context, command string, _ time.Duration) (sshconn.Result, error) {
		calls++
		switch command {
		case monitor.PlatformProbe:
			return sshconn.Result{Stdout: "@@PROBE %OS%\n"}, nil
		case monitor.WindowsProbeCommand:
			return sshconn.Result{Stdout: "5\n"}, nil
		}
		t.Fatal("unexpected detection command", command)
		return sshconn.Result{}, nil
	}
	if !platform.detectWindows(context.Background(), execute) || !platform.detectWindows(context.Background(), execute) || calls != 2 {
		t.Fatal("PowerShell shell detection failed or repeated probes", calls)
	}
	canceled, cancel := context.WithCancel(context.Background())
	cancel()
	if newMonitorPlatform().detectWindows(canceled, execute) || calls != 2 {
		t.Fatal("canceled detection performed remote work")
	}
}

func TestWindowsPlatformTemporaryFailureCanBeRetried(t *testing.T) {
	platform := newMonitorPlatform()
	calls := 0
	execute := func(_ context.Context, command string, _ time.Duration) (sshconn.Result, error) {
		if command != monitor.PlatformProbe {
			t.Fatal("unexpected retry command", command)
		}
		calls++
		if calls == 1 {
			return sshconn.Result{}, context.DeadlineExceeded
		}
		return sshconn.Result{Stdout: "@@PROBE Windows_NT\n"}, nil
	}
	if platform.detectWindows(context.Background(), execute) || platform.knownPOSIX() {
		t.Fatal("temporary failure committed a platform")
	}
	if !platform.detectWindows(context.Background(), execute) || calls != 2 {
		t.Fatal("temporary failure was cached permanently", calls)
	}
	if !platform.detectWindows(context.Background(), execute) || calls != 2 {
		t.Fatal("confirmed Windows was not cached", calls)
	}
}

func TestSharedWindowsMonitorRetriesUnknownPlatform(t *testing.T) {
	var probes atomic.Int32
	fixture := testserver.NewWithExec(t, func(command string) (string, string, int) {
		switch command {
		case monitor.Command:
			return "", "no /proc", 69
		case monitor.InventoryCommand, monitor.DockerStatsCommand:
			return "", "no POSIX shell", 1
		case monitor.PlatformProbe:
			if probes.Add(1) == 1 {
				return "", "temporary probe failure", 1
			}
			return "@@PROBE Windows_NT\n", "", 0
		case monitor.WindowsProbeCommand:
			return "", "temporary PowerShell failure", 1
		case monitor.WindowsStatsCommand:
			return windowsCounterFixture, "", 0
		case monitor.WindowsInventoryCommand:
			return windowsInventoryFixture, "", 0
		default:
			t.Error("unexpected retry telemetry command", command)
			return "", "unsupported", 1
		}
	})
	server := newSharedMonitorTestServer(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	connection, err := sshconn.Dial(ctx, fixture.Profile())
	if err != nil {
		t.Fatal(err)
	}
	defer connection.Close()
	p := fixture.Profile()
	collector, sub, err := server.subscribeMonitor(ctx, connection, p.ID, p.Fingerprint)
	if err != nil {
		t.Fatal(err)
	}
	select {
	case update := <-sub.updates:
		if update.err == nil || errors.Is(update.err, errMonitorUnsupported) {
			t.Fatal("unknown platform was stopped as permanently unsupported", update)
		}
	case <-time.After(4 * time.Second):
		t.Fatal("temporary failure was not reported")
	}
	if collector.probes.Err() != nil {
		t.Fatal("temporary probe failure stopped the collector")
	}
	select {
	case update := <-sub.updates:
		if update.err != nil || update.stats == nil || update.stats.ProcessCount != 137 || update.stats.SampleIntervalSeconds != 10 {
			t.Fatal("later Windows probe did not recover", update)
		}
	case <-time.After(15 * time.Second):
		t.Fatal("unknown platform was never retried")
	}
	if probes.Load() != 2 {
		t.Fatal("platform retry ignored cache or budget", probes.Load())
	}
}

func TestWindowsPlatformRecognizesUnsupportedPOSIXWithoutRetrying(t *testing.T) {
	platform := newMonitorPlatform()
	calls := 0
	execute := func(_ context.Context, command string, _ time.Duration) (sshconn.Result, error) {
		calls++
		if command != monitor.PlatformProbe {
			t.Fatal("POSIX platform triggered a PowerShell probe")
		}
		return sshconn.Result{Stdout: "@@PROBE %OS% Darwin\n"}, nil
	}
	if platform.detectWindows(context.Background(), execute) || !platform.knownPOSIX() {
		t.Fatal("known unsupported POSIX platform was left unknown")
	}
	if platform.detectWindows(context.Background(), execute) || calls != 1 {
		t.Fatal("definitive POSIX result was probed again", calls)
	}
}
