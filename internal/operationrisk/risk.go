// Package operationrisk classifies operations for an explicitly guarded session.
// Its command rules are conservative automation hints, not a remote sandbox.
package operationrisk

import (
	"path"
	"strings"
)

type Level uint8

const (
	Low Level = iota
	Medium
	High
)

type Assessment struct {
	Level  Level
	Reason string
}

// Assess never treats absence from the destructive filter as evidence of low
// risk. The default bypass is limited to fixed queries and typed reads.
func Assess(kind, command, target, destination string) Assessment {
	switch kind {
	case "put", "put-resume", "replace", "mkdir", "remove", "rename", "chmod", "file-attributes":
		for _, candidate := range []string{target, destination} {
			if criticalMutation(candidate) {
				return Assessment{High, "修改系统关键路径"}
			}
		}
	}
	switch kind {
	case "list", "file-info", "get", "get-resume", "monitor", "docker-logs", "compose-plan", "upgrade-preview":
		return Assessment{Low, "范围内的读取操作"}
	case "shell", "docker-terminal", "tunnel":
		return Assessment{High, "交互通道无法逐项检查"}
	case "exec":
		if reason := DestructiveCommand(command); reason != "" {
			return Assessment{High, reason}
		}
		if MaintenanceQuery(command) {
			return Assessment{Low, "固定只读查询"}
		}
		return Assessment{Medium, "命令需要核对"}
	default:
		return Assessment{Medium, "操作会改变远端状态"}
	}
}

// Only bounded SFTP mutations can use the owner's medium-risk opt-out.
// The broker additionally rejects aliases when resolving these paths.
func ReviewOptionalMutation(kind, target, destination string) bool {
	switch kind {
	case "mkdir", "put", "replace", "rename", "remove":
	default:
		return false
	}
	if !path.IsAbs(target) || path.Clean(target) != target || criticalMutation(target) {
		return false
	}
	return kind != "rename" || path.IsAbs(destination) && path.Clean(destination) == destination && !criticalMutation(destination)
}

func fixedQuery(command string) bool {
	// Do not normalize shell syntax, newlines, quoting, expansions or pipelines
	// into a recognized query. Unknown syntax always requires human review.
	if strings.ContainsAny(command, "\r\n\x00;'\"`$\\|&<>(){}*?[]!#") {
		return false
	}
	for _, char := range command {
		if char >= 127 || char < 32 && char != '\t' {
			return false
		}
	}
	words := strings.Fields(command)
	if len(words) == 0 {
		return false
	}
	switch strings.Join(words, " ") {
	case "pwd", "whoami", "uptime", "uptime -p", "uptime -s", "hostname", "hostname -s", "hostname -f", "hostname -I",
		"ps", "ps aux", "ps -ef", "ip route", "ip route show", "ip route show default", "ip -brief address",
		"docker ps", "docker ps -a", "docker images", "docker stats --no-stream":
		return true
	}
	flags := ""
	switch words[0] {
	case "uname":
		flags = "asnrvmipo"
	case "id":
		flags = "ugGnrz"
	case "df":
		flags = "hHikmPT"
	case "free":
		flags = "bhkmgw"
	default:
		return false
	}
	for _, option := range words[1:] {
		if !strings.HasPrefix(option, "-") || len(option) < 2 {
			return false
		}
		for _, flag := range option[1:] {
			if !strings.ContainsRune(flags, flag) {
				return false
			}
		}
	}
	return true
}

// MaintenanceQuery accepts fixed inspection commands only. Do not admit shell
// composition, user-selected flags, file paths, or a general package command.
func MaintenanceQuery(command string) bool {
	switch command {
	case "cat /etc/os-release", "systemctl --no-pager --plain list-units --type=service":
		return true
	default:
		return fixedQuery(command)
	}
}

// This lexical guard is additional to the server's configurable protected roots
// and remote canonical-path checks. It cannot resolve remote aliases itself.
func criticalMutation(target string) bool {
	if !path.IsAbs(target) {
		return false
	}
	target = path.Clean(target)
	if target == "/" {
		return true
	}
	for _, root := range []string{"/etc", "/boot", "/usr", "/bin", "/sbin", "/lib", "/lib64", "/proc", "/sys", "/dev", "/root", "/home", "/var/lib"} {
		if target == root || strings.HasPrefix(target, root+"/") || strings.HasPrefix(root, target+"/") {
			return true
		}
	}
	return false
}
