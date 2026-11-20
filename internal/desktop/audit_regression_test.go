package desktop

import (
	"context"
	"testing"
	"time"

	"github.com/egoist/mygo/ui/editor"
)

func TestLockAndShutdownReleaseTunnelState(t *testing.T) {
	for _, action := range []string{"lock", "shutdown"} {
		t.Run(action, func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			a := &app{ctx: ctx, cancel: cancel, dir: t.TempDir(), online: true}
			a.startUnlockedSession()
			a.tunnels = tunnelManager{open: true, host: "previous-host", kind: "remote", listen: "127.0.0.1:0", target: "previous-target:80", message: "previous error"}
			var runs []context.Context
			for range 32 {
				runCtx, stop := context.WithCancel(a.operationContext())
				runs = append(runs, runCtx)
				a.tunnels.active = append(a.tunnels.active, &tunnelRun{running: true, cancel: stop})
			}
			if action == "lock" {
				a.lock()
			} else {
				a.shutdown()
			}
			for _, run := range runs {
				if run.Err() == nil {
					t.Fatal("closing the owner session kept a tunnel alive")
				}
			}
			if a.tunnels.open || a.tunnels.host != "" || a.tunnels.kind != "" || a.tunnels.listen != "" || a.tunnels.target != "" || a.tunnels.message != "" || len(a.tunnels.active) != 0 {
				t.Fatal("closing the owner session retained tunnel records or editor values")
			}
			if action == "lock" {
				a.online = true
				a.startUnlockedSession()
				defer a.sessionCancel()
				a.openTunnels("new-host")
				if !a.tunnels.open || a.tunnels.host != "new-host" || a.tunnels.kind != "local" || a.tunnels.target != "127.0.0.1:80" || len(a.tunnels.active) != 0 {
					t.Fatal("fresh unlocked session reused the previous tunnel manager")
				}
			}
		})
	}
}

func TestReunlockBetweenIdleTicksStartsAFreshIdleClock(t *testing.T) {
	previous := idleTime
	t.Cleanup(func() { idleTime = previous })
	idleTime = func() time.Duration { return 6 * time.Hour }
	a := &app{ctx: context.Background(), online: true, idleUnlockedAt: time.Now().Add(-time.Hour)}
	a.lock()
	// No watcher tick observes the locked state before this unlock.
	a.online = true
	before := time.Now()
	a.startUnlockedSession()
	defer a.sessionCancel()
	if a.idleUnlockedAt.Before(before) {
		t.Fatal("a new owner session retained the previous idle-lock timestamp")
	}
	a.checkIdle()
	if !a.online {
		t.Fatal("the first timer tick immediately locked a fresh owner session")
	}
	a.idleUnlockedAt = time.Now().Add(-idleLockAfter)
	a.checkIdle()
	if a.online {
		t.Fatal("an idle session stayed unlocked after its full delay")
	}
}

func TestEditorByteCountMatchesSavedLineEndings(t *testing.T) {
	for _, tc := range []struct {
		name, text string
		want       int
	}{
		{"empty", "", 0},
		{"single unicode line", "中😀", 7},
		{"LF", "one\ntwo\n", 8},
		{"CRLF", "one\r\ntwo\r\n", 10},
		{"CRLF without trailing newline", "α\r\n😀", 8},
		{"mixed endings preserved on save", "one\r\ntwo\n", 9},
	} {
		t.Run(tc.name, func(t *testing.T) {
			buffer := editor.NewBuffer(tc.text)
			if got := editorBufferSize(buffer); got != tc.want || got != len(editorBufferText(buffer)) {
				t.Fatalf("byte count = %d, want %d saved bytes", got, tc.want)
			}
		})
	}
	buffer := editor.NewBuffer("original\r\n")
	buffer.Set("中\nfour\n")
	if got := editorBufferSize(buffer); got != 11 || got != len(editorBufferText(buffer)) {
		t.Fatalf("edited CRLF byte count = %d, want 11", got)
	}
	buffer.SetLineEnding("\n")
	if got := editorBufferSize(buffer); got != 9 || got != len(editorBufferText(buffer)) {
		t.Fatalf("edited LF byte count = %d, want 9", got)
	}
}
