package serverops

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
)

func TestRejectUnreviewableTargetsAndShellInjection(t *testing.T) {
	for _, unit := range []string{"--help.service", "a;touch /tmp/x.service", "../sshd.service", "a'$(id).service", "a*.service", "worker@.service", "x\n.service"} {
		if _, err := Service(unit, "stop"); err == nil {
			t.Fatal("unsafe unit accepted", unit)
		}
	}
	for _, unit := range []string{"nginx.service", "frp@home.service", "user@1000.service"} {
		if _, err := Service(unit, "restart"); err != nil {
			t.Fatal(err)
		}
	}
	for _, pid := range []int{-10, 0, 1} {
		if _, err := KillProcess(pid, 20, "00000000-0000-0000-0000-000000000000", "app", "KILL"); err == nil {
			t.Fatal("dangerous PID", pid)
		}
	}
	if _, err := KillProcess(25, 0, "00000000-0000-0000-0000-000000000000", "app", "TERM"); err == nil {
		t.Fatal("missing lifetime accepted")
	}
	if _, err := DockerContainer("tcp://elsewhere", "engine", strings.Repeat("a", 64), "stop"); err == nil {
		t.Fatal("remote daemon accepted")
	}
	if _, err := DockerContainer("/var/run/docker.sock", "engine", "short-id", "stop"); err == nil {
		t.Fatal("ambiguous container accepted")
	}
	if _, err := DockerContainer("/var/run/docker.sock", "engine", strings.Repeat("a", 64), "rm -f"); err == nil {
		t.Fatal("arbitrary action accepted")
	}
}

func TestProcessGuardsAndSignalOnlyOwnedChild(t *testing.T) {
	child := exec.Command("/bin/sleep", "30")
	if err := child.Start(); err != nil {
		t.Fatal(err)
	}
	defer child.Process.Kill()
	dir := t.TempDir()
	pid := child.Process.Pid
	if err := os.MkdirAll(filepath.Join(dir, "sys/kernel/random"), 0700); err != nil {
		t.Fatal(err)
	}
	proc := filepath.Join(dir, strconv.Itoa(pid))
	if err := os.Mkdir(proc, 0700); err != nil {
		t.Fatal(err)
	}
	boot := "00000000-0000-0000-0000-000000000000"
	if err := os.WriteFile(filepath.Join(dir, "sys/kernel/random/boot_id"), []byte(boot+"\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(proc, "comm"), []byte("owned-child\n"), 0600); err != nil {
		t.Fatal(err)
	}
	stat := fmt.Sprintf("%d (owned-child) S %s 12345\n", pid, strings.Repeat("0 ", 18))
	if err := os.WriteFile(filepath.Join(proc, "stat"), []byte(stat), 0600); err != nil {
		t.Fatal(err)
	}
	for _, check := range []struct {
		ticks      uint64
		boot, name string
		valid      bool
	}{{12346, boot, "owned-child", false}, {12345, "11111111-1111-1111-1111-111111111111", "owned-child", false}, {12345, boot, "other", false}, {12345, boot, "owned-child", true}} {
		command, err := KillProcess(pid, check.ticks, check.boot, check.name, "TERM")
		if err != nil {
			t.Fatal(err)
		}
		// Test-only /proc mapping. The only signalled process is this test's child.
		command = strings.ReplaceAll(command, "/proc/", dir+"/")
		out, err := exec.Command("/bin/sh", "-c", command).CombinedOutput()
		if check.valid {
			if err != nil {
				t.Fatal(err, string(out))
			}
		} else if err == nil {
			t.Fatal("identity mismatch was accepted")
		}
	}
	done := make(chan error, 1)
	go func() { done <- child.Wait() }()
	select {
	case err := <-done:
		if err == nil {
			t.Fatal("child was not signalled")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("TERM did not reach owned child")
	}
}

func TestDockerUsesPinnedLocalDaemonAndFullID(t *testing.T) {
	dir := t.TempDir()
	log := filepath.Join(dir, "called")
	script := `#!/bin/sh
[ -z "$DOCKER_CONTEXT$DOCKER_HOST$DOCKER_TLS_VERIFY$DOCKER_CERT_PATH" ] || exit 40
[ "$1" = --host ] && [ "$2" = unix:///var/run/docker.sock ] || exit 41
shift 2
if [ "$1" = info ]; then printf '%s\n' "$TEST_ENGINE"; exit 0; fi
printf '%s\n' "$@" > "$TEST_LOG"
`
	if err := os.WriteFile(filepath.Join(dir, "docker"), []byte(script), 0700); err != nil {
		t.Fatal(err)
	}
	command, err := DockerContainer("/var/run/docker.sock", "original-engine", strings.Repeat("a", 64), "stop")
	if err != nil {
		t.Fatal(err)
	}
	for _, engine := range []string{"changed-engine", "original-engine"} {
		cmd := exec.Command("/bin/sh", "-c", command)
		cmd.Env = append(os.Environ(), "PATH="+dir+":/usr/bin:/bin", "DOCKER_CONTEXT=wrong-context", "DOCKER_HOST=tcp://wrong", "DOCKER_TLS_VERIFY=1", "DOCKER_CERT_PATH=/wrong", "TEST_ENGINE="+engine, "TEST_LOG="+log)
		output, err := cmd.CombinedOutput()
		if engine == "changed-engine" {
			if err == nil {
				t.Fatal("changed daemon accepted")
			}
			if _, e := os.Stat(log); !os.IsNotExist(e) {
				t.Fatal("action reached wrong daemon")
			}
		} else {
			if err != nil {
				t.Fatal(err, string(output))
			}
			data, _ := os.ReadFile(log)
			if string(data) != "container\nstop\n-t\n10\n"+strings.Repeat("a", 64)+"\n" {
				t.Fatal(string(data))
			}
		}
	}
}

func TestServiceOperationDoesNotCombineStopAndDisable(t *testing.T) {
	dir := t.TempDir()
	log := filepath.Join(dir, "called")
	if err := os.WriteFile(filepath.Join(dir, "systemctl"), []byte("#!/bin/sh\nprintf '%s\\n' \"$@\" > \"$TEST_LOG\"\n"), 0700); err != nil {
		t.Fatal(err)
	}
	for _, verb := range []string{"start", "stop", "restart", "enable", "disable"} {
		command, err := Service("nginx.service", verb)
		if err != nil {
			t.Fatal(err)
		}
		cmd := exec.Command("/bin/sh", "-c", command)
		cmd.Env = append(os.Environ(), "PATH="+dir+":/usr/bin:/bin", "TEST_LOG="+log)
		if output, err := cmd.CombinedOutput(); err != nil {
			t.Fatal(err, string(output))
		}
		data, _ := os.ReadFile(log)
		if string(data) != "--no-ask-password\n"+verb+"\n--\nnginx.service\n" {
			t.Fatal(string(data))
		}
	}
}
