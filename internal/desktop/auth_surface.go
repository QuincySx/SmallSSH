package desktop

import (
	"time"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"
	"sshclient/internal/systemauth"
)

// automaticAuthDelay hides one interrupted first evaluation. It is deliberately
// short and used once: Touch ID reports notReady while the window, the native
// view or the application activation is still settling, and retrying later than
// this would look like a hang.
const automaticAuthDelay = 500 * time.Millisecond

// authReadiness is everything an automatic OS authentication must observe before
// it may start. The landing page and the approval surfaces queue the same struct
// so a condition can no longer be added to only one entry point.
type authReadiness struct {
	windowReady, visible, focused, active, systemReady bool
}

// ready reports whether an automatic attempt may start now. Application
// activation belongs here: a key window in an inactive application is not enough
// for the native view, and evaluating then fails for a reason the user cannot see.
func (r authReadiness) ready(now time.Time, attempted bool, retryAt time.Time, presentation *systemauth.EmbeddedPresentation) bool {
	return r.windowReady && r.visible && r.focused && r.active && r.systemReady && !attempted &&
		!now.Before(retryAt) && (presentation == nil || presentation.Ready())
}

// retryAutomatic treats a system-readiness interruption as the only transient
// failure worth a second automatic attempt. A rejected fingerprint, a locked or
// unavailable device and an explicit cancellation never authenticate again.
func retryAutomatic(err error, automatic, retryUsed bool, now time.Time) (time.Time, bool) {
	if !automatic || retryUsed || !systemauth.IsAuthenticationNotReady(err) {
		return time.Time{}, false
	}
	return now.Add(automaticAuthDelay), true
}

// placeAuthenticationFrame points a native authentication view at the host window
// and the anchor of the previous committed frame, or detaches it when the host
// cannot display it. Without an anchor the view must not keep a stale handle.
func placeAuthenticationFrame(host *mygo.Window, p *systemauth.EmbeddedPresentation, bounds ui.Rect) {
	if p == nil {
		return
	}
	if host == nil || host.IsDestroyed() || !host.IsVisible() || bounds.W <= 0 || bounds.H <= 0 {
		p.SetFrame(0, systemauth.EmbeddedRect{})
		return
	}
	p.SetFrame(host.NativeHandle(), systemauth.EmbeddedRect{X: float64(bounds.X), Y: float64(bounds.Y), Width: float64(bounds.W), Height: float64(bounds.H)})
}

// anchorTracker remembers the frame a native view was last placed at.
type anchorTracker struct {
	last ui.Rect
	seen bool
}

// follow reports whether the surface needs one more frame: while no usable
// native view exists yet, or right after the anchor moved, because bounds belong
// to the previous committed frame. A stable anchor asks for nothing, so an
// authentication in progress no longer rebuilds its whole surface every 33 ms.
// A resize still re-renders and asks for the one frame that places the view.
func (t *anchorTracker) follow(bounds ui.Rect, usable, nativeView, systemReady bool) bool {
	if !usable || !systemReady {
		t.seen = false // nothing is placed: the next usable anchor is a first one
		return systemReady && !usable
	}
	// The frame that places an anchor needs no follow-up; only a later change
	// does, because the bounds it reads belong to the frame before it.
	moved := t.seen && t.last != bounds
	t.last, t.seen = bounds, true
	return nativeView && moved
}
