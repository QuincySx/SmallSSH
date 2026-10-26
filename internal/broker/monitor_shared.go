package broker

import (
	"context"
	"sync"
	"time"

	"sshclient/internal/localipc"
	"sshclient/internal/monitor"
	"sshclient/internal/sshconn"
	"sshclient/internal/vault"
)

// The connected peer and verified host key, rather than a profile alias or
// remote-reported hostname, identify the peer. The saved profile also separates
// credentials that may have different forced commands for the same SSH user.
type monitorTarget struct {
	profileID, user, peer, fingerprint string
}

type monitorUpdate struct {
	stats *monitor.Stats
	err   error
}

type monitorSubscription struct {
	ctx     context.Context
	cancel  context.CancelFunc
	client  *sshconn.Client
	updates chan monitorUpdate
}

type monitorCollector struct {
	server       *Server
	target       monitorTarget
	sourceID     string
	ctx          context.Context
	cancel       context.CancelFunc
	probes       context.Context
	stopProbes   context.CancelFunc
	mu           sync.Mutex
	subscribers  []*monitorSubscription
	nextProvider int
	latest       *monitorUpdate
	inventory    *monitor.Inventory
	docker       *monitor.DockerSnapshot
}

// Called only after Approvals.Start and SSH host verification, for an approved
// monitor operation or a terminal whose immutable grant includes ServerStatus.
func (s *Server) subscribeMonitor(ctx context.Context, client *sshconn.Client, profileID, fingerprint string) (*monitorCollector, *monitorSubscription, error) {
	if err := ctx.Err(); err != nil {
		return nil, nil, err
	}
	target := monitorTarget{profileID, client.SSH.User(), client.SSH.RemoteAddr().String(), fingerprint}
	s.monitorMu.Lock()
	defer s.monitorMu.Unlock()
	if s.monitorClosed {
		return nil, nil, context.Canceled
	}
	if s.monitors == nil {
		s.monitors = make(map[monitorTarget]*monitorCollector)
	}
	collector := s.monitors[target]
	if collector != nil {
		collector.mu.Lock()
		live := false
		for _, sub := range collector.subscribers {
			if sub.ctx.Err() == nil {
				live = true
				break
			}
		}
		// Cancellation revokes access synchronously, even if its transport
		// close or asynchronous unsubscribe has not completed yet.
		if !live {
			collector.stopProbes()
			collector.cancel()
			collector.latest, collector.inventory, collector.docker = nil, nil, nil
			for _, sub := range collector.subscribers {
				select {
				case <-sub.updates:
				default:
				}
			}
		}
		collector.mu.Unlock()
		if !live {
			collector = nil
		}
	}
	fresh := collector == nil
	if fresh {
		parent := s.ctx
		if parent == nil {
			parent = context.Background()
		}
		lifetime, cancel := context.WithCancel(parent)
		probes, stopProbes := context.WithCancel(lifetime)
		collector = &monitorCollector{server: s, target: target, sourceID: vault.ShortID(), ctx: lifetime, cancel: cancel, probes: probes, stopProbes: stopProbes}
		s.monitors[target] = collector
	}
	lifetime, cancel := context.WithCancel(ctx)
	sub := &monitorSubscription{ctx: lifetime, cancel: cancel, client: client, updates: make(chan monitorUpdate, 1)}
	collector.mu.Lock()
	collector.subscribers = append(collector.subscribers, sub)
	if collector.latest != nil {
		sub.updates <- *collector.latest
	}
	collector.mu.Unlock()
	// A departing grant may be opening an SSH channel, before Exec can install
	// its session cancellation hook. Close only that grant's own transport.
	s.monitorWG.Add(1)
	context.AfterFunc(lifetime, func() {
		defer s.monitorWG.Done()
		client.Close()
		s.unsubscribeMonitor(collector, sub)
	})
	s.monitorWG.Go(func() { client.SSH.Wait(); cancel() })
	if fresh {
		s.monitorWG.Go(collector.runCounters)
		s.monitorWG.Go(collector.runInventory)
		s.monitorWG.Go(collector.runDocker)
	}
	return collector, sub, nil
}

func (s *Server) unsubscribeMonitor(collector *monitorCollector, sub *monitorSubscription) {
	sub.cancel()
	s.monitorMu.Lock()
	defer s.monitorMu.Unlock()
	collector.mu.Lock()
	defer collector.mu.Unlock()
	for i, candidate := range collector.subscribers {
		if candidate == sub {
			copy(collector.subscribers[i:], collector.subscribers[i+1:])
			collector.subscribers[len(collector.subscribers)-1] = nil
			collector.subscribers = collector.subscribers[:len(collector.subscribers)-1]
			// Release any cached sample held by a reader that has gone away.
			select {
			case <-sub.updates:
			default:
			}
			break
		}
	}
	if len(collector.subscribers) == 0 {
		if s.monitors[collector.target] == collector {
			delete(s.monitors, collector.target)
		}
		collector.latest, collector.inventory, collector.docker = nil, nil, nil
		collector.stopProbes()
		collector.cancel()
	}
}

func (s *Server) stopMonitorCollectors() {
	s.monitorMu.Lock()
	defer s.monitorMu.Unlock()
	s.monitorClosed = true
	for target, collector := range s.monitors {
		collector.stopProbes()
		collector.cancel()
		collector.mu.Lock()
		for _, sub := range collector.subscribers {
			sub.cancel()
		}
		collector.latest, collector.inventory, collector.docker = nil, nil, nil
		collector.mu.Unlock()
		delete(s.monitors, target)
	}
}

func (s *Server) streamSharedMonitor(ctx context.Context, client *sshconn.Client, profileID, fingerprint string, w *localipc.Wire) error {
	collector, sub, err := s.subscribeMonitor(ctx, client, profileID, fingerprint)
	if err != nil {
		return err
	}
	defer s.unsubscribeMonitor(collector, sub)
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-sub.ctx.Done():
			return sub.ctx.Err()
		case <-collector.ctx.Done():
			return collector.ctx.Err()
		case update := <-sub.updates:
			if err := sub.ctx.Err(); err != nil {
				return err
			}
			message := Message{Action: "stats", Stats: update.stats}
			if update.err != nil {
				detail := "服务器状态暂时无法更新，稍后自动重试"
				if update.err == errMonitorUnsupported {
					detail = update.err.Error()
				}
				message = Message{Action: "monitor-error", MonitorError: detail}
			}
			if err := w.Send(message); err != nil {
				return err
			}
		}
	}
}

func (c *monitorCollector) provider() *monitorSubscription {
	c.mu.Lock()
	defer c.mu.Unlock()
	count := len(c.subscribers)
	for range count {
		index := c.nextProvider % count
		c.nextProvider = (index + 1) % count
		sub := c.subscribers[index]
		if sub.ctx.Err() == nil {
			return sub
		}
	}
	return nil
}

func (c *monitorCollector) execute(command string, timeout time.Duration) (sshconn.Result, error) {
	// Validate before acquiring a provider or consuming the remote-work budget.
	if _, err := monitorProbeInterval(command); err != nil {
		return sshconn.Result{}, err
	}
	for c.probes.Err() == nil {
		provider := c.provider()
		if provider == nil {
			return sshconn.Result{}, context.Canceled
		}
		ctx, cancel := context.WithCancel(provider.ctx)
		stop := context.AfterFunc(c.probes, cancel)
		result, err := c.server.monitorExecutor(provider.client, c.target.fingerprint)(ctx, command, timeout)
		stop()
		cancel()
		if provider.ctx.Err() != nil && c.probes.Err() == nil {
			continue
		}
		return result, err
	}
	return sshconn.Result{}, c.probes.Err()
}

func (c *monitorCollector) publish(update monitorUpdate) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.ctx.Err() != nil {
		return
	}
	c.latest = &update
	for _, sub := range c.subscribers {
		if sub.ctx.Err() != nil {
			continue
		}
		// A slow local reader can lose intermediate samples, never remote work.
		select {
		case <-sub.updates:
		default:
		}
		select {
		case sub.updates <- update:
		default:
		}
	}
}

func monitorDelay(ctx context.Context, delay time.Duration) bool {
	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return false
	case <-timer.C:
		return true
	}
}

func (c *monitorCollector) runCounters() {
	var previous *monitor.Stats
	retryDelay := 10 * time.Second
	for c.probes.Err() == nil {
		result, err := c.execute(monitor.Command, 6*time.Second)
		if c.probes.Err() != nil {
			return
		}
		if err == nil && result.ExitCode == 69 {
			c.publish(monitorUpdate{err: errMonitorUnsupported})
			c.stopProbes()
			return
		}
		if err == nil && result.ExitCode != 0 {
			err = errMonitorRead
		}
		var stats monitor.Stats
		if err == nil {
			stats, err = monitor.Parse(result.Stdout, previous)
		}
		if err != nil {
			c.publish(monitorUpdate{err: err})
			if !monitorDelay(c.probes, retryDelay) {
				return
			}
			retryDelay = min(2*retryDelay, time.Minute)
			continue
		}
		stats.SourceID = c.sourceID
		c.mu.Lock()
		stats.Inventory, stats.DockerStats = c.inventory, c.docker
		c.mu.Unlock()
		c.publish(monitorUpdate{stats: &stats})
		previous = &stats
		retryDelay = 10 * time.Second
		if !monitorDelay(c.probes, monitorCounterInterval) {
			return
		}
	}
}

func (c *monitorCollector) runInventory() {
	for c.probes.Err() == nil {
		started := time.Now().UTC()
		result, err := c.execute(monitor.InventoryCommand, 6*time.Second)
		if c.probes.Err() != nil {
			return
		}
		info := monitor.ParseInventory(result.Stdout)
		info.Time = started
		if err != nil || result.ExitCode != 0 {
			info.ServiceNote = "系统资料读取不完整；实时资源更新继续"
			if info.Docker != nil && info.Docker.Status == "checking" {
				info.Docker.Status = "unavailable"
				info.Docker.Note = "Docker 读取超时或失败，稍后自动重试"
			}
		}
		c.mu.Lock()
		if c.ctx.Err() == nil {
			c.inventory = &info
		}
		c.mu.Unlock()
		if !monitorDelay(c.probes, monitorInventoryInterval) {
			return
		}
	}
}

func (c *monitorCollector) runDocker() {
	for c.probes.Err() == nil {
		result, err := c.execute(monitor.DockerStatsCommand, 4*time.Second)
		if c.probes.Err() != nil {
			return
		}
		snapshot := monitor.ParseDockerSnapshot(result.Stdout)
		if err != nil || result.ExitCode != 0 {
			snapshot.Status = "unavailable"
			snapshot.Note = "容器资源读取超时或失败"
			snapshot.Rows = nil
		}
		c.mu.Lock()
		if c.ctx.Err() == nil {
			c.docker = &snapshot
		}
		c.mu.Unlock()
		delay := monitorDockerInterval
		if snapshot.Status != "ready" {
			delay = time.Minute
		}
		if !monitorDelay(c.probes, delay) {
			return
		}
	}
}
