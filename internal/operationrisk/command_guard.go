package operationrisk

import (
	"regexp"
	"strings"
)

// The command guard is a best-effort filter for clearly destructive shell
// commands. It is deliberately not described as a security boundary: quoting,
// variables, encoded payloads, interpreters and unseen input can all evade it.
// Its purpose is to stop the common self-inflicted disasters while leaving
// ordinary read and administration commands usable by an owner-approved AI.
const (
	guardCritical      = `(?:etc|usr|bin|sbin|lib|lib64|boot|proc|sys|dev|root|home)`
	guardCriticalNoDev = `(?:etc|usr|bin|sbin|lib|lib64|boot|proc|sys|root|home)`
	guardBoundary      = `(?:/|["'\s]|$)`
	guardNoSlash       = `(?:["'\s]|$)`
	guardPath          = `["']?(?:/\*` + guardBoundary + `|/` + guardNoSlash + `|/(?:` + guardCritical + `)` + guardBoundary + `|/var` + guardNoSlash + `|/var/` + guardNoSlash + `|/var/lib` + guardBoundary + `)`
	guardPathNoDev     = `["']?(?:/\*` + guardBoundary + `|/` + guardNoSlash + `|/(?:` + guardCriticalNoDev + `)` + guardBoundary + `|/var` + guardNoSlash + `|/var/` + guardNoSlash + `|/var/lib` + guardBoundary + `)`
	guardDisk          = `["']?/dev/(?:sd|nvme|vd|hd|mmcblk|md|dm-|disk|xvd|loop)`
)

type commandGuardRule struct {
	reason  string
	pattern *regexp.Regexp
}

var commandGuardRules = []commandGuardRule{
	{"删除根目录或系统目录", mustGuard(`\brm\b[^|;&]*\s` + guardPath)},
	{"递归删除用户主目录", mustGuard(`\brm\b[^|;&]*\s["']?(?:~|\$home|\$\{home\})(?:/|["'\s]|$)`)},
	{"移动系统关键路径", mustGuard(`\bmv\b[^|;&]*\s` + guardPath)},
	{"删除系统目录中的文件", mustGuard(`\bfind\b[^|;&]*\s` + guardPath + `[^|;&]*(?:-delete|-exec\s+rm)\b`)},
	{"格式化或分区磁盘", mustGuard(`\b(?:mkfs(?:\.[a-z0-9]+)?|mke2fs|wipefs|fdisk|parted|sgdisk|gdisk|blkdiscard)\b`)},
	{"写入磁盘设备", mustGuard(`\bdd\b[^|;&]*\bof=` + guardDisk + `|(?:>|>>)\s*` + guardDisk + `|\b(?:tee|truncate|shred)\b[^|;&]*\s` + guardDisk)},
	{"关机或重启", mustGuard(`\b(?:shutdown|poweroff|halt|reboot|kexec)\b|\bsystemctl\s+(?:poweroff|reboot|halt|kexec|isolate)\b|\b(?:init|telinit)\s+[06]\b`)},
	{"停止 SSH 服务", mustGuard(`\bsystemctl\s+(?:stop|disable|mask)\s+(?:ssh|sshd)\b|\b(?:service|rc-service)\s+(?:ssh|sshd)\s+stop\b|\bkillall\b[^|;&]*\bsshd\b`)},
	{"递归修改系统路径权限", mustGuard(`\b(?:chmod|chown|chgrp)\b[^|;&]*\s-[a-z]*r[a-z]*\b[^|;&]*\s` + guardPath)},
	{"把系统路径权限改为 777 或 000", mustGuard(`\bchmod\s+(?:-[a-z]+\s+)*(?:0?777|0?000)\s+` + guardPath)},
	{"覆盖系统关键文件", mustGuard(`(?:>|>>)\s*` + guardPathNoDev + `|\b(?:tee|truncate|shred)\b[^|;&]*\s` + guardPathNoDev + `|\bsed\s+-i\S*\b[^|;&]*\s` + guardPathNoDev + `|\bperl\s+-[a-z]*i[a-z]*\b[^|;&]*\s` + guardPathNoDev + `|\bdd\b[^|;&]*\bof=` + guardPathNoDev)},
	{"脚本操作系统关键路径", mustGuard(`\b(?:rmtree|unlink|remove|rename|write_text|write_bytes|open)\s*\(\s*` + guardPathNoDev)},
	{"删除账户或修改 root 凭据", mustGuard(`\b(?:userdel|groupdel|visudo)\b|\b(?:passwd|usermod|chpasswd)\b[^|;&]*\broot\b`)},
	{"清空防火墙规则", mustGuard(`\biptables\b[^|;&]*\s(?:-F|--flush|-X|--delete-chain)\b|\bnft\s+flush\s+ruleset\b|\bufw\s+(?:disable|reset)\b`)},
	{"远程脚本直接执行", mustGuard(`\b(?:curl|wget)\b[^|;&]*\|\s*(?:sudo\s+)?(?:ba|z|k|da)?sh\b|\b(?:ba|z|k|da)?sh\b[^|;&]*[<\(]\s*\(?\s*(?:curl|wget)\b|\b(?:ba|z|k|da)?sh\s+-c\b[^|;&]*\$\(\s*(?:curl|wget)\b|\beval\b[^|;&]*\$\(\s*(?:curl|wget)\b`)},
	{"Fork bomb", mustGuard(`:\(\)\s*\{\s*:\s*\|\s*:\s*&\s*\}\s*;\s*:`)},
	{"结束 PID 1 或全部进程", mustGuard(`\bkill\s+-9\s+(?:1|-1)\b|\bkillall5\b`)},
	{"卸载系统关键路径", mustGuard(`\bumount\b[^|;&]*\s` + guardPathNoDev)},
	{"删除数据库或表", mustGuard(`\bdrop\s+(?:database|schema|table)\b|\btruncate\s+table\b`)},
	{"清理 Docker 数据", mustGuard(`\bdocker\s+(?:system\s+prune|volume\s+(?:rm|prune)|image\s+prune\s+-a)\b|\bdocker\s+compose\b[^|;&]*\s(?:down|rm)\b[^|;&]*\s(?:-v|--volumes)\b`)},
}

func mustGuard(expression string) *regexp.Regexp { return regexp.MustCompile("(?i)" + expression) }

// DestructiveCommand returns the guardrail reason for a command, or "" when the
// command does not match a known destructive pattern.
func DestructiveCommand(command string) string {
	command = strings.ToLower(strings.Join(strings.Fields(command), " "))
	// Cheap lexical normalization catches the common duplicate-slash and /.
	// spellings without pretending to resolve shell variables or paths.
	for strings.Contains(command, "//") {
		command = strings.ReplaceAll(command, "//", "/")
	}
	for strings.Contains(command, "/./") {
		command = strings.ReplaceAll(command, "/./", "/")
	}
	for _, rule := range commandGuardRules {
		if rule.pattern.MatchString(command) {
			return rule.reason
		}
	}
	return ""
}
