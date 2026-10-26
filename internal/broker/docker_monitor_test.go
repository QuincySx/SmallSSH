package broker

import (
	"context"
	"fmt"
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

func TestSlowDockerSamplingDoesNotBlockHostCounters(t *testing.T) {
	var sample atomic.Uint64
	fixture := testserver.NewWithExec(t, func(command string) (string, string, int) {
		switch command {
		case monitor.Command:
			n := sample.Add(1)
			return fmt.Sprintf("@@CPU\ncpu %d 0 100 %d 0 0 0 0\n@@MEM\nMemTotal: 524288 kB\nMemAvailable: 300000 kB\n", 100+n*10, 800+n*90), "", 0
		case monitor.InventoryCommand:
			return "@@SERVICE_MANAGER\nsystemd\n@@DOCKER_STATUS\nready\n@@DOCKER_INFO\n{\"ID\":\"fixture-engine\"}\n@@DOCKER_SOCKET\n/var/run/docker.sock\n", "", 0
		case monitor.DockerStatsCommand:
			time.Sleep(3 * time.Second)
			return fmt.Sprintf("@@DOCKER_STATUS\nready\n@@DOCKER_INFO\n{\"ID\":\"fixture-engine\"}\n@@DOCKER_SOCKET\n/var/run/docker.sock\n@@DOCKER_STATS\n{\"ID\":%q,\"CPUPerc\":\"3.0%%\",\"MemUsage\":\"205MiB / 2GiB\"}\n", strings.Repeat("a", 64)), "", 0
		}
		return "", "unsupported", 1
	})
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	defer cancel()
	c, err := sshconn.Dial(ctx, fixture.Profile())
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	left, right := net.Pipe()
	defer left.Close()
	defer right.Close()
	go streamMonitor(ctx, c, &localipc.Wire{Conn: left})
	reader := &localipc.Wire{Conn: right}
	started := time.Now()
	right.SetReadDeadline(time.Now().Add(7 * time.Second))
	for n := 0; n < 3; n++ {
		var message Message
		if err := reader.Receive(&message); err != nil {
			t.Fatal(err)
		}
		if message.Stats == nil || message.Stats.MemoryTotal == 0 {
			t.Fatal(message)
		}
		if n == 1 && time.Since(started) > 2800*time.Millisecond {
			t.Fatal("slow Docker delayed host sample", time.Since(started))
		}
		if n == 2 && (message.Stats.DockerStats == nil || len(message.Stats.DockerStats.Rows) != 1) {
			t.Fatal("asynchronous Docker sample did not reach stream", message.Stats.DockerStats)
		}
	}
}
