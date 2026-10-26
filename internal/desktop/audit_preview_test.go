//go:build preview_qa && terminal_integration

package desktop

import (
	"context"
	"fmt"
	"image/png"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/egoist/mygo/plugins/terminal"
	"github.com/egoist/mygo/ui"
	"sshclient/internal/approval"
	"sshclient/internal/monitor"
	"sshclient/internal/sshconn"
	"sshclient/internal/systemauth"
	"sshclient/internal/transfer"
	"sshclient/internal/vault"
)

// Offline native rendering only. No App.Run, native windows, authentication,
// network connections, filesystem browsing, or production vault access.
func TestAuditedWorkspaceSnapshotsWithoutWindows(t *testing.T) {
	if os.Getenv("SSH_CLIENT_PREVIEW_QA") != "1" {
		t.Skip("opt-in offline review")
	}
	dir := os.Getenv("SSH_CLIENT_UI_SNAPSHOT_DIR")
	if !filepath.IsAbs(dir) {
		t.Fatal("absolute output required")
	}
	if err := os.MkdirAll(dir, 0700); err != nil {
		t.Fatal(err)
	}
	for _, variant := range []string{"workspace", "standard-layout", "standard-tools-hidden", "standard-both-hidden", "terminal-split-files", "terminal-split-status", "terminal-groups", "terminal-groups-dark", "terminal-groups-small-right", "dock-menu", "dock-menu-right", "dock-menu-dark", "dock-menu-small", "files-horizontal", "files-vertical", "files-grid", "files-grid-docked", "files-grid-dark", "files-nested", "dark", "sidebar-hidden-dark", "right", "three-column", "three-column-hidden", "sidebar-hidden", "status", "processes", "services", "transfers", "transfers-dark", "transfers-empty", "transfers-many", "approval", "security", "prompt", "empty", "empty-dark", "empty-sidebar-hidden-dark", "small", "files-pinned", "files-choice"} {
		t.Run(variant, func(t *testing.T) {
			recorder := newTerminalRecorder()
			colors := terminal.DarkTheme()
			colors.Background = ui.Hex("#161b22")
			colors.Foreground = ui.Hex("#d2dce9")
			term, err := terminal.New(terminal.Options{Conn: recorder, Theme: colors, DarkTheme: colors, Scrollback: 32 << 20, ClipboardRead: terminal.ClipboardDeny, NoClipboardWrite: true})
			if err != nil {
				t.Fatal(err)
			}
			defer term.Close()
			a := &app{ctx: context.Background(), online: true, page: 1, groups: map[string]*bool{}, selected: "demo", sidebarWidth: 196, terminalShare: 68, terminalWidthShare: 65}
			a.connections = []vault.Summary{{ID: "demo", Name: "演示服务器", Group: "个人服务", Host: "192.0.2.10", Port: 22, User: "root"}, {ID: "staging", Name: "开发环境", Group: "工作", Host: "192.0.2.20", Port: 2222, User: "deploy"}}
			a.sessions = []*session{{host: "demo", name: "演示服务器", term: term, stats: &monitor.Stats{Time: time.Now(), CPU: 3.8, MemoryUsed: 234 << 20, MemoryTotal: 512 << 20, Load: "0.03 0.07 0.04", Uptime: 237482, RXPerSecond: 2048, TXPerSecond: 860, Disks: []monitor.Disk{{Mount: "/", Total: 20 << 30, Used: 4 << 30, Free: 16 << 30}}, Processes: []monitor.Process{{PID: 812, Name: "sshd", CPU: 1.2, Memory: 1.3, RSS: 7 << 20}, {PID: 445, Name: "nginx", CPU: 0.4, Memory: 0.8, RSS: 4 << 20}}, Inventory: &monitor.Inventory{OS: "Debian GNU/Linux 12", Architecture: "x86_64", Kernel: "6.1.0", Model: "Cloud Server", CPU: "Virtual CPU", Cores: 1, Hostname: "demo", Manager: "systemd", Services: []monitor.Service{{Name: "nginx.service", Enabled: "enabled", Active: "active", Sub: "running"}, {Name: "frps.service", Enabled: "enabled", Active: "active", Sub: "running"}}}}, monitorStatus: "自动更新"}}
			a.ensurePanes()
			a.primaryFilePane = newFilePane("demo", true)
			b := a.primaryFilePane.browser()
			b.dir = "/srv/site"
			b.address = b.dir
			b.cache[b.dir] = []sshconn.File{{Name: "assets", Directory: true}, {Name: "index.html", Size: 2048, Mode: "-rw-r--r--"}, {Name: "nginx.conf", Size: 876, Mode: "-rw-r--r--"}, {Name: "README.md", Size: 1240, Mode: "-rw-r--r--"}}
			b.reveal(false, b.dir)
			b.indexDirectory(false, b.dir)
			switch variant {
			case "terminal-split-files", "terminal-split-status", "terminal-groups", "terminal-groups-dark", "terminal-groups-small-right":
				count := 2
				if variant == "terminal-groups" || variant == "terminal-groups-dark" {
					count = 4
				}
				for i := 1; i < count; i++ {
					other, err := terminal.New(terminal.Options{Conn: newTerminalRecorder(), Theme: colors, DarkTheme: colors, ClipboardRead: terminal.ClipboardDeny, NoClipboardWrite: true})
					if err != nil {
						t.Fatal(err)
					}
					defer other.Close()
					other.Feed([]byte("\x1b[32mdeploy@staging\x1b[0m:/var/www$ uptime\r\n 12:15:42 up 7 days,  load average: 0.04, 0.06, 0.03\r\n\r\n\x1b[32mdeploy@staging\x1b[0m:/var/www$ "))
					name := "开发环境"
					if i > 1 {
						name = fmt.Sprintf("终端 %d", i+1)
					}
					a.sessions = append(a.sessions, &session{host: "staging", name: name, term: other, stats: &monitor.Stats{Time: time.Now(), CPU: 6.2, MemoryTotal: 2 << 30, MemoryUsed: 658 << 20, Load: "0.04 0.06 0.03", Uptime: 7 * 86400, Disks: []monitor.Disk{{Mount: "/", Total: 40 << 30, Used: 8 << 30}}, Inventory: &monitor.Inventory{OS: "Debian GNU/Linux 12", Architecture: "x86_64", Cores: 2}}, monitorStatus: "自动更新"})
				}
				a.ensureTerminalGroups()
				a.splitExistingTerminal(a.sessions[1])
				if count == 4 {
					a.moveTerminalTab(a.sessions[3], a.terminalLayout.right, nil)
				}
				a.activateTerminal(a.sessions[1], true)
				a.syncFollowingFilePanes()
				other := a.primaryFilePane.browser()
				other.dir, other.address = "/var/www", "/var/www"
				other.cache[other.dir] = []sshconn.File{{Name: "public", Directory: true}, {Name: "index.html", Size: 4096}, {Name: "app.conf", Size: 852}}
				other.reveal(false, other.dir)
				if variant == "terminal-split-status" {
					a.dock = toolDiagnostics
				}
				if variant == "terminal-groups-small-right" {
					a.toolsRight, a.sidebarWidth = true, 390
				}
			case "files-horizontal", "files-vertical", "files-grid", "files-grid-docked", "files-grid-dark", "files-nested":
				a.fileExpanded = true
				mode := "horizontal"
				if variant == "files-vertical" {
					mode = "vertical"
				} else if variant == "files-grid" || variant == "files-grid-dark" || variant == "files-grid-docked" {
					mode = "grid"
				}
				a.arrangeFilePanes(mode)
				if variant == "files-grid-docked" {
					a.fileExpanded, a.terminalShare = false, 42
				}
				if variant == "files-nested" {
					a.splitFilePane(a.fileLayout.panes()[1], true, false)
				}
				for i, p := range a.fileLayout.panes() {
					if i == 0 {
						continue
					}
					a.chooseFileEndpoint(p, "staging")
					other := p.browser()
					other.dir = []string{"/srv/site", "/var/www", "/etc/nginx", "/srv/backups"}[min(i, 3)]
					other.address = other.dir
					other.cache[other.dir] = []sshconn.File{{Name: "config", Directory: true}, {Name: "index.html", Size: 2048, Mode: "-rw-r--r--"}, {Name: "notes.md", Size: 768, Mode: "-rw-r--r--"}}
					other.reveal(false, other.dir)
				}
			case "files-pinned":
				a.chooseFileEndpoint(a.primaryFilePane, "staging")
				other := a.primaryFilePane.browser()
				other.dir = "/var/www"
				other.address = other.dir
				other.cache[other.dir] = []sshconn.File{{Name: "index.html", Size: 2048, Mode: "-rw-r--r--"}}
			case "right", "dock-menu-right":
				a.toolsRight = true
			case "three-column", "three-column-hidden":
				a.toolsRight, a.terminalWidthShare = true, 60
			case "status":
				a.dock = toolDiagnostics
			case "processes":
				a.dock = toolDiagnostics
				a.diagnosticTab = 1
			case "services":
				a.dock = toolDiagnostics
				a.diagnosticTab = 2
			case "transfers-empty":
				a.showTransfers()
			case "transfers", "transfers-dark", "transfers-many":
				a.showTransfers()
				a.jobs = []transfer.Job{{ID: "sample", Host: "demo", Direction: "upload", Remote: "/srv/site/index.html", Local: "/example/index.html", Size: 20 << 20, Bytes: 12 << 20, Speed: 1 << 20, State: "running"}, {ID: "sample2", Host: "demo", Direction: "download", Remote: "/srv/backup.zip", Size: 2 << 20, Bytes: 2 << 20, State: "done"}}
				if variant == "transfers-many" {
					for n := range 20 {
						a.jobs = append(a.jobs, transfer.Job{ID: fmt.Sprint("queued-", n), Host: "demo", Direction: "upload", Remote: fmt.Sprintf("/srv/site/file-%02d.txt", n), Size: 1 << 20, State: "queued"})
					}
				}
			case "approval":
				a.page = 3
				a.requests = []approval.Request{{ID: "example-external", Status: "pending", Expires: time.Now().Add(5 * time.Minute), Operation: approval.Operation{Kind: "exec", Host: "demo", Endpoint: "root@192.0.2.10:22", Client: "CLI / AI 工具", Command: "systemctl status nginx --no-pager", HostFingerprint: "SHA256:EXAMPLE_FINGERPRINT"}}}
				a.selectedRequest = a.requests[0].ID
			case "security":
				a.page = 4
				a.authStatus = systemauth.Status{Enabled: true, Available: true, Name: "Touch ID"}
			case "prompt":
				a.ask("新建文件夹", "演示服务器 · /srv/site", "", "文件夹名称", "../assets", "创建", func(string) {})
				a.prompt.error = "请输入有效名称，不要包含路径分隔符"
			case "empty", "empty-dark", "empty-sidebar-hidden-dark":
				a.sessions = nil
				a.connections = nil
				a.selected = ""
				a.toolsHidden = true
			}
			width, height := 1280, 900
			if variant == "small" || variant == "dock-menu-small" || variant == "terminal-groups-small-right" {
				width, height = 1000, 760
			}
			tt := ui.NewTester(a.view, width, height)
			tt.SetDark(strings.Contains(variant, "dark"))
			term.Feed([]byte("\x1b[32mroot@demo\x1b[0m:~# systemctl status nginx --no-pager\r\n\r\n  nginx.service - A high performance web server\r\n     Loaded: loaded; enabled\r\n     Active: \x1b[32mactive (running)\x1b[0m\r\n   Main PID: 445 (nginx)\r\n     Memory: 4.0M\r\n\r\n\x1b[32mroot@demo\x1b[0m:~# "))
			tt.Frame()
			if variant == "dock-menu" || variant == "dock-menu-right" || variant == "dock-menu-dark" || variant == "dock-menu-small" {
				if err := tt.Click("工具面板布局"); err != nil {
					t.Fatal(err)
				}
			}
			if variant == "standard-tools-hidden" || variant == "standard-both-hidden" {
				if err := tt.Click("隐藏工具面板"); err != nil {
					t.Fatal(err)
				}
			}
			if variant == "standard-both-hidden" {
				if err := tt.Click("收窄连接侧栏"); err != nil {
					t.Fatal(err)
				}
			}
			if variant == "sidebar-hidden" || variant == "three-column-hidden" || variant == "sidebar-hidden-dark" || variant == "empty-sidebar-hidden-dark" {
				if err := tt.Click("收窄连接侧栏"); err != nil {
					t.Fatal(err)
				}
			}
			if variant == "files-choice" {
				if err := tt.Click("文件位置"); err != nil {
					t.Fatal(err)
				}
				tt.Frame()
			}
			f, err := os.Create(filepath.Join(dir, "audit-"+variant+".png"))
			if err != nil {
				t.Fatal(err)
			}
			err = png.Encode(f, tt.Image())
			closeErr := f.Close()
			if err != nil || closeErr != nil {
				t.Fatal(err, closeErr)
			}
		})
	}
}
