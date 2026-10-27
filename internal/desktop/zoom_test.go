package desktop

import (
	"os"
	"path/filepath"
	"testing"
)

func TestZoomWalksTheLadderAndStopsAtBothEnds(t *testing.T) {
	a := &app{}
	if a.viewZoom() != 1 {
		t.Fatalf("unset zoom drew at %v, want 100%%", a.viewZoom())
	}
	for range 40 {
		a.zoomBy(1)
	}
	if got := a.viewZoom(); got != zoomSteps[len(zoomSteps)-1] {
		t.Fatalf("zooming in stopped at %v, want %v", got, zoomSteps[len(zoomSteps)-1])
	}
	for range 40 {
		a.zoomBy(-1)
	}
	if got := a.viewZoom(); got != zoomSteps[0] {
		t.Fatalf("zooming out stopped at %v, want %v", got, zoomSteps[0])
	}
	// Up one and down one lands on the size passed through, not near it.
	a.zoomActual()
	a.zoomBy(1)
	enlarged := a.viewZoom()
	a.zoomBy(-1)
	if got := a.viewZoom(); got != 1 {
		t.Fatalf("back from %v landed at %v, want 100%%", enlarged, got)
	}
}

func TestZoomClampsStoredValuesAndPersists(t *testing.T) {
	for _, stored := range []float32{0, -3, 0.1, 9, 99} {
		if got := clampZoom(stored); got != 1 {
			t.Fatalf("clampZoom(%v) = %v, want 100%%", stored, got)
		}
	}
	if got := clampZoom(1.25); got != 1.25 {
		t.Fatalf("clampZoom(1.25) = %v", got)
	}

	// A layout written before zoom existed still loads, and the zoom round-trips.
	dir := t.TempDir()
	if e := os.Chmod(dir, 0o700); e != nil { // layout.json only lives in a private directory
		t.Fatal(e)
	}
	a := &app{dir: dir}
	a.setZoom(1.25)
	saved := &app{dir: dir}
	saved.loadLayout()
	if got := saved.viewZoom(); got != 1.25 {
		t.Fatalf("reloaded zoom = %v, want 1.25", got)
	}
	older := `{"version":2,"right":false,"hidden":true,"tab":0,"sidebarHidden":true}`
	if e := os.WriteFile(filepath.Join(dir, "layout.json"), []byte(older), 0o600); e != nil {
		t.Fatal(e)
	}
	legacy := &app{dir: dir}
	legacy.loadLayout()
	if !legacy.toolsHidden || !legacy.sidebarHidden || legacy.viewZoom() != 1 {
		t.Fatal("a version 2 layout lost its fields or invented a zoom")
	}
}
