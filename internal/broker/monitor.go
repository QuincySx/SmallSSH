package broker

import (
	"context"
	"errors"
	"time"

	"sshclient/internal/localipc"
	"sshclient/internal/monitor"
	"sshclient/internal/sshconn"
)

var errMonitorUnsupported = errors.New("当前服务器未提供 Linux 状态接口，终端仍可正常使用")
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
	var previous *monitor.Stats
	var inventory *monitor.Inventory
	inventoryResults := make(chan *monitor.Inventory, 1)
	dockerResults := make(chan *monitor.DockerSnapshot, 1)
	var docker *monitor.DockerSnapshot
	go func() {
		for ctx.Err() == nil {
			result, err := execute(ctx, monitor.DockerStatsCommand, 4*time.Second)
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
			select {
			case <-ctx.Done():
				return
			case <-time.After(delay):
			}
		}
	}()
	go func() {
		for ctx.Err() == nil {
			started := time.Now().UTC()
			result, err := execute(ctx, monitor.InventoryCommand, 6*time.Second)
			info := monitor.ParseInventory(result.Stdout)
			info.Time = started
			if err != nil || result.ExitCode != 0 {
				info.ServiceNote = "系统资料读取不完整；实时资源更新继续"
				if info.Docker != nil && info.Docker.Status == "checking" {
					info.Docker.Status = "unavailable"
					info.Docker.Note = "Docker 读取超时或失败，稍后自动重试"
				}
			}
			select {
			case inventoryResults <- &info:
			case <-ctx.Done():
				return
			}
			select {
			case <-ctx.Done():
				return
			case <-time.After(monitorInventoryInterval):
			}
		}
	}()
	for ctx.Err() == nil {
		result, err := execute(ctx, monitor.Command, 6*time.Second)
		if err != nil {
			return err
		}
		if result.ExitCode == 69 {
			return errMonitorUnsupported
		}
		if result.ExitCode != 0 {
			return errMonitorRead
		}
		stats, err := monitor.Parse(result.Stdout, previous)
		if err != nil {
			return err
		}
		select {
		case inventory = <-inventoryResults:
		default:
		}
		stats.Inventory = inventory
		select {
		case docker = <-dockerResults:
		default:
		}
		stats.DockerStats = docker
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if err = w.Send(Message{Action: "stats", Stats: &stats}); err != nil {
			return err
		}
		previous = &stats
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(monitorCounterInterval):
		}
	}
	return ctx.Err()
}
