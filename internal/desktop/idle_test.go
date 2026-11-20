package desktop

import (
	"context"
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

func TestIdleWatchersBelongToEachAppAndStopWithTheirLifetime(t *testing.T) {
	type fixture struct {
		app     *app
		cancel  context.CancelFunc
		ticks   chan time.Time
		updates chan func()
		stopped chan struct{}
		starts  int
	}
	newFixture := func() *fixture {
		ctx, cancel := context.WithCancel(context.Background())
		f := &fixture{cancel: cancel, ticks: make(chan time.Time, 1), updates: make(chan func(), 4), stopped: make(chan struct{})}
		f.app = &app{ctx: ctx, dispatch: func(fn func()) { f.updates <- fn }}
		t.Cleanup(cancel)
		return f
	}
	first, second := newFixture(), newFixture()
	start := func(f *fixture) {
		f.app.watchIdleWithTicker(func() (<-chan time.Time, func()) {
			f.starts++
			return f.ticks, func() { close(f.stopped) }
		})
	}
	update := func(f *fixture) func() {
		t.Helper()
		f.ticks <- time.Now()
		select {
		case fn := <-f.updates:
			return fn
		case <-time.After(3 * time.Second):
			t.Fatal("app did not receive its idle tick")
			return nil
		}
	}
	start(first)
	start(first)
	start(second)
	if first.starts != 1 || second.starts != 1 {
		t.Fatal("watcher was shared between apps or started more than once", first.starts, second.starts)
	}
	queued := update(first)
	update(second)()
	first.cancel()
	select {
	case <-first.stopped:
	case <-time.After(3 * time.Second):
		t.Fatal("canceled app retained its clock")
	}
	first.app.idleUnlockedAt = time.Now()
	queued()
	if first.app.idleUnlockedAt.IsZero() {
		t.Fatal("queued callback changed app state after cancellation")
	}
	update(second)()
	second.cancel()
	select {
	case <-second.stopped:
	case <-time.After(3 * time.Second):
		t.Fatal("second app retained its clock")
	}
}

func TestIdleWatchDoesNotStartWithoutCancelableLifetime(t *testing.T) {
	for _, ctx := range []context.Context{nil, context.Background()} {
		a := &app{ctx: ctx}
		a.watchIdleWithTicker(func() (<-chan time.Time, func()) {
			t.Fatal("created an idle clock without an app cancellation path")
			return nil, func() {}
		})
	}
}
