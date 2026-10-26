package broker

import (
	"context"
	"errors"
	"time"

	"sshclient/internal/monitor"
	"sshclient/internal/sshconn"
)

const (
	monitorCounterInterval   = 2 * time.Second
	monitorDockerInterval    = 10 * time.Second
	monitorInventoryInterval = time.Minute
)

// All authorized sessions to the same SSH user, peer address and host key
// share a budget. Results and credentials are never shared or cached here.
type monitorBudget struct {
	slot chan struct{}
	next time.Time // protected by slot
}

func newMonitorBudget() *monitorBudget { return &monitorBudget{slot: make(chan struct{}, 1)} }

func (b *monitorBudget) run(ctx context.Context, gap time.Duration, fn func() (sshconn.Result, error)) (sshconn.Result, error) {
	select {
	case b.slot <- struct{}{}:
	case <-ctx.Done():
		return sshconn.Result{}, ctx.Err()
	}
	defer func() { <-b.slot }()
	if delay := time.Until(b.next); delay > 0 {
		timer := time.NewTimer(delay)
		defer timer.Stop()
		select {
		case <-timer.C:
		case <-ctx.Done():
			return sshconn.Result{}, ctx.Err()
		}
	}
	if err := ctx.Err(); err != nil {
		return sshconn.Result{}, err
	}
	result, err := fn()
	// A revoked provider produced no usable sample. Its successor can finish
	// this round without waiting an inventory minute, while repeated grant
	// cancellation still cannot bypass the target's minimum probe spacing.
	cooldown := gap
	if ctx.Err() != nil {
		cooldown = min(gap, monitorCounterInterval)
	}
	b.next = time.Now().Add(cooldown)
	return result, err
}

type monitorExecutor func(context.Context, string, time.Duration) (sshconn.Result, error)

var errUnknownMonitorProbe = errors.New("仅允许内置服务器状态采样")

// Only these fixed probes are internal telemetry within an already-authorized
// session. They do not create per-sample audit events. Public exec requests are
// audited normally, including requests with identical commands or caller labels.
func monitorProbeInterval(command string) (time.Duration, error) {
	switch command {
	case monitor.Command:
		return monitorCounterInterval, nil
	case monitor.InventoryCommand:
		return monitorInventoryInterval, nil
	case monitor.DockerStatsCommand:
		return monitorDockerInterval, nil
	default:
		return 0, errUnknownMonitorProbe
	}
}

func (s *Server) monitorExecutor(c *sshconn.Client, fingerprint string) monitorExecutor {
	// RemoteAddr uses the connected address, so DNS aliases of that same peer
	// cannot multiply sampling. Different users and host keys remain isolated.
	target := c.SSH.User() + "@" + c.SSH.RemoteAddr().String() + "\x00" + fingerprint
	return func(ctx context.Context, command string, timeout time.Duration) (sshconn.Result, error) {
		gap, err := monitorProbeInterval(command)
		if err != nil {
			return sshconn.Result{}, err
		}
		key := struct{ target, command string }{target, command}
		value, found := s.monitorBudgets.Load(key)
		if !found {
			value, _ = s.monitorBudgets.LoadOrStore(key, newMonitorBudget())
		}
		return value.(*monitorBudget).run(ctx, gap, func() (sshconn.Result, error) {
			probe, stop := context.WithTimeout(ctx, timeout)
			defer stop()
			return c.Exec(probe, command)
		})
	}
}
