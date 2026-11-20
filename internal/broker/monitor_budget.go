package broker

import (
	"context"
	"errors"
	"os"
	"sshclient/internal/approval"
	"sshclient/internal/vault"
	"time"

	"sshclient/internal/monitor"
	"sshclient/internal/sshconn"
)

const (
	monitorCounterInterval          = 2 * time.Second
	monitorDockerInterval           = 10 * time.Second
	monitorInventoryInterval        = time.Minute
	monitorWindowsCounterInterval   = 10 * time.Second
	monitorWindowsInventoryInterval = 5 * time.Minute
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
// session. Actual executions also enter the system audit category; budget waits
// and cache fanout do not masquerade as extra remote executions.
func monitorProbeInterval(command string) (time.Duration, error) {
	switch command {
	case monitor.PlatformProbe, monitor.WindowsProbeCommand:
		return monitorCounterInterval, nil
	case monitor.WindowsStatsCommand:
		return monitorWindowsCounterInterval, nil
	case monitor.WindowsInventoryCommand:
		return monitorWindowsInventoryInterval, nil
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

type monitorAuditSource struct{ Host string }

func (s *Server) monitorExecutor(c *sshconn.Client, fingerprint string, sources ...monitorAuditSource) monitorExecutor {
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
			if len(sources) == 0 {
				return c.Exec(probe, command)
			}
			source := sources[0]
			executable, _ := os.Executable()
			request := approval.Request{ID: vault.ShortID(), Peer: approval.Peer{PID: os.Getpid(), Path: executable}, Operation: approval.Operation{Kind: "system-probe", Host: source.Host, Endpoint: c.SSH.User() + "@" + c.SSH.RemoteAddr().String(), HostFingerprint: fingerprint, Command: command, Client: "SmallSSH 系统采样"}}
			if err := s.auditRequest("system_started", &request); err != nil {
				return sshconn.Result{}, err
			}
			result, runErr := c.Exec(probe, command)
			outcome := "success"
			var code *int
			if runErr != nil {
				outcome = "failed"
				if probe.Err() != nil {
					outcome = "cancelled"
				}
			} else {
				code = &result.ExitCode
				if result.ExitCode != 0 {
					outcome = "failed"
				}
			}
			name := ""
			if p, e := s.Vault.Profile(source.Host); e == nil {
				name = p.Name
			}
			if err := s.audit.writeOutcome("system_ended", &request, name, outcome, code); err != nil {
				return sshconn.Result{}, s.failAudit(err)
			}
			return result, runErr
		})
	}
}
