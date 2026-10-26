package broker_test

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"sshclient/internal/approval"
	"sshclient/internal/broker"
	"sshclient/internal/client"
	"sshclient/internal/localipc"
	"sshclient/internal/serverops"
	"sshclient/internal/testserver"
	"sshclient/internal/vault"
)

// Explicit opt-in: creates only a uniquely named QA project with no published
// ports/host mounts. Cleanup is scoped to that same project and its test volume.
func TestLiveDockerComposeUpdatePreservesDataAndUnselectedService(t *testing.T) {
	if os.Getenv("SSH_CLIENT_DOCKER_QA") != "1" {
		t.Skip("opt-in isolated local Docker QA")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Minute)
	defer cancel()
	command := func(args ...string) (string, error) {
		cmd := exec.CommandContext(ctx, "docker", append([]string{"--host", "unix:///var/run/docker.sock"}, args...)...)
		cmd.Env = cleanDockerEnv()
		b, err := cmd.CombinedOutput()
		return strings.TrimSpace(string(b)), err
	}
	engine, err := command("info", "--format", "{{.ID}}")
	if err != nil {
		t.Fatal(err, engine)
	}
	dir, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	file := filepath.Join(dir, "compose.yaml")
	project := fmt.Sprintf("sshwb-qa-%d", time.Now().UnixNano())
	yaml := `services:
  web:
    image: alpine:3.21
    command: ["sh", "-c", "test -f /data/sentinel || echo original > /data/sentinel; touch /tmp/ready; while :; do echo workbench-qa-log; sleep 1; done"]
    volumes: ["qa-data:/data"]
    healthcheck:
      test: ["CMD", "test", "-f", "/tmp/ready"]
      interval: 1s
      timeout: 1s
      retries: 30
  other:
    image: alpine:3.21
    command: ["sleep", "600"]
  orphan:
    image: alpine:3.21
    command: ["sleep", "600"]
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
		args := append(append([]string{}, compose...), "down", "--volumes", "--remove-orphans")
		cmd := exec.CommandContext(cleanup, "docker", append([]string{"--host", "unix:///var/run/docker.sock"}, args...)...)
		cmd.Env = cleanDockerEnv()
		if b, e := cmd.CombinedOutput(); e != nil {
			t.Errorf("QA project cleanup failed: %v %s", e, b)
		}
	})
	if out, e := command(append(append([]string{}, compose...), "up", "--detach", "--wait", "--wait-timeout", "90")...); e != nil {
		t.Fatal(e, out)
	}
	container := func(service string) string {
		id, e := command("container", "ls", "--all", "--quiet", "--no-trunc", "--filter", "label=com.docker.compose.project="+project, "--filter", "label=com.docker.compose.service="+service)
		if e != nil || len(id) != 64 {
			t.Fatal("QA container missing", service, id, e)
		}
		return id
	}
	oldWeb, oldOther, oldOrphan := container("web"), container("other"), container("orphan")
	updated := strings.Replace(yaml, "alpine:3.21", "alpine:3.22", 1)
	updated = strings.Replace(updated, "  orphan:\n    image: alpine:3.21\n    command: [\"sleep\", \"600\"]\n", "", 1)
	if err = os.WriteFile(file, []byte(updated), 0600); err != nil {
		t.Fatal(err)
	}
	r := serverops.ComposeRequest{Target: serverops.DockerTarget{Socket: "/var/run/docker.sock", Engine: engine, ID: oldWeb}, Project: project, Directory: dir, Files: file, Services: "web"}
	ssh := testserver.NewWithStreamExec(t, func(sessionCtx context.Context, cmd string, in io.Reader, out, stderr io.Writer) int {
		c := exec.CommandContext(sessionCtx, "/bin/sh", "-c", cmd)
		if strings.Contains(cmd, "container exec --interactive --tty") {
			if runtime.GOOS == "darwin" {
				c = exec.CommandContext(sessionCtx, "/usr/bin/script", "-q", "/dev/null", "/bin/sh", "-c", cmd)
			} else {
				c = exec.CommandContext(sessionCtx, "script", "-q", "-c", cmd, "/dev/null")
			}
		}
		c.Env = append(cleanDockerEnv(), "PATH=/usr/local/bin:/opt/homebrew/bin:/usr/bin:/bin", "COMPOSE_REMOVE_ORPHANS=1")
		c.Stdin, c.Stdout, c.Stderr = in, out, stderr
		e := c.Run()
		code := 0
		if e != nil {
			code = 1
			if exit, ok := e.(*exec.ExitError); ok {
				code = exit.ExitCode()
			}
		}
		return code
	})
	private := dockerPrivateDir(t)
	if err = os.Chmod(private, 0700); err != nil {
		t.Fatal(err)
	}
	if err = localipc.Prepare(private); err != nil {
		t.Fatal(err)
	}
	v, _ := vault.New(pass)
	v.Put(ssh.Profile())
	if err = vault.Save(broker.VaultPath(private), v, true); err != nil {
		t.Fatal(err)
	}
	v.Close()
	server, err := broker.Open(private, pass)
	if err != nil {
		t.Fatal(err)
	}
	defer server.Close()
	c := client.Client{Dir: private}
	stream := approvedDockerStream(t, c, approval.Operation{Kind: "compose-plan", Compose: r})
	var plan *serverops.ComposePlan
	for {
		m, e := stream.Receive()
		if e != nil {
			t.Fatal(e)
		}
		if m.ComposePlan != nil {
			plan = m.ComposePlan
		}
		if m.Action == "done" {
			break
		}
	}
	stream.Close()
	if plan == nil {
		t.Fatal("no live plan")
	}
	r.ConfigHash = plan.Hash
	stream = approvedDockerStream(t, c, approval.Operation{Kind: "compose-update", Compose: r})
	var phases []string
	var log strings.Builder
	for {
		m, e := stream.Receive()
		if e != nil {
			t.Fatal(e, log.String())
		}
		if m.Phase != "" {
			phases = append(phases, m.Phase)
		}
		log.Write(m.Data)
		if m.Action == "done" {
			break
		}
	}
	stream.Close()
	newWeb := container("web")
	if newWeb == oldWeb || container("other") != oldOther || container("orphan") != oldOrphan {
		t.Fatal("selected service was not recreated or unselected service changed")
	}
	if out, e := command("container", "exec", newWeb, "cat", "/data/sentinel"); e != nil || out != "original" {
		t.Fatal("named-volume data lost", out, e)
	}
	logs := approvedDockerStream(t, c, approval.Operation{Kind: "docker-logs", Docker: serverops.DockerRequest{Target: serverops.DockerTarget{Socket: r.Target.Socket, Engine: engine, ID: newWeb}, Tail: 20}})
	var output, stderr strings.Builder
	code, _, e := logs.Copy(&output, &stderr)
	logs.Close()
	if e != nil || code != 0 || !strings.Contains(output.String(), "workbench-qa-log") {
		t.Fatal("live logs failed", code, e, output.String(), stderr.String())
	}
	pty := approvedDockerStream(t, c, approval.Operation{Kind: "docker-terminal", Docker: serverops.DockerRequest{Target: serverops.DockerTarget{Socket: r.Target.Socket, Engine: engine, ID: newWeb}, Shell: "sh"}})
	stopPTY := context.AfterFunc(ctx, func() { pty.Close() })
	defer stopPTY()
	defer pty.Close()
	if err = pty.Resize(110, 32); err != nil {
		t.Fatal(err)
	}
	reader := &client.TerminalConn{Stream: pty}
	var terminalOutput strings.Builder
	chunk := make([]byte, 2048)
	for !strings.Contains(terminalOutput.String(), "#") {
		n, e := reader.Read(chunk)
		terminalOutput.Write(chunk[:n])
		if e != nil {
			t.Fatal("live PTY startup failed", e, terminalOutput.String())
		}
		if terminalOutput.Len() > 128<<10 {
			t.Fatal("unexpected terminal output")
		}
	}
	if _, err = pty.Write([]byte("printf 'WB%s\\n' '-LIVE-PTY'\nexit\n")); err != nil {
		t.Fatal(err)
	}
	for !strings.Contains(terminalOutput.String(), "WB-LIVE-PTY") {
		n, e := reader.Read(chunk)
		terminalOutput.Write(chunk[:n])
		if e != nil {
			t.Fatal("live PTY input failed", e, terminalOutput.String())
		}
	}
	pty.Close()
	t.Log("Actual Docker daemon: selected image upgraded, unselected container unchanged, named-volume sentinel preserved, logs read through broker; phases:", phases)
}

func cleanDockerEnv() []string {
	out := []string{}
	for _, v := range os.Environ() {
		name, _, _ := strings.Cut(v, "=")
		switch name {
		case "DOCKER_HOST", "DOCKER_CONTEXT", "DOCKER_TLS_VERIFY", "DOCKER_CERT_PATH", "COMPOSE_FILE", "COMPOSE_PROJECT_NAME", "COMPOSE_PROFILES", "COMPOSE_ENV_FILES":
			continue
		}
		out = append(out, v)
	}
	return out
}
