//go:build preview_qa && terminal_integration

package desktop

import (
	"image/png"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/egoist/mygo/plugins/terminal"
	"github.com/egoist/mygo/ui"
)

func TestComposeProjectSnapshotsWithoutWindows(t *testing.T) {
	if os.Getenv("SSH_CLIENT_PREVIEW_QA") != "1" {
		t.Skip("offline preview opt in")
	}
	dir := os.Getenv("SSH_CLIENT_UI_SNAPSHOT_DIR")
	if !filepath.IsAbs(dir) {
		t.Fatal("absolute output required")
	}
	if err := os.MkdirAll(dir, 0700); err != nil {
		t.Fatal(err)
	}
	for _, variant := range []string{"projects", "projects-dark", "projects-narrow", "projects-right", "details", "details-narrow", "source-conflict", "confirm-start", "confirm-stop", "confirm-restart", "confirm-restart-narrow"} {
		t.Run(variant, func(t *testing.T) {
			a := composeProjectsDemo()
			a.terminalShare = 35
			term, err := terminal.New(terminal.Options{Conn: newTerminalRecorder(), Theme: terminal.DarkTheme(), ClipboardRead: terminal.ClipboardDeny, NoClipboardWrite: true})
			if err != nil {
				t.Fatal(err)
			}
			defer term.Close()
			a.sessions[0].term = term
			term.Feed([]byte("root@demo-node:~# uptime\r\n 12:30:00 up 7 days, load average: 0.10, 0.08, 0.03\r\nroot@demo-node:~# "))
			width, height := 1280, 1000
			if strings.HasSuffix(variant, "narrow") {
				width = 560
			}
			if variant == "projects-right" {
				a.toolsRight, a.terminalWidthShare = true, 62
			}
			if strings.HasPrefix(variant, "details") || variant == "source-conflict" {
				d := a.sessions[0].stats.Inventory.Docker
				s := a.managementState()
				s.composeProject, s.composeEngine, s.composeSocket = "blog", d.EngineID, d.Socket
				if variant == "source-conflict" {
					d.Containers[1].ComposeDir = "/different/source"
				}
			}
			tt := ui.NewTester(a.view, width, height)
			tt.SetDark(variant == "projects-dark")
			if strings.HasPrefix(variant, "confirm-") {
				project, action := "blog", "重启项目…"
				if variant == "confirm-start" {
					project, action = "docs", "启动项目…"
				} else if variant == "confirm-stop" {
					action = "停止项目…"
				}
				if err := tt.Click("Compose 项目 " + project); err != nil {
					t.Fatal(err)
				}
				if err := chooseComposeProjectMenu(tt, project, action); err != nil {
					t.Fatal(err)
				}
			}
			f, err := os.Create(filepath.Join(dir, "compose-"+variant+".png"))
			if err != nil {
				t.Fatal(err)
			}
			err, closeErr := png.Encode(f, tt.Image()), f.Close()
			if err != nil || closeErr != nil {
				t.Fatal(err, closeErr)
			}
		})
	}
}
