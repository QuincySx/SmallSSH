//go:build preview_qa

package desktop

import (
	"context"
	"image/png"
	"os"
	"path/filepath"
	"testing"

	"github.com/egoist/mygo/ui"
	"sshclient/internal/desktop/components"
	"sshclient/internal/systemauth"
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
						ctx: context.Background(), online: true, page: 4, settingsSection: section,
						sidebarHidden: sidebar.hidden,
						authStatus:    systemauth.Status{Available: true, Enabled: true, Name: "Touch ID"},
					}
					tt := ui.NewTester(a.view, size.width, size.height)
					region, width := "设置分类", float32(152)
					if sidebar.hidden {
						region, width = "设置分类窄栏", components.RailWidth
					}
					navigation, ok := tt.Find(region)
					if !ok || navigation.W != width {
						t.Fatal("settings snapshot lost its category navigation", navigation)
					}
					for _, category := range settingsCategories {
						item, ok := tt.Find("设置分类：" + category.title)
						icon, iconOK := tt.Find(category.title + "图标")
						if !ok || !iconOK || item.H != components.NavigationItemHeight || item.X < navigation.X || item.X+item.W > navigation.X+navigation.W || icon.W != 28 || icon.H != 28 || icon.X != item.X+4 || icon.Y != item.Y+(components.NavigationItemHeight-28)/2 {
							t.Fatal("settings snapshot changed the shared target or icon geometry", category.title, item, icon)
						}
						if sidebar.hidden && item.W != components.NavigationIconSlotSize {
							t.Fatal("collapsed settings snapshot lost its compact target", item)
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
