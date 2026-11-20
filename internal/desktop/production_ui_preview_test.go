//go:build preview_qa && terminal_integration

package desktop

import (
	"context"
	"encoding/json"
	"github.com/egoist/mygo/plugins/terminal"
	"github.com/egoist/mygo/ui"
	"github.com/egoist/mygo/ui/editor"
	"image/png"
	"os"
	"path/filepath"
	"sshclient/internal/systemauth"
	"testing"
)

// These images render the shipping views with synthetic, cached data. They do
// not use the earlier alva_prototype tag, real credentials, OS windows or SSH.
func TestProductionWorkspaceSnapshotsWithoutWindows(t *testing.T) {
	if os.Getenv("SSH_CLIENT_PREVIEW_QA") != "1" {
		t.Skip("opt-in offline UI")
	}
	directory := os.Getenv("SSH_CLIENT_UI_SNAPSHOT_DIR")
	base, _ := filepath.Abs("../../build")
	rel, err := filepath.Rel(base, directory)
	if err != nil || !filepath.IsAbs(directory) || rel == ".." || len(rel) > 3 && rel[:3] == "../" {
		t.Fatal("output must be inside build")
	}
	if err := os.MkdirAll(directory, 0700); err != nil {
		t.Fatal(err)
	}
	for _, dark := range []bool{false, true} {
		for _, screen := range []string{"workspace", "readme-workspace", "unlock", "unlock-system", "unlock-expanded", "vault-create", "connection", "connection-narrow", "connection-hardware", "connection-hardware-narrow", "settings", "settings-security", "settings-presence", "canvas-home", "canvas-mixed", "canvas-grouped", "canvas-tabs", "canvas-bottom", "canvas-right", "canvas-small", "files-tab", "workspace-four", "workspace-six", "dock-left", "dock-top", "protected-paths", "protected-paths-narrow", "editor", "editor-loading", "ports"} {
			name := screen + "-light"
			if dark {
				name = screen + "-dark"
			}
			t.Run(name, func(t *testing.T) {
				a := filePresentationDemo()
				a.ctx = context.Background()
				a.toolsDetached = false
				a.page = 1
				a.sidebarWidth = 204
				lightColors, darkColors := workspaceTerminalColors()
				term, err := terminal.New(terminal.Options{Conn: newTerminalRecorder(), Theme: lightColors, DarkTheme: darkColors, Padding: workspaceTerminalPadding(), ClipboardRead: terminal.ClipboardDeny, NoClipboardWrite: true})
				if err != nil {
					t.Fatal(err)
				}
				defer term.Close()
				a.sessions[0].term = term
				term.Feed([]byte("demo@example:~$ uname -a\r\nLinux example 6.12.0 x86_64 GNU/Linux\r\ndemo@example:~$ "))
				width, height := 1280, 860
				view := a.view
				switch screen {
				case "readme-workspace":
					width, height = 1280, 700
					a.connections[0].Name, a.connections[0].Group = "Web 生产环境", "生产环境"
					a.connections[1].Name, a.connections[1].Group = "Staging", "测试环境"
					a.connections[2].Name, a.connections[2].Group = "Backup", "生产环境"
					a.sessions[0].name, a.sessions[1].name = "Web 生产环境", "Staging"
					sample := detailedStatusDemo().sessions[0]
					a.sessions[0].stats = sample.stats
					a.sessions[0].history = sample.history
					a.sessions[0].monitorStatus = sample.monitorStatus
					a.setToolsDockSide("right")
					a.dock = toolDiagnostics
					term.Feed([]byte("\x1b[2J\x1b[Hops@web:~$ uptime\r\n" +
						" 13:42:28 up 7 days, load average: 0.10, 0.20, 0.30\r\n\r\n" +
						"ops@web:~$ systemctl status nginx --no-pager\r\n" +
						"● nginx.service - A high performance web server\r\n" +
						"     Loaded: loaded (/usr/lib/systemd/system/nginx.service; enabled)\r\n" +
						"     Active: \x1b[32mactive (running)\x1b[0m\r\n" +
						"   Main PID: 445 (nginx)\r\n\r\n" +
						"ops@web:~$ df -h /\r\n" +
						"Filesystem      Size  Used Avail Use% Mounted on\r\n" +
						"/dev/vda1        64G   18G   43G  30% /\r\n\r\nops@web:~$ "))
				case "connection", "connection-narrow", "connection-hardware", "connection-hardware-narrow":
					a.newConnection()
					a.form.Name, a.form.Host, a.form.User = "示例服务器", "192.0.2.10", "demo"
					if screen == "connection-hardware" || screen == "connection-hardware-narrow" {
						a.form.Auth = "pkcs11"
						a.connection.authLabel = authLabel(a.form.Auth)
						a.form.Hardware.Module = "/example/pkcs11/provider.dylib"
						a.form.Hardware.TokenSerial = "EXAMPLE0001"
					}
					if screen == "connection-narrow" || screen == "connection-hardware-narrow" {
						width, height = 560, 760
					}
				case "unlock", "unlock-system", "unlock-expanded", "vault-create":
					a = &app{ctx: context.Background(), create: screen == "vault-create"}
					if screen == "unlock-system" || screen == "unlock-expanded" {
						a.authStatus = systemauth.Status{Enabled: true, Available: true, Name: "Touch ID"}
					}
					if screen == "unlock-expanded" {
						a.ensureUnlockState().passwordMode = true
					}
					view = a.view
				case "settings", "settings-security", "settings-presence":
					width, height = 1080, 760
					a.settingsOpen = true
					if screen == "settings-security" {
						a.settingsSection, height = settingsSecurity, 860
					}
					if screen == "settings-presence" {
						a.settingsSection = settingsPresence
						var events []string
						fakePresence(a, &events)
					}
					view = a.settingsWindowView
				case "canvas-home":
					fixture, _ := terminalSplitFixture(t, 1)
					a.sessions = fixture.sessions
					a.setWorkspaceCanvas(true)
					a.toolsHidden, a.dock = false, toolFiles
				case "canvas-mixed", "canvas-grouped", "canvas-tabs", "canvas-bottom", "canvas-right", "canvas-small":
					fixture, _ := terminalSplitFixture(t, 2)
					a.sessions = fixture.sessions
					a.openWorkspaceTool(toolFiles)
					a.openWorkspaceTool(toolDiagnostics)
					a.setWorkspaceCanvas(true)
					a.toolsHidden = true
					width, height = 1440, 960
					if screen == "canvas-grouped" {
						a.dropWorkspaceTab(a.workspaceTabPayload(workspaceTab{session: a.sessions[1]}), a.sessions[0].group, workspaceTab{}, "")
					}
					if screen == "canvas-tabs" {
						a.setWorkspaceCanvas(false)
					}
					if screen == "canvas-bottom" || screen == "canvas-right" {
						a.dockWorkspaceTool(a.workspaceTools[toolDiagnostics])
						a.dock = toolDiagnostics
						a.setToolsDockSide("bottom")
						if screen == "canvas-right" {
							a.setToolsDockSide("right")
						}
					}
					if screen == "canvas-small" {
						a.setWorkspaceCanvas(false)
						a.workspaceCanvas = true
						width, height = 560, 760
					}
					view = a.view
				case "workspace-four", "workspace-six":
					fixture, _ := terminalSplitFixture(t, 4)
					a.sessions = fixture.sessions
					a.ctx = context.Background()
					a.toolsHidden = true
					a.openWorkspaceTool(toolFiles)
					files := a.workspaceTools[toolFiles]
					a.openWorkspaceTool(toolDiagnostics)
					status := a.workspaceTools[toolDiagnostics]
					a.dropWorkspaceTab(a.workspaceTabPayload(workspaceTab{session: a.sessions[3]}), a.sessions[0].group, workspaceTab{}, "right")
					a.dropWorkspaceTab(a.workspaceTabPayload(workspaceTab{tool: files}), a.sessions[0].group, workspaceTab{}, "bottom")
					a.dropWorkspaceTab(a.workspaceTabPayload(workspaceTab{session: a.sessions[2]}), a.sessions[3].group, workspaceTab{}, "bottom")
					if screen == "workspace-six" {
						a.dropWorkspaceTab(a.workspaceTabPayload(workspaceTab{tool: status}), a.sessions[0].group, workspaceTab{}, "top")
						a.dropWorkspaceTab(a.workspaceTabPayload(workspaceTab{session: a.sessions[1]}), a.sessions[2].group, workspaceTab{}, "right")
					}
					width, height = 1440, 960
					view = a.view
				case "dock-left", "dock-top":
					a.setToolsDockSide(screen[5:])
				case "protected-paths", "protected-paths-narrow":
					a.newConnection()
					a.form.Name = "示例服务器"
					a.connection.section = connectionAccess
					view = a.view
					width, height = 960, 760
					if screen == "protected-paths-narrow" {
						width = 560
						a.updateProtectedPaths("/srv/private\nrelative/path")
					}
				case "files-tab":
					a.openFileTab()
				case "editor", "editor-loading":
					a.edit = &editor.State{Buffer: editor.NewBuffer("user nginx;\nworker_processes auto;\n\nevents {\n    worker_connections 1024;\n}\n")}
					a.editPath = "/etc/nginx/nginx.conf"
					a.editHost = "a"
					width, height = 900, 680
					view = a.editorWindowView
					if screen == "editor-loading" {
						a.editorRead = &fileReadState{endpoint: "a", name: a.editPath, loading: true, editor: a.edit, cancel: func() {}}
					}
				case "ports":
					p := portsApp()
					a.sessions[0].stats = p.sessions[0].stats
					a.dock = toolDiagnostics
					a.diagnosticTab = 5
					a.fileExpanded = true
				}
				tt := ui.NewTester(view, width, height)
				tt.SetLocale("zh-Hans")
				tt.SetDark(dark)
				tt.SetTitleBar(ui.TitleBar{Height: 48, Left: 70})
				tt.Frame()
				tt.Frame()
				f, err := os.OpenFile(filepath.Join(directory, name+".png"), os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0600)
				if err != nil {
					t.Fatal(err)
				}
				if err := png.Encode(f, tt.Image()); err != nil {
					f.Close()
					t.Fatal(err)
				}
				if err := f.Close(); err != nil {
					t.Fatal(err)
				}
				bounds := map[string]ui.Rect{}
				for _, label := range []string{"保护路径输入", "允许 AI 访问此服务器", "保护关键操作", "连接分类内容", "顶部工作区选项卡", "画布功能开关", "画布功能：文件", "设置", "工具栏标题", "工具选项卡", "目录树", "文件列表", "目录路径", "显示本机文件", "显示或隐藏目录树", "上一级", "刷新目录", "新建文件夹", "上传文件", "下载到本机…", "文件窗口菜单", "设置页面", "设置分类", "设置内容容器"} {
					if rect, ok := tt.Find(label); ok {
						bounds[label] = rect
					}
				}
				if _, ok := bounds["顶部工作区选项卡"]; ok {
					selected := a.selectedWorkspaceTab(a.activeTerminalGroup)
					label := "顶部标签：" + a.workspaceTabTitle(nil, selected)
					r, visible := tt.Find(label)
					if !visible || r.Y < 0 || r.Y+r.H > 48 {
						t.Fatal("canvas or split workspace lost its selected top tab", label, r)
					}
					bounds[label] = r
				}
				if rail, ok := bounds["画布功能开关"]; ok && rail.W != 32 {
					t.Fatal("canvas rail differs from the 32 DIP design specification", rail)
				}
				metadata, err := json.MarshalIndent(bounds, "", "  ")
				if err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(filepath.Join(directory, name+".json"), append(metadata, '\n'), 0600); err != nil {
					t.Fatal(err)
				}
			})
		}
	}
}
