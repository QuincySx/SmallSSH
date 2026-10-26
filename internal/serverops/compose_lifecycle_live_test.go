//go:build !windows

package serverops_test

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

	"sshclient/internal/serverops"
)

// Disposable local project only: no ports or host mounts. The separate
// container proves lifecycle operations do not affect unrelated workloads.
func TestLiveComposeLifecyclePreservesContainersDataAndOtherProject(t *testing.T) {
	if os.Getenv("SSH_CLIENT_DOCKER_QA") != "1" {
		t.Skip("opt-in isolated local Docker QA")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	docker := func(args ...string) string {
		t.Helper()
		out, err := exec.CommandContext(ctx, "docker", append([]string{"--host", "unix:///var/run/docker.sock"}, args...)...).CombinedOutput()
		if err != nil {
			t.Fatal(err, string(out))
		}
		return strings.TrimSpace(string(out))
	}
	dir, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	project := fmt.Sprintf("sshwb-lifecycle-qa-%d", time.Now().UnixNano())
	other := project + "-outside"
	file := filepath.Join(dir, "compose.yaml")
	yaml := `services:
  web:
    image: alpine:3.21
    command: ["sh", "-c", "test -f /data/sentinel || echo original > /data/sentinel; exec sleep 300"]
    volumes: ["qa-data:/data"]
  db:
    image: alpine:3.21
    command: ["sleep", "300"]
volumes:
  qa-data: {}
`
	if err = os.WriteFile(file, []byte(yaml), 0600); err != nil {
		t.Fatal(err)
	}
	compose := []string{"compose", "--project-name", project, "--project-directory", dir, "--file", file}
	t.Cleanup(func() {
		cleanup, stop := context.WithTimeout(context.Background(), 30*time.Second)
		defer stop()
		for _, args := range [][]string{append(slices.Clone(compose), "down", "--volumes", "--remove-orphans"), {"container", "rm", "--force", other}} {
			if out, err := exec.CommandContext(cleanup, "docker", append([]string{"--host", "unix:///var/run/docker.sock"}, args...)...).CombinedOutput(); err != nil {
				t.Errorf("QA cleanup failed: %v %s", err, out)
			}
		}
	})
	docker(append(slices.Clone(compose), "up", "--detach", "--pull", "never")...)
	docker("run", "--detach", "--name", other, "--pull", "never", "alpine:3.21", "sleep", "300")
	otherStarted := docker("inspect", "--format", "{{.State.StartedAt}}", other)
	container := func(service string) string {
		return docker("container", "ls", "--all", "--quiet", "--no-trunc", "--filter", "label=com.docker.compose.project="+project, "--filter", "label=com.docker.compose.service="+service)
	}
	web, db := container("web"), container("db")
	r := serverops.ComposeRequest{Target: serverops.DockerTarget{Socket: "/var/run/docker.sock", Engine: docker("info", "--format", "{{.ID}}"), ID: web}, Project: project, Directory: dir, Files: file, Services: "db,web"}
	members := []serverops.ComposeMember{{ID: web, Service: "web"}, {ID: db, Service: "db"}}
	run := func(action string) {
		t.Helper()
		command, err := serverops.ComposeLifecycle(r, action, members)
		if err != nil {
			t.Fatal(err)
		}
		cmd := exec.CommandContext(ctx, "/bin/sh", "-c", command)
		cmd.Env = append(os.Environ(), "DOCKER_HOST=tcp://wrong-host", "DOCKER_CONTEXT=wrong-context", "COMPOSE_FILE=/wrong.yml", "COMPOSE_REMOVE_ORPHANS=1")
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatal(action, err, string(out))
		}
	}
	run("stop")
	for _, id := range []string{web, db} {
		if state := docker("inspect", "--format", "{{.State.Status}}", id); state != "exited" {
			t.Fatal("stop did not stop the whole project", state)
		}
	}
	run("start")
	for _, id := range []string{web, db} {
		if state := docker("inspect", "--format", "{{.State.Status}}", id); state != "running" {
			t.Fatal("start did not start the whole project", state)
		}
	}
	docker("stop", "--time", "1", web)
	before := docker("inspect", "--format", "{{.State.StartedAt}}", db)
	run("restart")
	if docker("inspect", "--format", "{{.State.Status}}", web) != "running" || docker("inspect", "--format", "{{.State.StartedAt}}", db) == before {
		t.Fatal("restart did not include both stopped and running services")
	}
	if container("web") != web || container("db") != db || docker("exec", web, "cat", "/data/sentinel") != "original" {
		t.Fatal("lifecycle recreated a container or changed its volume")
	}
	if docker("inspect", "--format", "{{.State.StartedAt}}", other) != otherStarted {
		t.Fatal("another workload was restarted")
	}
	t.Log("Real Compose stop/start/restart passed; original IDs, volume contents and unrelated workload preserved")
}
