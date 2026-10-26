// Package serverops builds reviewed server management commands. It performs no
// network IO or authorization; the desktop/CLI must use the shared broker.
package serverops

import (
	"errors"
	"fmt"
	"regexp"
	"strings"
)

var unitName = regexp.MustCompile(`^[A-Za-z0-9_][A-Za-z0-9_.:@-]{0,240}\.service$`)
var bootID = regexp.MustCompile(`^[a-fA-F0-9-]{36}$`)
var containerID = regexp.MustCompile(`^[a-f0-9]{64}$`)
var engineID = regexp.MustCompile(`^[A-Za-z0-9:._-]{1,128}$`)
var rootlessSocket = regexp.MustCompile(`^/run/user/[0-9]+/docker\.sock$`)

func quote(s string) string { return "'" + strings.ReplaceAll(s, "'", "'\"'\"'") + "'" }

// KillProcess checks a sampled process lifetime and boot before signalling one
// positive PID. It never signals a process group, PID 1, or retries with KILL.
func KillProcess(pid int, ticks uint64, boot, name, signal string) (string, error) {
	if pid <= 1 || ticks == 0 || !bootID.MatchString(boot) || name == "" || len(name) > 256 || strings.ContainsAny(name, "\x00\r\n") {
		return "", errors.New("进程身份尚未读取完整，请等待下一次采样")
	}
	if signal != "TERM" && signal != "KILL" {
		return "", errors.New("不支持的进程信号")
	}
	return fmt.Sprintf(`expected_boot=%s
[ "$(cat /proc/sys/kernel/random/boot_id)" = "$expected_boot" ] || { printf '服务器已重启，请刷新后重试\n' >&2; exit 75; }
pid=%d
IFS= read -r line < "/proc/$pid/stat" || { printf '进程已退出\n' >&2; exit 76; }
rest=${line##*) }; set -- $rest
[ "$#" -ge 20 ] || exit 77
shift 19
[ "$1" = %s ] || { printf 'PID 已被其他进程使用，请刷新后重试\n' >&2; exit 77; }
IFS= read -r comm < "/proc/$pid/comm" || exit 76
[ "$comm" = %s ] || { printf '进程名称已变化，请刷新后重试\n' >&2; exit 77; }
kill -%s "$pid"
`, quote(boot), pid, quote(fmt.Sprint(ticks)), quote(name), signal), nil
}

func Service(unit, action string) (string, error) {
	if !unitName.MatchString(unit) || strings.Contains(unit, "@.service") {
		return "", errors.New("请选择一个具体的 systemd 服务")
	}
	switch action {
	case "start", "stop", "restart", "enable", "disable":
	default:
		return "", errors.New("不支持的服务操作")
	}
	return "systemctl --no-ask-password " + action + " -- " + quote(unit) + "\n", nil
}

// DockerContainer pins both the daemon identity and the full container ID. A
// context change or name reused after recreation cannot redirect the action.
func DockerContainer(socket, engine, id, action string) (string, error) {
	if socket != "/var/run/docker.sock" && !rootlessSocket.MatchString(socket) {
		return "", errors.New("不支持的 Docker 本地接口")
	}
	if !engineID.MatchString(engine) || !containerID.MatchString(id) {
		return "", errors.New("容器或 Docker 身份不完整，请刷新后重试")
	}
	switch action {
	case "start", "stop", "restart":
	default:
		return "", errors.New("不支持的容器操作")
	}
	args := action
	if action != "start" {
		args += " -t 10"
	}
	return fmt.Sprintf(`(
unset DOCKER_HOST DOCKER_CONTEXT DOCKER_TLS_VERIFY DOCKER_CERT_PATH
docker_local() { docker --host %s "$@"; }
actual=$(docker_local info --format '{{.ID}}') || exit 69
[ "$actual" = %s ] || { printf 'Docker 实例已变化，请刷新后重试\n' >&2; exit 78; }
docker_local container %s %s
)
`, quote("unix://"+socket), quote(engine), args, quote(id)), nil
}
