//go:build preview_qa

package desktop

import (
	"github.com/egoist/mygo/ui"
	"image/png"
	"os"
	"path/filepath"
	"sshclient/internal/connectcfg"
	"sshclient/internal/vault"
	"testing"
)

func TestSessionRouteSnapshotsWithoutWindows(t *testing.T) {
	if os.Getenv("SSH_CLIENT_PREVIEW_QA") != "1" {
		t.Skip("opt-in offline snapshots")
	}
	dir := os.Getenv("SSH_CLIENT_UI_SNAPSHOT_DIR")
	if !filepath.IsAbs(dir) {
		t.Fatal("absolute snapshot path required")
	}
	if err := os.MkdirAll(dir, 0700); err != nil {
		t.Fatal(err)
	}
	for _, style := range []struct {
		name, locale string
		width        int
		dark         bool
	}{
		{"light", "zh-Hans", 1280, false}, {"dark", "zh-Hans", 900, true}, {"english", "en", 720, false},
	} {
		t.Run(style.name, func(t *testing.T) {
			a := fileFollowDemo()
			a.routes = []vault.RouteSummary{{ID: "once", Name: "Office proxy with a deliberately long route name"}}
			source := a.sessions[0]
			source.target.Route = connectcfg.RouteSelection{Mode: "saved", ID: "once"}
			source.closed, source.connectionState, source.connectionError = true, "error", "Synthetic route is unavailable"
			a.syncFollowingFilePanes()
			a.setWorkspaceCanvas(true)
			tt := ui.NewTester(a.view, style.width, 820)
			tt.SetLocale(style.locale)
			tt.SetDark(style.dark)
			file, err := os.Create(filepath.Join(dir, "session-route-"+style.name+".png"))
			if err != nil {
				t.Fatal(err)
			}
			err = png.Encode(file, tt.Image())
			closeErr := file.Close()
			if err != nil || closeErr != nil {
				t.Fatal(err, closeErr)
			}
		})
	}
}
