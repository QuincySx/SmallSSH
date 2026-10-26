//go:build preview_qa && terminal_integration

package desktop

import (
	"fmt"
	"image/png"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/egoist/mygo/plugins/terminal"
	"github.com/egoist/mygo/ui"
	"sshclient/internal/monitor"
	"sshclient/internal/sshconn"
	"sshclient/internal/vault"
)

func detailedStatusDemo() *app {
	a := managementApp()
	a.sessions = a.sessions[:1]
	a.connections = []vault.Summary{{ID: "fixture", Name: "演示服务器", Host: "192.0.2.10", Port: 22, User: "root", Group: "演示环境"}}
	a.groups = map[string]*bool{}
	a.sidebarWidth = 190
	a.terminalShare = 64
	a.terminalWidthShare = 62
	a.sessions[0].name = "演示服务器"
	a.showTool(toolDiagnostics)
	s := a.sessions[0].stats
	s.MemoryTotal = 24 << 30
	s.MemoryUsed = 1600 << 20
	s.MemoryAvailable = 22 << 30
	s.MemoryCached = 4300 << 20
	s.MemoryFree = 18 << 30
	s.Uptime = 7 * 86400
	s.Load = "0.10 / 0.20 / 0.30"
	s.CPU = 5
	s.CPUDetail = monitor.CPUUsage{Name: "cpu", Used: 5, User: 3, System: 2, Valid: true}
	for n := range 16 {
		s.Cores = append(s.Cores, monitor.CPUUsage{Name: fmt.Sprintf("cpu%d", n), Used: float64(n%5) * 3, User: float64(n%5) * 2, System: float64(n % 5), Valid: true})
	}
	s.Networks = []monitor.Network{{Name: "eth0", RX: 84 << 30, TX: 532 << 20, RXRate: 201 << 10, TXRate: 23 << 10, Valid: true}, {Name: "br-demo", RX: 349 << 20, TX: 84 << 30, RXRate: 652, TXRate: 208 << 10, Valid: true}, {Name: "lo", RX: 4 << 20, TX: 4 << 20, Valid: true}}
	s.DefaultNetwork = "eth0"
	s.TCP = monitor.TCPInfo{ActiveOpens: 1381, PassiveOpens: 451, Established: 44, Available: true, RateValid: true}
	s.Inventory.OS = "Debian GNU/Linux 13"
	s.Inventory.Cores = 16
	s.Inventory.Architecture = "x86_64"
	s.Inventory.Interfaces = []monitor.Interface{{Name: "eth0", State: "up", Addresses: []string{"192.0.2.10/24"}}, {Name: "br-demo", State: "up", Virtual: true, Addresses: []string{"172.19.0.1/16"}}, {Name: "lo", State: "unknown", Virtual: true, Addresses: []string{"127.0.0.1/8", "::1/128"}}}
	d := s.Inventory.Docker
	d.Containers[0].Labels = "com.docker.compose.project=personal"
	s.DockerStats = &monitor.DockerSnapshot{Time: time.Now(), EngineID: d.EngineID, Socket: d.Socket, Status: "ready", Rows: []monitor.ContainerStats{{ID: d.Containers[0].ID, CPU: 3.2, Memory: 205 << 20, Limit: 24 << 30, MemoryPercent: .83, Read: 606 << 20, Write: 38 << 30, RX: 21 << 30, TX: 100 << 20, CPUValid: true, MemoryValid: true, BlockValid: true, NetworkValid: true}}}
	s.Disks = []monitor.Disk{{Mount: "/", Device: "overlayfs:/overlay", Filesystem: "overlay", Total: 74 << 20, Used: 70 << 20, Free: 4 << 20, ModeKnown: true}, {Mount: "/overlay", Device: "/dev/ubi0_2", Filesystem: "ubifs", Total: 74 << 20, Used: 70 << 20, Free: 4 << 20, ModeKnown: true}, {Mount: "/rom", Device: "/dev/root", Filesystem: "squashfs", Total: 16 << 20, Used: 16 << 20, ReadOnly: true, ModeKnown: true}}
	now := s.Time
	for n := 0; n < 120; n++ {
		copy := *s
		copy.Time = now.Add(-time.Duration(120-n) * 2 * time.Second)
		copy.CPU = float64(n%20) + 2
		copy.CPUDetail.Valid = true
		copy.Networks = append([]monitor.Network(nil), s.Networks...)
		copy.Networks[0].RXRate = float64((n%17)+120) * 1024
		copy.Networks[0].TXRate = float64((n%13)+15) * 1024
		a.sessions[0].history.add(&copy)
	}
	a.sessions[0].history.add(s)

	a.sessions[0].monitorStatus = "自动更新"
	return a
}

// Render the actual native UI offscreen, without opening windows or reading a vault.
func TestManagementResourceSnapshotsWithoutWindows(t *testing.T) {
	if os.Getenv("SSH_CLIENT_PREVIEW_QA") != "1" {
		t.Skip("offline preview opt in")
	}
	dir := os.Getenv("SSH_CLIENT_UI_SNAPSHOT_DIR")
	if !filepath.IsAbs(dir) {
		t.Fatal("absolute path required")
	}
	if err := os.MkdirAll(dir, 0700); err != nil {
		t.Fatal(err)
	}
	for _, variant := range []string{"overview", "cpu", "network", "docker", "docker-dark", "docker-list", "compose", "processes", "services", "confirm", "right", "mounts", "connection-dialog", "connection-dialog-dark", "connection-key", "connection-identity"} {
		t.Run(variant, func(t *testing.T) {
			a := detailedStatusDemo()
			a.fileExpanded = true
			term, err := terminal.New(terminal.Options{Conn: newTerminalRecorder(), Theme: terminal.DarkTheme(), DarkTheme: terminal.DarkTheme(), ClipboardRead: terminal.ClipboardDeny, NoClipboardWrite: true})
			if err != nil {
				t.Fatal(err)
			}
			defer term.Close()
			a.sessions[0].term = term
			term.Feed([]byte("root@demo:~# \r\n"))
			switch variant {
			case "connection-dialog", "connection-dialog-dark", "connection-key", "connection-identity":
				a.fileExpanded, a.toolsHidden = false, true
				a.newConnection()
				a.form.Name, a.form.Host, a.form.User, a.form.Group = "个人服务器", "192.0.2.20", "root", "个人服务"
				term.Feed([]byte("systemctl status nginx --no-pager\r\n\r\n  nginx.service - A high performance web server\r\n     Loaded: loaded; enabled\r\n     Active: active (running)\r\n   Main PID: 445 (nginx)\r\n\r\nroot@demo:~# "))
				if variant == "connection-key" {
					a.form.Auth, a.connection.authLabel = "key", "已有私钥"
				}
				if variant == "connection-identity" {
					a.connection.identity = &sshconn.HostIdentity{Host: a.form.Host, Port: 22, Algorithm: "ssh-ed25519", Fingerprint: "SHA256:w1H85gE9C7qRE4zP0O4Wp5vM7Tg5J2Gr8RcbX6cVxfk"}
					a.connection.review = true
				}
			case "mounts":
				a.statusMetric = "磁盘"
			case "cpu":
				a.statusMetric = "CPU"
			case "network":
				a.statusMetric = "网络"
				a.managementState().virtualOpen = true
			case "docker", "docker-dark", "docker-list":
				a.diagnosticTab = 4
				a.managementState().containerList = variant == "docker-list"
			case "compose":
				a.diagnosticTab = 4
				a.managementState().dockerTab = 2
			case "processes":
				a.diagnosticTab = 1
			case "services":
				a.diagnosticTab = 2
			case "confirm":
				a.diagnosticTab = 1
				a.confirmProcess(a.sessions[0].stats.Processes[0], a.sessions[0].stats.BootID, "TERM")
			case "right":
				a.fileExpanded = false
				a.toolsRight = true
				a.diagnosticTab = 4
			}
			tt := ui.NewTester(a.view, 1280, 900)
			tt.SetDark(variant == "docker-dark" || variant == "connection-dialog-dark")
			f, err := os.Create(filepath.Join(dir, "management-"+variant+".png"))
			if err != nil {
				t.Fatal(err)
			}
			err = png.Encode(f, tt.Image())
			closeErr := f.Close()
			if err != nil {
				t.Fatal(err)
			}
			if closeErr != nil {
				t.Fatal(closeErr)
			}
		})
	}
}
