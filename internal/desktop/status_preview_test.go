//go:build preview_qa && terminal_integration

package desktop

import (
	"image/png"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/egoist/mygo/plugins/terminal"
	"github.com/egoist/mygo/ui"
)

func TestServerResourceSnapshotsWithoutWindows(t *testing.T) {
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
	for _, variant := range []string{"overview", "expanded", "cpu", "memory", "network", "disk", "system", "processes", "services", "dark", "small", "right", "disconnected", "hover", "cpu-docked", "cpu-small", "cpu-right", "cpu-scrolled", "compact", "compact-dark", "compact-small", "compact-right", "shallow", "shallow-dark", "shallow-minimum", "width-560", "width-700", "width-850", "width-1100"} {
		t.Run(variant, func(t *testing.T) {
			a := statusDemo()
			if variant == "compact" || variant == "compact-dark" || variant == "compact-small" || variant == "compact-right" {
				a = compactStatusDemo()
				a.fileExpanded = true
			}
			term, err := terminal.New(terminal.Options{Conn: newTerminalRecorder(), Theme: terminal.DarkTheme(), DarkTheme: terminal.DarkTheme(), ClipboardRead: terminal.ClipboardDeny, NoClipboardWrite: true})
			if err != nil {
				t.Fatal(err)
			}
			defer term.Close()
			a.sessions[0].term = term
			width, height := 1280, 900
			if size, ok := strings.CutPrefix(variant, "width-"); ok {
				width, err = strconv.Atoi(size)
				if err != nil {
					t.Fatal(err)
				}
				height, a.terminalShare = 760, 40
			}
			switch variant {
			case "shallow", "shallow-dark", "shallow-minimum":
				width, height = 1000, 760
				a.terminalShare = 72
				if variant == "shallow-minimum" {
					a.terminalShare = 90
				}
			case "compact-small":
				width, height = 1000, 760
				a.fileExpanded, a.terminalShare = false, 40
			case "compact-right":
				a.fileExpanded, a.toolsRight, a.terminalWidthShare = false, true, 62
			case "cpu-small":
				width, height = 1000, 760
			case "cpu-right":
				a.toolsRight, a.terminalWidthShare = true, 62
			case "expanded":
				a.fileExpanded = true
			case "cpu":
				a.statusMetric = "CPU"
				a.fileExpanded = true
			case "hover":
				a.statusMetric = "CPU"
				a.fileExpanded = true
			case "memory":
				a.statusMetric = "内存"
				a.fileExpanded = true
			case "network":
				a.statusMetric = "网络"
				a.fileExpanded = true
			case "disk":
				a.statusMetric = "磁盘"
				a.fileExpanded = true
			case "system":
				a.diagnosticTab = 3
				a.fileExpanded = true
			case "processes":
				a.diagnosticTab = 1
				a.fileExpanded = true
			case "services":
				a.diagnosticTab = 2
				a.fileExpanded = true
			case "small":
				width, height = 1000, 760
				a.terminalShare = 65
			case "right":
				a.toolsRight = true
				a.terminalWidthShare = 62
			case "disconnected":
				a.sessions[0].closed = true
			}
			tt := ui.NewTester(a.view, width, height)
			tt.SetDark(variant == "dark" || variant == "compact-dark" || variant == "shallow-dark")
			term.Feed([]byte("\x1b[32mroot@demo-node\x1b[0m:~# uptime\r\n 14:32:18 up 2 days, 18:11, 1 user, load average: 0.12, 0.08, 0.05\r\n\x1b[32mroot@demo-node\x1b[0m:~# "))
			tt.Frame()
			tt.Frame()
			if variant == "cpu-docked" || variant == "cpu-small" || variant == "cpu-right" || variant == "cpu-scrolled" {
				if err := tt.Click("CPU详情"); err != nil {
					t.Fatal(err)
				}
				if variant == "cpu-scrolled" {
					viewport, _ := tt.Find("服务器资源内容")
					tt.Scroll(viewport.X+viewport.W/2, viewport.Y+viewport.H/2, 0, 10000)
				}
			}
			if variant == "hover" {
				bounds, _ := tt.Find("CPU 使用率趋势")
				tt.Move(bounds.X+bounds.W*.65, bounds.Y+bounds.H*.4)
				tt.Frame()
			}
			f, err := os.Create(filepath.Join(dir, "server-"+variant+".png"))
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
