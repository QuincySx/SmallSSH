//go:build preview_qa && terminal_integration

package desktop

import (
	"fmt"
	"image/png"
	"os"
	"path/filepath"
	"testing"

	"github.com/egoist/mygo/ui"
)

func TestAssessedStatusSnapshotsWithoutWindows(t *testing.T) {
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
	for _, section := range []string{"system", "network", "blocks", "container-info", "logs", "images", "processes", "services"} {
		for _, width := range []int{560, 1600} {
			name := fmt.Sprintf("%s-%d", section, width)
			t.Run(name, func(t *testing.T) {
				a := assessedStatusDemo()
				a.fileExpanded = true
				showAssessedSection(a, section)
				tt := ui.NewTester(a.view, width, 1000)
				f, err := os.Create(filepath.Join(dir, "assessed-"+name+".png"))
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
}
