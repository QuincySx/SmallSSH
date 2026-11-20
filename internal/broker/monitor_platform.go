package broker

import (
	"context"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"sshclient/internal/monitor"
)

// Healthy Linux sampling needs no extra remote commands. A failed Linux probe
// may identify Windows using these fixed probes within the same grant/budget.
// All sampling lanes share the result and wake immediately when it changes.
type monitorPlatform struct {
	mu      sync.Mutex
	posix   bool
	windows atomic.Bool
	changed chan struct{}
}

func newMonitorPlatform() *monitorPlatform { return &monitorPlatform{changed: make(chan struct{})} }

func (p *monitorPlatform) detectWindows(ctx context.Context, execute monitorExecutor) bool {
	if ctx.Err() != nil {
		return false
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	if ctx.Err() != nil {
		return false
	}
	if p.windows.Load() || p.posix {
		return p.windows.Load()
	}
	result, err := execute(ctx, monitor.PlatformProbe, 3*time.Second)
	if err != nil || ctx.Err() != nil {
		return false
	}
	windows := result.ExitCode == 0 && monitor.WindowsPlatform(result.Stdout)
	if !windows && result.ExitCode == 0 {
		for _, field := range strings.Fields(result.Stdout) {
			switch field {
			case "Linux", "Darwin", "FreeBSD", "OpenBSD", "NetBSD", "DragonFly":
				p.posix = true
				return false
			}
		}
	}
	if !windows {
		// PowerShell login shells do not expand cmd.exe's %OS% variable.
		result, err = execute(ctx, monitor.WindowsProbeCommand, 4*time.Second)
		version, parseErr := strconv.Atoi(strings.TrimSpace(result.Stdout))
		windows = err == nil && result.ExitCode == 0 && parseErr == nil && version > 0
	}
	if windows && ctx.Err() == nil {
		p.windows.Store(true)
		close(p.changed)
	}
	// Transient errors and unrecognized output do not commit a platform result.
	return p.windows.Load()
}

func (p *monitorPlatform) knownPOSIX() bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.posix
}

func (p *monitorPlatform) delay(ctx context.Context, interval time.Duration, windows bool) bool {
	changed := p.changed
	if windows {
		changed = nil
	}
	timer := time.NewTimer(interval)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return false
	case <-changed:
		return ctx.Err() == nil
	case <-timer.C:
		return ctx.Err() == nil
	}
}
