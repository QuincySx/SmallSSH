// Package testdocker supplies an isolated Docker CLI fixture. It never opens a
// Docker socket, downloads images or operates the developer's containers.
package testdocker

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"sshclient/internal/serverops"
)

type Fixture struct {
	Dir, Bin, Model, Calls, State, Members string
	Target                                 serverops.ComposeRequest
}

func New(t testing.TB) *Fixture {
	t.Helper()
	root := t.TempDir()
	dir := filepath.Join(root, "project ' $(not-a-command)")
	if err := os.Mkdir(dir, 0700); err != nil {
		t.Fatal(err)
	}
	f := &Fixture{Dir: dir, Bin: root, Model: filepath.Join(root, "model.json"), Calls: filepath.Join(root, "calls"), State: filepath.Join(root, "mode"), Members: filepath.Join(root, "members")}
	f.Target = serverops.ComposeRequest{Target: serverops.DockerTarget{Socket: "/var/run/docker.sock", Engine: "fixture-engine", ID: strings.Repeat("a", 64)}, Project: "sample", Directory: dir, Files: filepath.Join(dir, "compose.yaml"), Services: "web"}
	if err := os.WriteFile(f.Members, []byte(f.Target.Target.ID+" web\n"), 0600); err != nil {
		t.Fatal(err)
	}
	model := map[string]any{"name": "sample", "services": map[string]any{"web": map[string]any{"image": "example/web:1", "environment": map[string]string{"PASSWORD": "fixture-private-value"}}, "database": map[string]any{"image": "example/db:1"}}}
	b, _ := json.Marshal(model)
	if err := os.WriteFile(f.Model, b, 0600); err != nil {
		t.Fatal(err)
	}
	script := `#!/bin/sh
set -eu
[ -z "${DOCKER_HOST-}${DOCKER_CONTEXT-}${DOCKER_TLS_VERIFY-}${DOCKER_CERT_PATH-}" ] || exit 40
[ "$1" = --host ] && [ "$2" = unix:///var/run/docker.sock ] || exit 41
shift 2
mode=$(cat "$TEST_MODE" 2>/dev/null || true)
if [ "$1" = info ]; then printf '%s\n' fixture-engine;exit 0;fi
if [ "$1" = container ]; then
 shift
 case "$1" in
 ls)
  if [ "$mode" = changed-members ]; then printf '%064d\n' 1;else awk '{print $1}' "$TEST_MEMBERS";fi;exit 0;;
 inspect)
  case "$3" in
   *'{{.Id}}'*)
    suffix=ok
    [ "$mode" != changed-source ] || suffix=changed
    while read -r id service;do printf '%s %s %s\n' "$id" "$service" "$suffix";done < "$TEST_MEMBERS";;
   *working_dir*) printf '%s\n' "$TEST_PROJECT";;
   *config_files*) printf '%s\n' "$TEST_FILES";;
   *) printf 'sample\n';;
  esac;exit 0;;
 logs) printf '\033[32m2026-10-21T10:00:00Z fixture log\033[0m\n';printf 'fixture stderr log\n' >&2;exit 0;;
 exec) printf 'container-ready> ';cat;exit 0;;
 esac
fi
[ "$1" = compose ] || exit 42
shift
if [ "$1" = version ];then printf '2.39.0\n';exit 0;fi
while [ "$#" -gt 0 ];do
 case "$1" in
  --ansi|--project-name|--project-directory|--profile|--file) shift 2;;
  *) break;;
 esac
done
mode=$(cat "$TEST_MODE" 2>/dev/null || true)
case "$1" in
 start|stop|restart)
  printf '%s %s\n' "$1" "$*" >> "$TEST_CALLS"
  [ "$mode" != lifecycle-fail ] || { printf 'fixture lifecycle failed\n' >&2;exit 1; };;
 config) cat "$TEST_MODEL";;
 pull)
  printf 'pull %s\n' "$*" >> "$TEST_CALLS"
  printf 'pulling fixture image\n'
  [ "$mode" != pull-fail ] || { printf 'fixture registry denied\n' >&2;exit 1; }
  if [ "$mode" = change ];then printf '{"name":"sample","services":{"web":{"image":"example/web:2"}}}' > "$TEST_MODEL";fi;;
 up)
  if [ "$2" = --help ];then printf ' --wait-timeout\n';exit 0;fi
  [ "${COMPOSE_REMOVE_ORPHANS-}" = 0 ] || exit 44
  case "$*" in *--remove-orphans=false*) ;; *) exit 45;;esac
  printf 'up %s\n' "$*" >> "$TEST_CALLS"
  printf 'recreating fixture web\n'
  [ "$mode" != up-fail ] || { printf 'fixture unhealthy\n' >&2;exit 1; };;
 ps)
  if [ "$mode" = bad-status ];then printf '[{"ID":"new-id","Name":"web","Service":"web","State":"running","Health":"unhealthy"}]\n'
  else printf '[{"ID":"new-id","Name":"web","Service":"web","State":"running","Health":"healthy","Image":"example/web:1"}]\n';fi;;
 *) exit 43;;
esac
`
	if err := os.WriteFile(filepath.Join(root, "docker"), []byte(script), 0700); err != nil {
		t.Fatal(err)
	}
	return f
}
func (f *Fixture) Command(ctx context.Context, command string) *exec.Cmd {
	cmd := exec.CommandContext(ctx, "/bin/sh", "-c", command)
	cmd.Env = append(os.Environ(), "PATH="+f.Bin+":/usr/bin:/bin", "DOCKER_HOST=tcp://wrong-daemon", "DOCKER_CONTEXT=wrong", "COMPOSE_FILE=/wrong", "COMPOSE_REMOVE_ORPHANS=1", "TEST_PROJECT="+f.Dir, "TEST_FILES="+f.Target.Files, "TEST_MODEL="+f.Model, "TEST_MODE="+f.State, "TEST_CALLS="+f.Calls, "TEST_MEMBERS="+f.Members)
	return cmd
}
func (f *Fixture) Execute(command string) (string, string, int) {
	var out, stderr strings.Builder
	cmd := f.Command(context.Background(), command)
	cmd.Stdout, cmd.Stderr = &out, &stderr
	err := cmd.Run()
	code := 0
	if err != nil {
		code = 1
		if e, ok := err.(*exec.ExitError); ok {
			code = e.ExitCode()
		} else {
			fmt.Fprintln(&stderr, err)
		}
	}
	return out.String(), stderr.String(), code
}
func (f *Fixture) Stream(ctx context.Context, command string, stdin io.Reader, stdout, stderr io.Writer) int {
	cmd := f.Command(ctx, command)
	cmd.Stdin, cmd.Stdout, cmd.Stderr = stdin, stdout, stderr
	if err := cmd.Run(); err != nil {
		if e, ok := err.(*exec.ExitError); ok {
			return e.ExitCode()
		}
		return 1
	}
	return 0
}
