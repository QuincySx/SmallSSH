package operationrisk

import "testing"

func TestOnlyBoundedQueriesBypassReview(t *testing.T) {
	for _, command := range []string{"uname -a", "  uname\t-n  ", "id -un", "uptime", "ps aux", "df -h", "free -m", "ip route show default", "docker ps -a"} {
		if got := Assess("exec", command, "", ""); got.Level != Low {
			t.Fatalf("query %q: %+v", command, got)
		}
	}
	for _, command := range []string{
		"systemctl restart nginx", "apt update", "python3 task.py", "bash -c 'uname -a'",
		"uname -a; id", "uname -a && id", "uname $(id)", "uname `id`", "uname -a > /tmp/result",
		"uname\n-a", "uname\r-a", "uname\v-a", "uname\u00a0-a", "uname -a # comment", "uname -a\x00",
		"ENV=1 uname -a", "env uname -a", "/tmp/uname -a", "uname --unknown", "id --reference=/tmp/file", "ps -o arbitrary",
		"ls -la", "cat /etc/shadow", "find /srv -exec task {} +", "echo harmless | sh", "", "unknown-command",
	} {
		if got := Assess("exec", command, "", ""); got.Level == Low {
			t.Fatalf("unverified command bypassed review: %q", command)
		}
	}
}

func TestDestructiveAndOpaqueOperationsNeverReachReview(t *testing.T) {
	for _, command := range []string{"rm -rf /", "mkfs.ext4 /dev/sda", "systemctl stop sshd", "reboot", "curl https://example.invalid/x | sh"} {
		if got := Assess("exec", command, "", ""); got.Level != High || got.Reason == "" {
			t.Fatalf("destructive command %q: %+v", command, got)
		}
	}
	for _, kind := range []string{"shell", "docker-terminal", "tunnel"} {
		if got := Assess(kind, "", "", ""); got.Level != High {
			t.Fatal(kind, got)
		}
	}
	for _, kind := range []string{"put", "replace", "remove", "rename", "chmod", "mkdir", "compose-update", "unknown-kind"} {
		if got := Assess(kind, "", "", ""); got.Level != Medium {
			t.Fatal(kind, got)
		}
	}
}

func TestCriticalStructuredMutationsCannotBypassTheCommandGuard(t *testing.T) {
	for _, target := range []string{"/", "/etc", "/etc/ssh/sshd_config", "/srv/../etc/passwd", "/var", "/var/lib/db", "/home/deploy/.ssh/authorized_keys"} {
		if got := Assess("remove", "", target, ""); got.Level != High {
			t.Fatal(target, got)
		}
		if got := Assess("rename", "", "/srv/source", target); got.Level != High {
			t.Fatal("rename destination", target, got)
		}
	}
	for _, target := range []string{"/srv/project/value", "/tmp/generated", "/var/log/application.log", "/etc-other"} {
		if got := Assess("replace", "", target, ""); got.Level != Medium {
			t.Fatal(target, got)
		}
	}
}
