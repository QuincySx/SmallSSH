//go:build preview_qa

package desktop

import (
	"context"
	"image/png"
	"os"
	"path/filepath"
	"testing"

	"github.com/egoist/mygo/ui"
	"sshclient/docs/design"
	"sshclient/internal/broker"
	"sshclient/internal/desktop/components"
	"sshclient/internal/systemauth"
	"sshclient/internal/vault"
)

// Offline settings snapshots use synthetic state and never start the native
// app, system authentication, SSH, a production vault or an external window.
func TestSettingsSnapshotsWithoutWindows(t *testing.T) {
	if os.Getenv("SSH_CLIENT_PREVIEW_QA") != "1" {
		t.Skip("opt-in offline settings snapshots")
	}
	dir := os.Getenv("SSH_CLIENT_UI_SNAPSHOT_DIR")
	if !filepath.IsAbs(dir) {
		t.Fatal("absolute snapshot directory required")
	}
	if err := os.MkdirAll(dir, 0700); err != nil {
		t.Fatal(err)
	}
	private, err := os.MkdirTemp("", "pin-preview-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(private) })
	owner, err := vault.New([]byte("synthetic offline public broker pin preview"))
	if err != nil {
		t.Fatal(err)
	}
	server, err := broker.OpenSession(private, owner)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(server.Close)
	for _, section := range []string{settingsWorkspace, settingsSecurity} {
		for _, sidebar := range []struct {
			name   string
			hidden bool
		}{{"", false}, {"-collapsed", true}} {
			for _, size := range []struct {
				name   string
				width  int
				height int
			}{{"wide", 1280, 800}, {"narrow", 560, 760}} {
				t.Run(section+"-"+size.name+sidebar.name, func(t *testing.T) {
					a := &app{
						ctx: context.Background(), online: true, page: 4, settingsSection: section, settingsOpen: true,
						server:        testDesktopSession(server),
						sidebarHidden: sidebar.hidden,
						authStatus:    systemauth.Status{Available: true, Enabled: true, Name: "Touch ID"},
					}
					tt := ui.NewTester(a.settingsWindowView, size.width, size.height)
					tt.SetTitleBar(ui.TitleBar{Height: 48, Left: 70})
					navigation, ok := tt.Find("设置分类")
					if !ok || navigation.X != 20 || navigation.W != design.Length("size-settings-nav")-24 {
						t.Fatal("settings snapshot lost its category navigation", navigation)
					}
					for _, category := range settingsCategories {
						item, ok := tt.Find("设置分类：" + category.title)
						if !ok || item.H != components.ControlHeight || item.X != navigation.X || item.W != navigation.W {
							t.Fatal("settings navigation depends on the workbench sidebar or differs from its fixed text layout", category.title, item)
						}
					}
					path := filepath.Join(dir, "settings-"+section+"-"+size.name+sidebar.name+".png")
					f, err := os.OpenFile(path, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0600)
					if err != nil {
						t.Fatal(err)
					}
					err = png.Encode(f, tt.Image())
					closeErr := f.Close()
					if err != nil || closeErr != nil {
						t.Fatal(err, closeErr)
					}
					t.Log(path)
				})
			}
		}
	}
}
