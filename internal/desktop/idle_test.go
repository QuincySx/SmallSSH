package desktop

import (
	"testing"
	"time"
)

func TestIdleLockNeedsBothClocksPastTheLimit(t *testing.T) {
	long := idleLockAfter + time.Minute
	short := idleLockAfter - time.Minute
	for _, tc := range []struct {
		name              string
		online            bool
		idle, unlockedFor time.Duration
		want              bool
	}{
		{"away and long unlocked", true, long, long, true},
		{"online but in use", true, short, long, false},
		{"just unlocked after a break", true, long, time.Second, false},
		{"locked", false, long, long, false},
		{"no idle information", true, 0, long, false},
		{"exactly at the limit", true, idleLockAfter, idleLockAfter, true},
	} {
		if got := lockWhenIdle(tc.online, tc.idle, tc.unlockedFor); got != tc.want {
			t.Fatalf("%s: lockWhenIdle(%v, %v, %v) = %v, want %v", tc.name, tc.online, tc.idle, tc.unlockedFor, got, tc.want)
		}
	}
}

// A window unlocked while the machine was already idle must not lock on the very
// next tick, which is what a single clock would do.
func TestUnlockedAfterIdleDoesNotLockImmediately(t *testing.T) {
	previous := idleTime
	defer func() { idleTime = previous }()
	idleTime = func() time.Duration { return 6 * time.Hour }
	a := &app{online: true}
	unlocked := time.Now()
	if a.idleExpired(unlocked.Add(idleTick), unlocked) {
		t.Fatal("locked a vault that had just been unlocked after a long break")
	}
	if !a.idleExpired(unlocked.Add(idleLockAfter), unlocked) {
		t.Fatal("did not lock after the window was left alone for the whole limit")
	}
}
