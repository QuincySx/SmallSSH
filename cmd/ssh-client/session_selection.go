package main

import (
	"fmt"
	"io"

	"sshclient/internal/approval"
	"sshclient/internal/client"
)

// Recognize legacy selections so operation commands fail closed with migration
// guidance. Owner management and session serve must remain available.
func sessionForCommand(args []string, explicit string, flagSet bool, environment string) string {
	if flagSet {
		return explicit
	}
	if len(args) == 0 {
		return ""
	}
	switch args[0] {
	case "exec", "shell", "ls", "get", "put", "replace", "monitor", "docker":
		return environment
	case "transfer":
		if len(args) > 1 && args[1] == "resume" {
			return environment
		}
	}
	// In particular an expired client environment must not prevent status,
	// revoke, new session requests or unrelated owner-management commands.
	return ""
}

func writeApprovalNotice(w io.Writer, r approval.Request) {
	if r.Status != "pending" {
		return
	}
	fmt.Fprintf(w, "%s\n单次请求已进入授权中心，也可由人类终端执行 smallssh approve %s。连续操作请运行 smallssh exec 连接ID，复用同一进程的 session 会话授权。\n", client.Describe(r), r.ID)
}
