//go:build preview_qa && terminal_integration

package desktop

import (
	"context"
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
		for _, screen := range []string{"workspace", "unlock", "unlock-system", "unlock-expanded", "vault-create", "connection", "connection-narrow", "connection-hardware", "connection-hardware-narrow", "settings", "files-tab", "workspace-four", "workspace-six", "dock-left", "dock-top", "protected-paths", "protected-paths-narrow", "editor", "editor-loading", "ports"} {
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
				term, err := terminal.New(terminal.Options{Conn: newTerminalRecorder(), Theme: lightColors, DarkTheme: darkColors, ClipboardRead: terminal.ClipboardDeny, NoClipboardWrite: true})
				if err != nil {
					t.Fatal(err)
				}
				defer term.Close()
				a.sessions[0].term = term
				term.Feed([]byte("demo@example:~$ uname -a\r\nLinux example 6.12.0 x86_64 GNU/Linux\r\ndemo@example:~$ "))
				width, height := 1280, 860
				view := a.view
				switch screen {
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
				case "settings":
					width, height = 1080, 760
					a.settingsOpen = true
					view = a.settingsWindowView
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
					a.editProtectedPaths()
					view = a.promptView
					width, height = 960, 760
					if screen == "protected-paths-narrow" {
						width = 560
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
			})
		}
	}
}
