package broker

import (
	"context"
	"errors"
	"time"

	"sshclient/internal/localipc"
	"sshclient/internal/monitor"
	"sshclient/internal/sshconn"
)

var errMonitorUnsupported = errors.New("当前服务器未提供 Linux 或 Windows 状态接口，终端仍可正常使用")
var errMonitorRead = errors.New("服务器状态读取未成功")

// Each caller provides its already-authorized SSH transport. A terminal session
// and its status channels share the same immutable grant, host and lifetime.
func streamMonitor(parent context.Context, c *sshconn.Client, w *localipc.Wire) error {
	return streamMonitorWithExecutor(parent, func(ctx context.Context, command string, timeout time.Duration) (sshconn.Result, error) {
		probe, stop := context.WithTimeout(ctx, timeout)
		defer stop()
		return c.Exec(probe, command)
	}, w)
}

func streamMonitorWithExecutor(parent context.Context, execute monitorExecutor, w *localipc.Wire) error {
	ctx, cancel := context.WithCancel(parent)
	defer cancel()
	platform := newMonitorPlatform()
	var previous *monitor.Stats
	var inventory *monitor.Inventory
	type inventoryResult struct {
		info    *monitor.Inventory
		windows bool
	}
	inventoryResults := make(chan inventoryResult, 1)
	dockerResults := make(chan *monitor.DockerSnapshot, 1)
	var docker *monitor.DockerSnapshot
	go func() {
		for ctx.Err() == nil {
			if platform.windows.Load() {
				return
			}
			result, err := execute(ctx, monitor.DockerStatsCommand, 4*time.Second)
			if ctx.Err() != nil {
				return
			}
			if platform.windows.Load() {
				return
			}
			snap := monitor.ParseDockerSnapshot(result.Stdout)
			if err != nil || result.ExitCode != 0 {
				snap.Status = "unavailable"
				snap.Note = "容器资源读取超时或失败"
				snap.Rows = nil
			}
			select {
			case dockerResults <- &snap:
			case <-ctx.Done():
				return
			}
			delay := monitorDockerInterval
			if snap.Status != "ready" {
				delay = time.Minute
			}
			if !platform.delay(ctx, delay, false) {
				return
			}
		}
	}()
	go func() {
		for ctx.Err() == nil {
			windows := platform.windows.Load()
			command, interval := monitor.InventoryCommand, monitorInventoryInterval
			if windows {
				command, interval = monitor.WindowsInventoryCommand, monitorWindowsInventoryInterval
			}
			started := time.Now().UTC()
			result, err := execute(ctx, command, 6*time.Second)
			if ctx.Err() != nil {
				return
			}
			info := monitor.ParseInventory(result.Stdout)
			if windows {
				info = monitor.ParseWindowsInventory(result.Stdout, started)
			}
			info.Time = started
			if err != nil || result.ExitCode != 0 {
				info.ServiceNote = "系统资料读取不完整；实时资源更新继续"
				if info.Docker != nil && info.Docker.Status == "checking" {
					info.Docker.Status = "unavailable"
					info.Docker.Note = "Docker 读取超时或失败，稍后自动重试"
				}
			}
			select {
			case inventoryResults <- inventoryResult{info: &info, windows: windows}:
			case <-ctx.Done():
				return
			}
			if !platform.delay(ctx, interval, windows) {
				return
			}
		}
	}()
	for ctx.Err() == nil {
		windows := platform.windows.Load()
		command, interval := monitor.Command, monitorCounterInterval
		if windows {
			command, interval = monitor.WindowsStatsCommand, monitorWindowsCounterInterval
		}
		result, err := execute(ctx, command, 6*time.Second)
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if err != nil {
			return err
		}
		var stats monitor.Stats
		if result.ExitCode != 0 {
			err = errMonitorRead
		} else if windows {
			stats, err = monitor.ParseWindows(result.Stdout, previous)
		} else {
			stats, err = monitor.Parse(result.Stdout, previous)
		}
		if err != nil {
			if !windows && platform.detectWindows(ctx, execute) {
				previous, inventory, docker = nil, nil, nil
				continue
			}
			if !windows && result.ExitCode == 69 && platform.knownPOSIX() {
				return errMonitorUnsupported
			}
			if !windows && !platform.knownPOSIX() {
				if !monitorDelay(ctx, 10*time.Second) {
					return ctx.Err()
				}
				continue
			}
			return err
		}
		select {
		case update := <-inventoryResults:
			if update.windows == windows {
				inventory = update.info
			}
		default:
		}
		stats.Inventory = inventory
		select {
		case docker = <-dockerResults:
		default:
		}
		if !windows {
			stats.DockerStats = docker
		}
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if err = w.Send(Message{Action: "stats", Stats: &stats}); err != nil {
			return err
		}
		previous = &stats
		if !platform.delay(ctx, interval, windows) {
			return ctx.Err()
		}
	}
	return ctx.Err()
}
