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

func TestResponsiveWorkspaceSnapshotsWithoutWindows(t *testing.T) {
	if os.Getenv("SSH_CLIENT_PREVIEW_QA") != "1" {
		t.Skip("offline preview opt in")
	}
	dir := os.Getenv("SSH_CLIENT_UI_SNAPSHOT_DIR")
	if !filepath.IsAbs(dir) {
		t.Fatal("absolute output path required")
	}
	if err := os.MkdirAll(dir, 0700); err != nil {
		t.Fatal(err)
	}
	for _, variant := range []string{"narrow", "medium", "wide", "narrow-right", "narrow-docker", "wide-docker", "narrow-compose", "wide-compose", "wide-single", "wide-single-docker", "wide-single-compose", "narrow-connection", "minimum"} {
		t.Run(variant, func(t *testing.T) {
			a := responsiveWorkspaceDemo()
			term, err := terminal.New(terminal.Options{Conn: newTerminalRecorder(), Theme: terminal.DarkTheme(), ClipboardRead: terminal.ClipboardDeny, NoClipboardWrite: true})
			if err != nil {
				t.Fatal(err)
			}
			defer term.Close()
			a.sessions[0].term = term
			term.Feed([]byte("root@demo:~# uptime\r\n 15:00:00 up 7 days, load average: 0.10, 0.08, 0.05\r\nroot@demo:~# "))
			width, height := 560, 760
			if variant == "medium" {
				width = 1000
			}
			if strings.HasPrefix(variant, "wide") {
				width, height = 1600, 1000
			}
			if strings.Contains(variant, "single") {
				a.sessions[0].stats.Disks = a.sessions[0].stats.Disks[:1]
				d := a.sessions[0].stats.Inventory.Docker
				d.Containers = d.Containers[:1]
				a.fileExpanded = variant == "wide-single"
			}
			if strings.HasSuffix(variant, "docker") {
				a.diagnosticTab = 4
			}
			if strings.HasSuffix(variant, "compose") {
				a.diagnosticTab = 4
				a.managementState().dockerTab = 2
			}
			if variant == "narrow-right" {
				a.toolsRight = true
			}
			if variant == "narrow-connection" {
				a.newConnection()
				a.form.Name, a.form.Host, a.form.User = "个人服务器", "192.0.2.10", "root"
			}
			if variant == "minimum" || variant == "narrow-connection" {
				height = 560
			}
			tt := ui.NewTester(a.view, width, height)
			f, err := os.Create(filepath.Join(dir, "responsive-"+variant+".png"))
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
