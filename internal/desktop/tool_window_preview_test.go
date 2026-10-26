//go:build preview_qa && terminal_integration

package desktop

import (
	"image/png"
	"os"
	"path/filepath"
	"testing"

	"github.com/egoist/mygo/plugins/terminal"
	"github.com/egoist/mygo/ui"
)

// Both window contents are rendered offscreen through the real views. The
// native window factory is replaced; this never shows or activates a window.
func TestDetachedToolsSnapshotsWithoutWindows(t *testing.T) {
	if os.Getenv("SSH_CLIENT_PREVIEW_QA") != "1" {
		t.Skip("opt-in offline preview")
	}
	dir := os.Getenv("SSH_CLIENT_UI_SNAPSHOT_DIR")
	if !filepath.IsAbs(dir) {
		t.Fatal("absolute output path required")
	}
	if err := os.MkdirAll(dir, 0700); err != nil {
		t.Fatal(err)
	}
	for _, variant := range []string{"status", "status-dark", "files", "docker", "dock-menu"} {
		t.Run(variant, func(t *testing.T) {
			a := detailedStatusDemo()
			a.fileExpanded = false
			if variant == "files" {
				a = filePresentationDemo()
				a.toolsDetached = false
			}
			if variant == "docker" {
				a.diagnosticTab = 4
			}
			term, err := terminal.New(terminal.Options{Conn: newTerminalRecorder(), Theme: terminal.DarkTheme(), ClipboardRead: terminal.ClipboardDeny, NoClipboardWrite: true})
			if err != nil {
				t.Fatal(err)
			}
			defer term.Close()
			a.sessions[0].term = term
			term.Feed([]byte("root@demo:~# uptime\r\n 10:40:00 up 7 days, 1 user, load average: 0.10, 0.20, 0.30\r\nroot@demo:~# "))
			fakeToolsFactory(a)
			a.detachTools()
			main := ui.NewTester(a.view, 1280, 900)
			tools := ui.NewTester(a.detachedToolsView, 960, 720)
			main.SetDark(variant == "status-dark")
			tools.SetDark(variant == "status-dark")
			var previousRight float32
			for _, label := range []string{"文件", "命令", "服务器状态"} {
				r, ok := tools.Find(label)
				if !ok || r.W < 20 || r.Y < 0 || r.Y+r.H > 37 || r.X < previousRight {
					t.Fatal("tool tab not visible in independent window", variant, label, r)
				}
				previousRight = r.X + r.W
			}
			if variant == "dock-menu" {
				if err := tools.Click("工具面板布局"); err != nil {
					t.Fatal(err)
				}
			}
			for _, target := range []struct {
				name string
				ui   *ui.Tester
			}{{"main", main}, {"tools", tools}} {
				f, err := os.Create(filepath.Join(dir, "detached-"+variant+"-"+target.name+".png"))
				if err != nil {
					t.Fatal(err)
				}
				err = png.Encode(f, target.ui.Image())
				closeErr := f.Close()
				if err != nil || closeErr != nil {
					t.Fatal(err, closeErr)
				}
			}
		})
	}
}
