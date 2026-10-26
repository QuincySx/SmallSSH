package monitor

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestDockerInventoryStatesJSONAndBoundedRows(t *testing.T) {
	for _, state := range []string{"missing", "unavailable", "checking"} {
		d := ParseInventory("@@DOCKER_STATUS\n" + state + "\n@@DOCKER_ERROR\npermission denied\n").Docker
		if d == nil || d.Status != state || !strings.Contains(d.Note, "permission denied") {
			t.Fatal(d)
		}
	}
	var text strings.Builder
	text.WriteString("@@DOCKER_STATUS\nready\n@@DOCKER_SOCKET\n/var/run/docker.sock\n@@DOCKER_INFO\n{\"ID\":\"engine\",\"Version\":\"28.0\"}\n@@DOCKER_CONTAINERS\n")
	for n := 0; n < 1100; n++ {
		fmt.Fprintf(&text, "{\"ID\":\"%064x\",\"Names\":\"app-%d\",\"Image\":\"nginx:latest\",\"State\":\"running\",\"Ports\":\"127.0.0.1:8080->80/tcp\"}\n", n, n)
	}
	text.WriteString("@@DOCKER_IMAGES\n{\"ID\":\"sha256:123\",\"Repository\":\"nginx\",\"Tag\":\"latest\",\"Size\":\"50MB\"}\n")
	d := ParseInventory(text.String()).Docker
	if d == nil || len(d.Containers) != 1024 || d.EngineID != "engine" || d.Version != "28.0" || d.Note == "" || len(d.Images) != 1 || d.Containers[0].Ports != "127.0.0.1:8080->80/tcp" {
		t.Fatal(d)
	}
}

func TestDockerProbeDistinguishesPermissionFailureFromEmpty(t *testing.T) {
	dir := t.TempDir()
	script := `#!/bin/sh
if [ "$1" = --version ]; then printf 'Docker version fixture\n'; exit 0; fi
[ -z "$DOCKER_HOST$DOCKER_CONTEXT$DOCKER_CERT_PATH$DOCKER_TLS_VERIFY" ] || exit 80
[ "$1" = --host ] || exit 81
shift 2
if [ "$TEST_DENY" = 1 ]; then printf 'permission denied\n' >&2;exit 1;fi
if [ "$1" = info ]; then printf '{"ID":"engine","Version":"28.0"}\n';exit 0;fi
exit 0
`
	if err := os.WriteFile(filepath.Join(dir, "docker"), []byte(script), 0700); err != nil {
		t.Fatal(err)
	}
	for _, deny := range []string{"0", "1"} {
		cmd := exec.Command("/bin/sh", "-c", DockerCommand)
		cmd.Env = append(os.Environ(), "PATH="+dir+":/usr/bin:/bin", "DOCKER_CONTEXT=remote", "DOCKER_HOST=tcp://remote", "TEST_DENY="+deny)
		output, err := cmd.Output()
		if err != nil {
			t.Fatal(err)
		}
		d := ParseInventory(string(output)).Docker
		if d == nil {
			t.Fatal(string(output))
		}
		if deny == "1" {
			if d.Status != "unavailable" || !strings.Contains(d.Note, "permission denied") {
				t.Fatal(d)
			}
		} else {
			if d.Status != "ready" || d.EngineID != "engine" || len(d.Containers) != 0 || d.Note != "" {
				t.Fatal(d)
			}
		}
	}
}

func TestProcessIdentityIsBoundToCurrentSample(t *testing.T) {
	s, err := Parse(sample("100 0 100 800 0 0 0 0", 1, 2)+"@@PROCESS_DETAIL\n42 1 2 1024 root S 00:01 worker\n@@BOOT_ID\n00000000-0000-0000-0000-000000000000\n@@PROCESS_IDENTITY\n42 12345\n999 9999\n", nil)
	if err != nil || len(s.Processes) != 1 || s.Processes[0].StartTicks != 12345 || s.BootID == "" {
		t.Fatal(s, err)
	}
}

func TestDockerResourceProbeUsesNoStreamAndCapsFullContainerIDs(t *testing.T) {
	dir := t.TempDir()
	ids := filepath.Join(dir, "ids")
	args := filepath.Join(dir, "args")
	var list strings.Builder
	for n := 0; n < 129; n++ {
		fmt.Fprintf(&list, "%064x\n", n)
	}
	if err := os.WriteFile(ids, []byte(list.String()), 0600); err != nil {
		t.Fatal(err)
	}
	script := `#!/bin/sh
if [ "$1" = --version ];then printf 'Docker fixture\n';exit 0;fi
[ "$1" = --host ] || exit 90
shift 2
case "$1 $2" in
 "info --format") printf '{"ID":"engine","Version":"28.0"}\n';;
 "container ls") cat "$TEST_IDS";;
 "container stats")
  printf '%s\n' "$@" > "$TEST_ARGS"
  shift 2
  [ "$1" = --no-stream ] && [ "$2" = --no-trunc ] && [ "$3" = --format ] || exit 91
  shift 4
  for id in "$@";do printf '{"ID":"%s","CPUPerc":"3.0%%","MemUsage":"205MiB / 2GiB"}\n' "$id";done;;
 *) exit 92;;
esac
`
	if err := os.WriteFile(filepath.Join(dir, "docker"), []byte(script), 0700); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command("/bin/sh", "-c", DockerStatsCommand)
	cmd.Env = append(os.Environ(), "PATH="+dir+":/usr/bin:/bin", "TEST_IDS="+ids, "TEST_ARGS="+args)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatal(err, string(out))
	}
	s := ParseDockerSnapshot(string(out))
	if len(s.Rows) != 128 || !strings.Contains(s.Note, "128") {
		t.Fatal(len(s.Rows), s.Note, string(out))
	}
	log, _ := os.ReadFile(args)
	if !strings.Contains(string(log), "--no-stream\n--no-trunc") || strings.Contains(string(log), fmt.Sprintf("%064x", 128)) {
		t.Fatal(string(log))
	}
}
