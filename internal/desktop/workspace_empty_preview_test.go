//go:build preview_qa

package desktop

import (
	"context"
	"image/png"
	"os"
	"path/filepath"
	"testing"

	"github.com/egoist/mygo/ui"
	"sshclient/internal/sshconn"
	"sshclient/internal/vault"
)

func TestWorkspaceEmptySnapshotsWithoutWindows(t *testing.T) {
	if os.Getenv("SSH_CLIENT_PREVIEW_QA") != "1" {
		t.Skip("opt-in offline snapshots")
	}
	dir := os.Getenv("SSH_CLIENT_UI_SNAPSHOT_DIR")
	if !filepath.IsAbs(dir) {
		t.Fatal("absolute snapshot directory required")
	}
	if err := os.MkdirAll(dir, 0700); err != nil {
		t.Fatal(err)
	}
	for _, variant := range []struct {
		name, locale, selected           string
		width                            int
		dark, compact, hasServer, canvas bool
	}{
		{"empty-wide", "zh-Hans", "", 1100, false, false, false, false},
		{"choose-narrow", "zh-Hans", "", 560, false, false, true, false},
		{"selected-wide", "zh-Hans", "example", 1100, false, false, true, false},
		{"choose-english", "en", "", 560, false, false, true, false},
		{"selected-dark", "zh-Hans", "example", 1100, true, false, true, false},
		{"choose-compact", "zh-Hans", "", 1100, false, true, true, false},
		{"choose-canvas-light", "zh-Hans", "", 1280, false, false, true, true},
		{"choose-canvas-dark", "zh-Hans", "", 1280, true, false, true, true},
	} {
		t.Run(variant.name, func(t *testing.T) {
			a := &app{ctx: context.Background(), online: true, page: 1, workspaceCanvas: variant.canvas, toolsHidden: variant.canvas, selected: variant.selected, sidebarWidth: 190, sidebarHidden: variant.compact}
			fixtureDir := filepath.Join(string(filepath.Separator), "SmallSSH-QA")
			a.localFilePane = newFilePane(localEndpoint, false)
			a.primaryFilePane = newFilePane(localEndpoint, true)
			for _, pane := range []*filePane{a.localFilePane, a.primaryFilePane} {
				browser := &directoryBrowser{dir: fixtureDir, address: fixtureDir, cache: map[string][]sshconn.File{fixtureDir: nil}, open: map[string]*bool{}, selected: -1, anchor: -1}
				browser.reveal(true, fixtureDir)
				pane.states[localEndpoint] = browser
			}
			if variant.hasServer {
				a.connections = []vault.Summary{{ID: "example", Name: "Example server", Host: "192.0.2.10", User: "demo"}}
			}
			tt := ui.NewTester(a.view, variant.width, 760)
			tt.SetTitleBar(ui.TitleBar{Height: 32, Left: 78})
			tt.SetLocale(variant.locale)
			tt.SetDark(variant.dark)
			f, err := os.OpenFile(filepath.Join(dir, "workspace-empty-"+variant.name+".png"), os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0600)
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
