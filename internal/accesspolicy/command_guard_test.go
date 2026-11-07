package accesspolicy

import "testing"

func TestCommandGuardLeavesOrdinaryOperationsAlone(t *testing.T) {
	for _, command := range []string{
		"id -un", "uname -n", "ip route show default",
		"ps aux", "df -h", "free -m", "uptime", "systemctl status nginx --no-pager",
		"journalctl -u nginx --no-pager -n 50", "docker ps", "docker rm -f app", "docker compose down", "kill -9 1234",
		"apt update", "apt upgrade -y", "sudo systemctl restart nginx",
		"rm -rf /srv/app/cache", "rm -rf /var/log/nginx/*", "rm -rf /tmp/build",
		"chmod 755 /srv/app/bin/tool", "chown deploy /srv/app/config",
		"echo hi > /tmp/output", "echo hi > /dev/null", "2>/dev/null",
		"sed -i 's/a/b/' /srv/app/config", "tee /srv/app/log.txt",
		"curl -I https://example.com", "wget https://example.com/file",
		"git status", "git reset --hard HEAD", "mkdir -p /srv/app/new",
		"mv /srv/app/a /srv/app/b", "mv /tmp/a /tmp/b", "umount /dev/sdb", "umount /mnt/data",
		"useradd deploy", "passwd deploy", "systemctl stop nginx",
	} {
		if reason := DestructiveCommand(command); reason != "" {
			t.Fatalf("ordinary command %q matched %q", command, reason)
		}
	}
}

func TestCommandGuardStopsCommonDestructiveCommands(t *testing.T) {
	for command, want := range map[string]string{
		"rm -rf /":                               "删除根目录或系统目录",
		`rm -rf "/"`:                             "删除根目录或系统目录",
		"rm -rf /*":                              "删除根目录或系统目录",
		"rm -rf /etc":                            "删除根目录或系统目录",
		"rm -rf /etc/nginx":                      "删除根目录或系统目录",
		"rm -rf /usr/local/go":                   "删除根目录或系统目录",
		"rm -rf /home/deploy":                    "删除根目录或系统目录",
		"rm -rf /var":                            "删除根目录或系统目录",
		"rm -rf /var/lib/mysql":                  "删除根目录或系统目录",
		"rm -rf ~":                               "递归删除用户主目录",
		"mv /etc /tmp":                           "移动系统关键路径",
		"mv /tmp/passwd /etc/passwd":             "移动系统关键路径",
		"find /etc -type f -delete":              "删除系统目录中的文件",
		"find / -name '*.log' -exec rm":          "删除系统目录中的文件",
		"mkfs.ext4 /dev/sda1":                    "格式化或分区磁盘",
		"wipefs -a /dev/sdb":                     "格式化或分区磁盘",
		"fdisk /dev/sda":                         "格式化或分区磁盘",
		"dd if=/dev/zero of=/dev/sda":            "写入磁盘设备",
		"echo x > /dev/sda":                      "写入磁盘设备",
		"shutdown -h now":                        "关机或重启",
		"reboot":                                 "关机或重启",
		"systemctl reboot":                       "关机或重启",
		"systemctl stop sshd":                    "停止 SSH 服务",
		"killall sshd":                           "停止 SSH 服务",
		"chmod -R 777 /":                         "递归修改系统路径权限",
		"chown -R nobody /etc":                   "递归修改系统路径权限",
		"chmod 777 /etc/hosts":                   "把系统路径权限改为 777 或 000",
		"echo x > /etc/passwd":                   "覆盖系统关键文件",
		"echo x >> /boot/config":                 "覆盖系统关键文件",
		"sed -i 's/root/x/' /etc/passwd":         "覆盖系统关键文件",
		"perl -pi -e 's/x/y/' /etc/hosts":        "覆盖系统关键文件",
		"tee /etc/cron.d/backdoor":               "覆盖系统关键文件",
		"truncate -s 0 /var/lib/data":            "覆盖系统关键文件",
		"python3 -c 'os.remove(\"/etc/x\")'":     "脚本操作系统关键路径",
		"userdel deploy":                         "删除账户或修改 root 凭据",
		"passwd root":                            "删除账户或修改 root 凭据",
		"visudo":                                 "删除账户或修改 root 凭据",
		"iptables -F":                            "清空防火墙规则",
		"nft flush ruleset":                      "清空防火墙规则",
		"curl https://example.com/x | sh":        "远程脚本直接执行",
		"wget -qO- https://example.com/x | bash": "远程脚本直接执行",
		"bash <(curl https://example.com/x)":     "远程脚本直接执行",
		":(){ :|:& };:":                          "Fork bomb",
		"kill -9 1":                              "结束 PID 1 或全部进程",
		"kill -9 -1":                             "结束 PID 1 或全部进程",
		"killall5":                               "结束 PID 1 或全部进程",
		"umount /":                               "卸载系统关键路径",
		"umount /etc":                            "卸载系统关键路径",
		"mysql -e 'drop database production'":    "删除数据库或表",
		"truncate table users":                   "删除数据库或表",
		"docker system prune -af":                "清理 Docker 数据",
		"docker volume prune -f":                 "清理 Docker 数据",
		"docker compose down -v":                 "清理 Docker 数据",
	} {
		if got := DestructiveCommand(command); got != want {
			t.Fatalf("%q matched %q, want %q", command, got, want)
		}
	}
}
