package broker

import (
	"context"
	"errors"
	"fmt"
	"io"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"golang.org/x/crypto/ssh"

	"sshclient/internal/monitor"
	"sshclient/internal/sshconn"
	"sshclient/internal/testserver"
)

func newSharedMonitorTestServer(t *testing.T) *Server {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	server := &Server{ctx: ctx}
	t.Cleanup(func() {
		cancel()
		server.stopMonitorCollectors()
		server.monitorWG.Wait()
	})
	return server
}

func sharedMonitorUpdate(t *testing.T, sub *monitorSubscription) monitorUpdate {
	t.Helper()
	select {
	case update := <-sub.updates:
		if update.err != nil || update.stats == nil {
			t.Fatalf("expected shared resource sample: %+v", update)
		}
		return update
	case <-time.After(4 * time.Second):
		t.Fatal("shared resource sample did not arrive")
		return monitorUpdate{}
	}
}

func waitMonitorCondition(t *testing.T, condition func() bool) {
	t.Helper()
	limit := time.Now().Add(2 * time.Second)
	for !condition() {
		if time.Now().After(limit) {
			t.Fatal("shared monitor lifecycle did not settle")
		}
		time.Sleep(5 * time.Millisecond)
	}
}

func sharedCounterFixture(n int32) string {
	return fmt.Sprintf("@@CPU\ncpu %d 0 100 %d 0 0 0 0\n@@MEM\nMemTotal: 524288 kB\nMemAvailable: 300000 kB\n", 100+n*10, 800+n*90)
}

func TestSharedMonitorFanoutCacheProviderHandoffAndCleanup(t *testing.T) {
	var counters, inventories, docker atomic.Int32
	fixture := testserver.NewWithExec(t, func(command string) (string, string, int) {
		switch command {
		case monitor.Command:
			return sharedCounterFixture(counters.Add(1)), "", 0
		case monitor.InventoryCommand:
			inventories.Add(1)
			return "@@OS\nShared Linux\n", "", 0
		case monitor.DockerStatsCommand:
			docker.Add(1)
			return "@@DOCKER_STATUS\nmissing\n", "", 0
		}
		t.Error("non-builtin command reached shared telemetry")
		return "", "", 1
	})
	server := newSharedMonitorTestServer(t)
	var clients []*sshconn.Client
	var contexts []context.Context
	var cancels []context.CancelFunc
	for range 3 {
		ctx, cancel := context.WithCancel(context.Background())
		contexts, cancels = append(contexts, ctx), append(cancels, cancel)
		t.Cleanup(cancel)
		client, err := sshconn.Dial(ctx, fixture.Profile())
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { client.Close() })
		clients = append(clients, client)
	}
	profile := fixture.Profile()
	collector, first, err := server.subscribeMonitor(contexts[0], clients[0], profile.ID, profile.Fingerprint)
	if err != nil {
		t.Fatal(err)
	}
	initial := sharedMonitorUpdate(t, first)
	if initial.stats.SourceID == "" {
		t.Fatal("broker did not assign collector identity")
	}
	secondCollector, second, err := server.subscribeMonitor(contexts[1], clients[1], profile.ID, profile.Fingerprint)
	if err != nil {
		t.Fatal(err)
	}
	thirdCollector, slow, err := server.subscribeMonitor(contexts[2], clients[2], profile.ID, profile.Fingerprint)
	if err != nil {
		t.Fatal(err)
	}
	if collector != secondCollector || collector != thirdCollector {
		t.Fatal("equivalent authorized transports created duplicate collectors")
	}
	if sharedMonitorUpdate(t, second).stats != initial.stats || sharedMonitorUpdate(t, slow).stats != initial.stats {
		t.Fatal("late subscribers did not receive the same cached immutable sample")
	}
	waitMonitorCondition(t, func() bool { return inventories.Load() == 1 && docker.Load() == 1 })
	if counters.Load() != 1 {
		t.Fatal("late subscriptions caused extra remote counter work", counters.Load())
	}
	latest := sharedMonitorUpdate(t, second)
	latest = sharedMonitorUpdate(t, second)
	if latest.stats.SourceID != initial.stats.SourceID || latest.stats.Time.Equal(initial.stats.Time) {
		t.Fatal("collector identity or counter timestamp was not stable across fanout")
	}
	if counters.Load() != 3 || inventories.Load() != 1 || docker.Load() != 1 {
		t.Fatal("remote work scaled with subscribers", counters.Load(), inventories.Load(), docker.Load())
	}
	if update := sharedMonitorUpdate(t, slow); update.stats != latest.stats || len(slow.updates) != 0 {
		t.Fatal("slow subscriber buffered a history or blocked fresh counter work")
	}
	// Closing the original provider must not keep its first grant alive or dial
	// another SSH transport. The two independently granted siblings remain.
	clients[0].Close()
	waitMonitorCondition(t, func() bool { return first.ctx.Err() != nil })
	afterClose := sharedMonitorUpdate(t, second)
	if !afterClose.stats.Time.After(latest.stats.Time) || afterClose.stats.SourceID != initial.stats.SourceID {
		t.Fatal("provider close restarted or stopped the collector")
	}
	// A sibling grant being revoked has the same handoff behavior.
	cancels[1]()
	waitMonitorCondition(t, func() bool { return second.ctx.Err() != nil })
	for update := sharedMonitorUpdate(t, slow); ; update = sharedMonitorUpdate(t, slow) {
		if update.stats.Time.After(afterClose.stats.Time) {
			break
		}
	}
	if fixture.Auths.Load() != 3 {
		t.Fatal("handoff opened an unauthorized extra transport", fixture.Auths.Load())
	}
	cancels[2]()
	waitMonitorCondition(t, func() bool {
		server.monitorMu.Lock()
		defer server.monitorMu.Unlock()
		return len(server.monitors) == 0
	})
	collector.mu.Lock()
	retained := collector.latest != nil || collector.inventory != nil || collector.docker != nil
	collector.mu.Unlock()
	if retained || len(slow.updates) != 0 {
		t.Fatal("last unsubscribe retained resource snapshots")
	}
	server.monitorWG.Wait()
	stopped := counters.Load()
	time.Sleep(monitorCounterInterval + 50*time.Millisecond)
	if counters.Load() != stopped {
		t.Fatal("remote sampling survived its final subscriber")
	}
	reconnectCtx, cancelReconnect := context.WithCancel(context.Background())
	defer cancelReconnect()
	reconnect, err := sshconn.Dial(reconnectCtx, profile)
	if err != nil {
		t.Fatal(err)
	}
	defer reconnect.Close()
	replacement, sub, err := server.subscribeMonitor(reconnectCtx, reconnect, profile.ID, profile.Fingerprint)
	if err != nil {
		t.Fatal(err)
	}
	if replacement.sourceID == collector.sourceID {
		t.Fatal("new collector reused an old lifetime identity")
	}
	if update := sharedMonitorUpdate(t, sub); update.stats.SourceID == initial.stats.SourceID {
		t.Fatal("reconnect revived old cached data")
	}
}

func TestSharedMonitorSeparatesSavedProfilesAndSSHAuthorities(t *testing.T) {
	fixture := testserver.NewWithExec(t, func(string) (string, string, int) { return sharedCounterFixture(1), "", 0 })
	otherPeer := testserver.NewWithExec(t, func(string) (string, string, int) { return sharedCounterFixture(1), "", 0 })
	server := newSharedMonitorTestServer(t)
	base := fixture.Profile()
	type identity struct {
		profileID, user, fingerprint string
		peer                         *testserver.Server
	}
	cases := []identity{
		{base.ID, base.User, base.Fingerprint, fixture},
		{"separate-saved-credentials", base.User, base.Fingerprint, fixture},
		{base.ID, "separate-user", base.Fingerprint, fixture},
		{base.ID, base.User, "separate-verified-host-key", fixture},
		{base.ID, base.User, base.Fingerprint, otherPeer},
	}
	sources := make(map[string]bool)
	for _, identity := range cases {
		profile := identity.peer.Profile()
		profile.User = identity.user
		ctx, cancel := context.WithCancel(context.Background())
		t.Cleanup(cancel)
		client, err := sshconn.Dial(ctx, profile)
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { client.Close() })
		collector, _, err := server.subscribeMonitor(ctx, client, identity.profileID, identity.fingerprint)
		if err != nil {
			t.Fatal(err)
		}
		if sources[collector.sourceID] {
			t.Fatal("distinct saved profile or SSH authority reused resource data", identity)
		}
		sources[collector.sourceID] = true
	}
	server.monitorMu.Lock()
	defer server.monitorMu.Unlock()
	if len(server.monitors) != len(cases) {
		t.Fatal("authority isolation lost", len(server.monitors))
	}
}

func TestSharedMonitorSlowDockerDoesNotMultiplyWork(t *testing.T) {
	var counters, docker atomic.Int32
	fixture := testserver.NewWithExec(t, func(command string) (string, string, int) {
		switch command {
		case monitor.Command:
			return sharedCounterFixture(counters.Add(1)), "", 0
		case monitor.InventoryCommand:
			return "@@OS\nShared Linux\n", "", 0
		case monitor.DockerStatsCommand:
			docker.Add(1)
			time.Sleep(3 * time.Second)
			return "@@DOCKER_STATUS\nmissing\n", "", 0
		}
		return "", "", 1
	})
	server := newSharedMonitorTestServer(t)
	var subscribers []*monitorSubscription
	profile := fixture.Profile()
	for range 3 {
		ctx, cancel := context.WithCancel(context.Background())
		t.Cleanup(cancel)
		client, err := sshconn.Dial(ctx, profile)
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { client.Close() })
		_, sub, err := server.subscribeMonitor(ctx, client, profile.ID, profile.Fingerprint)
		if err != nil {
			t.Fatal(err)
		}
		subscribers = append(subscribers, sub)
	}
	initial := sharedMonitorUpdate(t, subscribers[0])
	second := sharedMonitorUpdate(t, subscribers[0])
	if second.stats.Time.Sub(initial.stats.Time) > 2800*time.Millisecond || counters.Load() != 2 || docker.Load() != 1 {
		t.Fatal("slow shared Docker lane delayed or multiplied counter work", counters.Load(), docker.Load())
	}
}

func TestSharedMonitorBlockedProbesCancelAndHandoffWithGrantLifetime(t *testing.T) {
	started, stopped := make(chan string, 12), make(chan string, 12)
	fixture := testserver.NewWithStreamExec(t, func(ctx context.Context, command string, _ io.Reader, _ io.Writer, _ io.Writer) int {
		if _, err := monitorProbeInterval(command); err != nil {
			t.Error("untrusted shared command")
			return 1
		}
		started <- command
		<-ctx.Done()
		stopped <- command
		return 1
	})
	server := newSharedMonitorTestServer(t)
	profile := fixture.Profile()
	subscribe := func() (*monitorCollector, context.CancelFunc) {
		ctx, cancel := context.WithCancel(context.Background())
		t.Cleanup(cancel)
		client, err := sshconn.Dial(ctx, profile)
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { client.Close() })
		collector, _, err := server.subscribeMonitor(ctx, client, profile.ID, profile.Fingerprint)
		if err != nil {
			t.Fatal(err)
		}
		return collector, cancel
	}
	receiveLanes := func(events <-chan string) {
		t.Helper()
		lanes := make(map[string]int)
		for range 3 {
			select {
			case command := <-events:
				lanes[command]++
			case <-time.After(3 * time.Second):
				t.Fatal("blocked shared telemetry lane did not transition")
			}
		}
		for _, command := range []string{monitor.Command, monitor.InventoryCommand, monitor.DockerStatsCommand} {
			if lanes[command] != 1 {
				t.Fatal("shared telemetry transition duplicated or omitted a lane")
			}
		}
	}
	collector, cancelFirst := subscribe()
	receiveLanes(started)
	_, cancelSecond := subscribe()
	_, cancelThird := subscribe()
	// A shared collector cannot use arbitrary commands even with a valid grant.
	if _, err := collector.execute("echo caller-controlled", time.Second); !errors.Is(err, errUnknownMonitorProbe) {
		t.Fatal("shared collector accepted a non-builtin probe", err)
	}
	cancelFirst()
	receiveLanes(stopped)
	receiveLanes(started)
	if fixture.Auths.Load() != 3 {
		t.Fatal("blocked provider handoff dialed another transport", fixture.Auths.Load())
	}
	// Every slow lane is currently blocked remotely. Final unsubscription must
	// cancel all of them now, rather than waiting for their 10/60-second timers.
	cancelSecond()
	cancelThird()
	receiveLanes(stopped)
	drained := make(chan struct{})
	go func() { server.monitorWG.Wait(); close(drained) }()
	select {
	case <-drained:
	case <-time.After(3 * time.Second):
		t.Fatal("collector left blocked probe or transport goroutines after last grant")
	}
	server.monitorMu.Lock()
	active := len(server.monitors)
	server.monitorMu.Unlock()
	if active != 0 {
		t.Fatal("last grant did not release the collector")
	}
}

func TestSharedMonitorCounterFailuresAreRetriedCentrally(t *testing.T) {
	var counters atomic.Int32
	fixture := testserver.NewWithExec(t, func(command string) (string, string, int) {
		if command == monitor.Command {
			counters.Add(1)
			return "", "fixture failure", 1
		}
		return "", "", 0
	})
	server := newSharedMonitorTestServer(t)
	profile := fixture.Profile()
	for range 4 {
		ctx, cancel := context.WithCancel(context.Background())
		t.Cleanup(cancel)
		client, err := sshconn.Dial(ctx, profile)
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { client.Close() })
		_, sub, err := server.subscribeMonitor(ctx, client, profile.ID, profile.Fingerprint)
		if err != nil {
			t.Fatal(err)
		}
		select {
		case update := <-sub.updates:
			if !errors.Is(update.err, errMonitorRead) || update.stats != nil {
				t.Fatal("central failure was not shared")
			}
		case <-time.After(time.Second):
			t.Fatal("cached collector failure did not reach subscriber")
		}
	}
	time.Sleep(100 * time.Millisecond)
	if counters.Load() != 1 {
		t.Fatal("subscribers multiplied failed samples or retry loops", counters.Load())
	}
}

type blockedMonitorCloseConn struct {
	ssh.Conn
	started chan struct{}
	release chan struct{}
	once    sync.Once
}

func (c *blockedMonitorCloseConn) Close() error {
	c.once.Do(func() { close(c.started) })
	<-c.release
	return c.Conn.Close()
}

func TestSharedMonitorExpiredCollectorCannotLeakCacheWhileTransportCloseBlocks(t *testing.T) {
	fixture := testserver.NewWithExec(t, func(command string) (string, string, int) {
		if command == monitor.Command {
			return sharedCounterFixture(1), "", 0
		}
		return "", "", 0
	})
	server := newSharedMonitorTestServer(t)
	profile := fixture.Profile()
	original, err := sshconn.Dial(context.Background(), profile)
	if err != nil {
		t.Fatal(err)
	}
	blocked := &blockedMonitorCloseConn{Conn: original.SSH.Conn, started: make(chan struct{}), release: make(chan struct{})}
	// Use an isolated SSH client wrapper, so no background goroutine races a
	// mutation of the established ssh.Client's embedded connection interface.
	original.SSH = &ssh.Client{Conn: blocked}
	release := sync.OnceFunc(func() { close(blocked.release) })
	defer release()
	t.Cleanup(func() { original.Close() })
	firstCtx, cancelFirst := context.WithCancel(context.Background())
	defer cancelFirst()
	old, first, err := server.subscribeMonitor(firstCtx, original, profile.ID, profile.Fingerprint)
	if err != nil {
		t.Fatal(err)
	}
	cached := sharedMonitorUpdate(t, first)
	cancelFirst()
	select {
	case <-blocked.started:
	case <-time.After(time.Second):
		t.Fatal("transport close fixture did not block expired-grant cleanup")
	}
	// The old callback is deliberately blocked before registry removal. No
	// previously authorized live subscriber remains when this grant arrives.
	replacement, err := sshconn.Dial(context.Background(), profile)
	if err != nil {
		t.Fatal(err)
	}
	defer replacement.Close()
	nextCtx, cancelNext := context.WithCancel(context.Background())
	defer cancelNext()
	next, sub, err := server.subscribeMonitor(nextCtx, replacement, profile.ID, profile.Fingerprint)
	if err != nil {
		t.Fatal(err)
	}
	if old == next || old.sourceID == next.sourceID {
		t.Fatal("new grant inherited an expired collector lifetime")
	}
	old.mu.Lock()
	retained := old.latest != nil || old.inventory != nil || old.docker != nil
	old.mu.Unlock()
	if retained || len(first.updates) != 0 {
		t.Fatal("retired authority retained cached resource snapshots")
	}
	release()
	waitMonitorCondition(t, func() bool {
		old.mu.Lock()
		defer old.mu.Unlock()
		return len(old.subscribers) == 0
	})
	server.monitorMu.Lock()
	current := server.monitors[next.target]
	server.monitorMu.Unlock()
	if current != next {
		t.Fatal("late old cleanup removed the replacement collector")
	}
	if update := sharedMonitorUpdate(t, sub); update.stats.SourceID == cached.stats.SourceID {
		t.Fatal("late old cleanup or cached update crossed the revoked authority boundary")
	}
}

func TestSharedMonitorCancelledRoundsKeepMinimumGlobalProbeSpacing(t *testing.T) {
	starts := make(chan time.Time, 3)
	stops := make(chan struct{}, 3)
	var commands atomic.Int32
	fixture := testserver.NewWithStreamExec(t, func(ctx context.Context, command string, _ io.Reader, out io.Writer, _ io.Writer) int {
		if command == monitor.Command {
			commands.Add(1)
			starts <- time.Now()
			<-ctx.Done()
			stops <- struct{}{}
			return 1
		}
		io.WriteString(out, "@@DOCKER_STATUS\nmissing\n")
		return 0
	})
	server := newSharedMonitorTestServer(t)
	profile := fixture.Profile()
	var previous time.Time
	for range 3 {
		ctx, cancel := context.WithCancel(context.Background())
		t.Cleanup(cancel)
		client, err := sshconn.Dial(ctx, profile)
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { client.Close() })
		_, _, err = server.subscribeMonitor(ctx, client, profile.ID, profile.Fingerprint)
		if err != nil {
			t.Fatal(err)
		}
		select {
		case started := <-starts:
			if !previous.IsZero() && started.Sub(previous) < monitorCounterInterval {
				t.Fatal("cancel/reconnect bypassed minimum remote probe spacing", started.Sub(previous))
			}
			previous = started
		case <-time.After(3 * time.Second):
			t.Fatal("canceled collector failed to hand off within its bounded cooldown")
		}
		cancel()
		select {
		case <-stops:
		// The whole suite runs its packages in parallel, so a canceled goroutine
		// can take longer than a quiet machine would need to observe it.
		case <-time.After(3 * time.Second):
			t.Fatal("canceled counter probe survived its grant")
		}
	}
	server.monitorWG.Wait()
	if commands.Load() != 3 {
		t.Fatal("canceled rounds duplicated remote probes", commands.Load())
	}
}

type blockedMonitorOpenConn struct {
	ssh.Conn
	opened    atomic.Int32
	released  atomic.Int32
	closeOnce sync.Once
	closed    chan struct{}
}

func (c *blockedMonitorOpenConn) OpenChannel(kind string, payload []byte) (ssh.Channel, <-chan *ssh.Request, error) {
	c.opened.Add(1)
	<-c.closed
	c.released.Add(1)
	return nil, nil, io.EOF
}

func (c *blockedMonitorOpenConn) Close() error {
	c.closeOnce.Do(func() { close(c.closed) })
	return c.Conn.Close()
}

func TestSharedMonitorGrantCancellationUnblocksSSHChannelOpen(t *testing.T) {
	fixture := testserver.NewWithExec(t, func(command string) (string, string, int) {
		if command == monitor.Command {
			return sharedCounterFixture(1), "", 0
		}
		return "@@DOCKER_STATUS\nmissing\n", "", 0
	})
	server := newSharedMonitorTestServer(t)
	profile := fixture.Profile()
	firstCtx, cancelFirst := context.WithCancel(context.Background())
	defer cancelFirst()
	first, err := sshconn.Dial(firstCtx, profile)
	if err != nil {
		t.Fatal(err)
	}
	defer first.Close()
	blocked := &blockedMonitorOpenConn{Conn: first.SSH.Conn, closed: make(chan struct{})}
	first.SSH = &ssh.Client{Conn: blocked}
	collector, _, err := server.subscribeMonitor(firstCtx, first, profile.ID, profile.Fingerprint)
	if err != nil {
		t.Fatal(err)
	}
	waitMonitorCondition(t, func() bool { return blocked.opened.Load() == 3 })
	var observer *monitorSubscription
	for range 2 {
		ctx, cancel := context.WithCancel(context.Background())
		t.Cleanup(cancel)
		client, err := sshconn.Dial(ctx, profile)
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { client.Close() })
		_, observer, err = server.subscribeMonitor(ctx, client, profile.ID, profile.Fingerprint)
		if err != nil {
			t.Fatal(err)
		}
	}
	cancelFirst()
	waitMonitorCondition(t, func() bool { return blocked.released.Load() == 3 })
	update := sharedMonitorUpdate(t, observer)
	if update.stats.SourceID != collector.sourceID || fixture.Auths.Load() != 3 {
		t.Fatal("blocked channel-open handoff replaced the collector or reauthenticated")
	}
}

func TestMonitorBudgetCancelledPacingWaiterPreservesNext(t *testing.T) {
	budget := newMonitorBudget()
	next := time.Now().Add(time.Minute)
	budget.next = next
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Millisecond)
	defer cancel()
	_, err := budget.run(ctx, monitorInventoryInterval, func() (sshconn.Result, error) {
		t.Error("canceled pacing waiter started a remote probe")
		return sshconn.Result{}, nil
	})
	if !errors.Is(err, context.DeadlineExceeded) || !budget.next.Equal(next) {
		t.Fatal("canceled pacing waiter changed the previous rate limit", err, budget.next.Sub(next))
	}
}

// A hidden panel must stop the remote work, and showing it again must resume it
// without dropping the session or reconnecting.
func TestSharedMonitorProbesPauseWhileNothingShowsThem(t *testing.T) {
	var counters atomic.Int32
	fixture := testserver.NewWithExec(t, func(command string) (string, string, int) {
		switch command {
		case monitor.Command:
			return sharedCounterFixture(counters.Add(1)), "", 0
		case monitor.InventoryCommand:
			return "@@OS\nShared Linux\n", "", 0
		case monitor.DockerStatsCommand:
			return "@@DOCKER_STATUS\nmissing\n", "", 0
		}
		return "", "", 1
	})
	server := newSharedMonitorTestServer(t)
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	client, err := sshconn.Dial(ctx, fixture.Profile())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { client.Close() })
	collector, sub, err := server.subscribeMonitor(ctx, client, fixture.Profile().ID, fixture.Profile().Fingerprint)
	if err != nil {
		t.Fatal(err)
	}
	sharedMonitorUpdate(t, sub)
	sharedMonitorUpdate(t, sub)

	server.activateMonitor(collector, sub, false)
	settled := counters.Load()
	time.Sleep(monitorCounterInterval + 600*time.Millisecond)
	if got := counters.Load(); got != settled {
		t.Fatalf("a hidden panel kept probing: %d -> %d rounds", settled, got)
	}

	server.activateMonitor(collector, sub, true)
	deadline := time.Now().Add(3 * time.Second)
	for counters.Load() == settled {
		if time.Now().After(deadline) {
			t.Fatal("showing the panel again did not resume probing")
		}
		time.Sleep(10 * time.Millisecond)
	}
}

// One terminal may hide status while another on the same server still shows it:
// the shared collector must keep the one round that is displayed.
func TestSharedMonitorKeepsProbingWhileAnotherSubscriberShowsIt(t *testing.T) {
	var counters atomic.Int32
	fixture := testserver.NewWithExec(t, func(command string) (string, string, int) {
		switch command {
		case monitor.Command:
			return sharedCounterFixture(counters.Add(1)), "", 0
		case monitor.InventoryCommand:
			return "@@OS\nShared Linux\n", "", 0
		case monitor.DockerStatsCommand:
			return "@@DOCKER_STATUS\nmissing\n", "", 0
		}
		return "", "", 1
	})
	server := newSharedMonitorTestServer(t)
	var subs []*monitorSubscription
	var collectors []*monitorCollector
	for range 2 {
		ctx, cancel := context.WithCancel(context.Background())
		t.Cleanup(cancel)
		client, err := sshconn.Dial(ctx, fixture.Profile())
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { client.Close() })
		collector, sub, err := server.subscribeMonitor(ctx, client, fixture.Profile().ID, fixture.Profile().Fingerprint)
		if err != nil {
			t.Fatal(err)
		}
		collectors, subs = append(collectors, collector), append(subs, sub)
	}
	sharedMonitorUpdate(t, subs[1])
	server.activateMonitor(collectors[1], subs[1], false)

	before := counters.Load()
	select {
	case <-subs[0].updates:
	case <-time.After(3 * time.Second):
		t.Fatal("the visible subscriber stopped receiving samples")
	}
	if counters.Load() == before && before == 0 {
		t.Fatal("no round ran with one subscriber still visible")
	}
}
