package main

import (
	"bytes"
	"strings"
	"testing"

	"sshclient/internal/approval"
)

func TestClientSessionEnvironmentIsExplicitAndLimitedToOperations(t *testing.T) {
	for _, command := range [][]string{{"exec"}, {"shell"}, {"ls"}, {"get"}, {"put"}, {"replace"}, {"monitor"}, {"docker", "logs"}, {"transfer", "resume"}} {
		if got := sessionForCommand(command, "", false, "my-ai.json"); got != "my-ai.json" {
			t.Fatal("operation did not reuse explicit client session", command, got)
		}
		if got := sessionForCommand(command, "other-ai.json", true, "my-ai.json"); got != "other-ai.json" {
			t.Fatal("explicit flag lost precedence", command, got)
		}
		if got := sessionForCommand(command, "", true, "my-ai.json"); got != "" {
			t.Fatal("explicit empty flag did not opt out", command, got)
		}
	}
	for _, command := range [][]string{nil, {"help"}, {"session", "request"}, {"session", "status"}, {"session", "revoke"}, {"init"}, {"agent"}, {"host", "add"}, {"hosts"}, {"approve"}, {"pending"}, {"transfer", "list"}, {"transfer", "pause"}} {
		if got := sessionForCommand(command, "", false, "expired-session.json"); got != "" {
			t.Fatal("environment interfered with session recovery or owner management", command)
		}
	}
}

func TestApprovedOperationsDoNotPrintAnotherApprovalRequest(t *testing.T) {
	for _, status := range []string{"approved", "running", "finished", "denied", "revoked"} {
		var out bytes.Buffer
		writeApprovalNotice(&out, approval.Request{Status: status})
		if out.Len() != 0 {
			t.Fatal("non-pending operation printed approval instructions", status, out.String())
		}
	}
	var out bytes.Buffer
	writeApprovalNotice(&out, approval.Request{ID: "one-request", Status: "pending"})
	if !strings.Contains(out.String(), "已进入授权中心") || !strings.Contains(out.String(), "session 会话授权") {
		t.Fatal("one-time request did not explain session reuse", out.String())
	}
}
