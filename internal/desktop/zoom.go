package desktop

import "runtime"

// zoomKeys are the menu accelerators for the three zoom commands. macOS uses the
// browser shortcuts; on Windows and Linux every bare Ctrl chord belongs to the
// terminal, so they take Shift with it, as the app's copy and paste do.
func zoomKeys() (in, out, actual string) {
	if runtime.GOOS == "darwin" {
		return "Cmd++", "Cmd+-", "Cmd+0"
	}
	return "Ctrl+Shift++", "Ctrl+Shift+-", "Ctrl+Shift+0"
}

// zoomSteps is the ladder the zoom commands walk, so the same number of presses
// always lands on a size a human picked instead of an arbitrary factor of 1.1.
var zoomSteps = []float32{0.5, 0.67, 0.8, 0.9, 1, 1.1, 1.25, 1.5, 1.75, 2, 2.5, 3}

// clampZoom keeps a stored zoom inside the range MyGo lays out, and treats a
// missing or nonsensical value as 100%.
func clampZoom(z float32) float32 {
	if z < zoomSteps[0] || z > zoomSteps[len(zoomSteps)-1] {
		return 1
	}
	return z
}

// viewZoom is the zoom the main window is drawn at.
func (a *app) viewZoom() float32 {
	if a.zoom == 0 {
		return 1
	}
	return a.zoom
}

// zoomBy moves one step along the ladder, dir > 0 larger and dir < 0 smaller,
// and stops at either end. A press never leaves the ladder, so enlarging and
// shrinking come back to exactly the sizes that were passed through.
func (a *app) zoomBy(dir int) {
	at := a.indexZoom(a.viewZoom())
	if dir > 0 && at < len(zoomSteps)-1 {
		at++
	} else if dir < 0 && at > 0 {
		at--
	}
	a.setZoom(zoomSteps[at])
}

// zoomActual returns the main window to 100%.
func (a *app) zoomActual() { a.setZoom(1) }

// indexZoom is where z sits on the ladder, or the nearest step below it for a
// value stored by another version.
func (a *app) indexZoom(z float32) int {
	best := 0
	for i, step := range zoomSteps {
		if step <= z+0.001 {
			best = i
		}
	}
	return best
}

func (a *app) setZoom(z float32) {
	z = clampZoom(z)
	if z == a.viewZoom() && a.zoom != 0 {
		return
	}
	a.zoom = z
	if e := a.saveLayout(); e != nil {
		a.status = e.Error()
	}
	a.invalidateWindows()
}
