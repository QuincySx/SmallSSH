//go:build !windows

package monitor

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"
)

// Opt-in local Docker QA: unique project, no host mounts, volumes or published
// ports. All cleanup is limited to that same disposable project.
func TestLiveComposeProjectDetectionHealthAndResources(t *testing.T) {
	if os.Getenv("SSH_CLIENT_DOCKER_QA") != "1" {
		t.Skip("opt-in isolated local Docker QA")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	dir, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	file := filepath.Join(dir, "compose.yaml")
	project := fmt.Sprintf("sshwb-compose-qa-%d", time.Now().UnixNano())
	yaml := `services:
  web:
    image: alpine:3.21
    command: ["sleep", "120"]
    healthcheck:
      test: ["CMD", "true"]
      interval: 1s
      timeout: 1s
      retries: 1
  db:
    image: alpine:3.21
    command: ["sleep", "120"]
    healthcheck:
      test: ["CMD", "false"]
      interval: 1s
      timeout: 1s
      retries: 1
  worker:
    image: alpine:3.21
    command: ["true"]
`
	if err = os.WriteFile(file, []byte(yaml), 0600); err != nil {
		t.Fatal(err)
	}
	args := []string{"--host", "unix:///var/run/docker.sock", "compose", "--project-name", project, "--project-directory", dir, "--file", file}
	t.Cleanup(func() {
		cleanup, stop := context.WithTimeout(context.Background(), 20*time.Second)
		defer stop()
		cmd := exec.CommandContext(cleanup, "docker", append(slices.Clone(args), "down", "--remove-orphans")...)
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Errorf("cleanup of QA project failed: %v %s", err, out)
		}
	})
	if out, err := exec.CommandContext(ctx, "docker", append(slices.Clone(args), "up", "--detach", "--pull", "never")...).CombinedOutput(); err != nil {
		t.Fatal(err, string(out))
	}
	collect := func(command string) *DockerInfo {
		t.Helper()
		out, err := exec.CommandContext(ctx, "/bin/sh", "-c", command).CombinedOutput()
		if err != nil {
			t.Fatal(err, string(out))
		}
		d := ParseInventory(string(out)).Docker
		if d == nil || d.Status != "ready" {
			t.Fatal("actual Docker probe did not return an inventory", string(out))
		}
		return d
	}
	var found ComposeProject
	for {
		d := collect(DockerCommand)
		for _, p := range ComposeProjects(d.Containers, nil) {
			if p.Name == project {
				found = p
			}
		}
		if len(found.Containers) == 3 && found.Faults == 1 && found.Starting == 0 && found.Running == 2 {
			break
		}
		select {
		case <-ctx.Done():
			t.Fatal("real project status was not detected", found)
		case <-time.After(250 * time.Millisecond):
		}
	}
	stats := collect(DockerStatsCommand)
	metrics := map[string]ContainerStats{}
	for _, row := range stats.Resources {
		metrics[row.ID] = row
	}
	p := ComposeProjects(found.Containers, metrics)[0]
	if p.Services != 3 || p.State() != "attention" || p.CPUSamples != 2 || p.MemorySamples != 2 || p.Memory == 0 {
		t.Fatal("real project resources were not aggregated", p)
	}
	for _, row := range p.Containers {
		if row.ComposeDir != dir || !strings.Contains(row.ComposeFiles, file) {
			t.Fatal("real configuration labels not detected", row)
		}
	}
	t.Logf("Real Compose project: %d services, %d/%d containers running, %d unhealthy, CPU %.1f%%, memory %d bytes", p.Services, p.Running, len(p.Containers), p.Faults, p.CPU, p.Memory)
}
