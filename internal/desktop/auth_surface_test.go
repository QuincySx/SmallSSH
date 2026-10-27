package desktop

import (
	"testing"

	"github.com/egoist/mygo/ui"
)

// A stable anchor must not keep asking for frames: that rebuilt the whole
// surface 30 times a second for the length of an authentication.
func TestNativeViewFollowsOnlyAMovingAnchor(t *testing.T) {
	var tracker anchorTracker
	anchor := ui.Rect{X: 8, Y: 12, W: 36, H: 34}

	// Before a usable native view exists the surface keeps asking, so the first
	// committed anchor can arrive.
	if !tracker.follow(ui.Rect{}, false, false, true) {
		t.Fatal("first frame without an anchor did not ask for another")
	}
	if tracker.follow(anchor, true, true, true) {
		t.Fatal("the frame that first placed the anchor asked for another")
	}
	// The same anchor on the next frames is stable.
	for range 3 {
		if tracker.follow(anchor, true, true, true) {
			t.Fatal("a stable anchor kept asking for frames")
		}
	}
	// A layout change asks for exactly one follow-up frame.
	moved := ui.Rect{X: 8, Y: 70, W: 36, H: 34}
	if !tracker.follow(moved, true, true, true) {
		t.Fatal("a moved anchor did not ask for the follow-up frame")
	}
	if tracker.follow(moved, true, true, true) {
		t.Fatal("the follow-up frame asked for yet another")
	}
	// Without a live native view and with system verification off, nothing asks.
	if tracker.follow(ui.Rect{}, true, false, false) {
		t.Fatal("asked for frames with no native view and no system verification")
	}
}
