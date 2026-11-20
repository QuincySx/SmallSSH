package desktop

import (
	"time"

	"github.com/egoist/mygo"
)

// idleLockAfter is how long the keyboard and mouse may sit untouched before the
// vault locks itself. An unlocked window holds full owner authority: any process
// running as this user can ask it to do owner work, and every approval the owner
// already granted stays valid. Leaving that on a desk is the largest remaining
// risk once the rest of the trust model is in place.
const idleLockAfter = 15 * time.Minute

// idleTick is how often the idle time is read. It runs on its own clock rather
// than on frames, so a window nobody touches still reaches the deadline.
const idleTick = 10 * time.Second

// idleTime reads how long this machine has been idle. It is a seam for tests.
var idleTime = func() time.Duration {
	if !mygo.App.IsReady() {
		return 0
	}
	return mygo.Power.IdleTime()
}

// lockWhenIdle reports whether an online app has been left alone long enough.
// Both clocks must agree, because the operating system's idle time can already
// exceed the limit when the vault is unlocked after a break: a window that was
// just unlocked must not lock itself again on the next tick.
func lockWhenIdle(online bool, idle, unlockedFor time.Duration) bool {
	return lockWhenIdleAfter(online, idle, unlockedFor, idleLockAfter)
}

func lockWhenIdleAfter(online bool, idle, unlockedFor, delay time.Duration) bool {
	if !online || delay <= 0 || idle <= 0 || unlockedFor <= 0 {
		return false
	}
	return idle >= delay && unlockedFor >= delay
}

// watchIdle starts one watcher for this app, from its first rendered frame.
// State reads and locking stay in UI callbacks; canceling the app stops its clock.
func (a *app) watchIdle() {
	a.watchIdleWithTicker(func() (<-chan time.Time, func()) {
		ticker := time.NewTicker(idleTick)
		return ticker.C, ticker.Stop
	})
}

func (a *app) watchIdleWithTicker(newTicker func() (<-chan time.Time, func())) {
	if a.ctx == nil || a.ctx.Done() == nil || a.ctx.Err() != nil {
		return
	}
	ctx := a.ctx
	a.idleWatch.Do(func() {
		ticks, stop := newTicker()
		go func() {
			defer stop()
			for {
				select {
				case <-ctx.Done():
					return
				case <-ticks:
				}
				if ctx.Err() != nil {
					return
				}
				a.postUI(func() {
					if ctx.Err() == nil {
						a.checkIdle()
					}
				})
			}
		}()
	})
}

// checkIdle locks the vault once it has been left alone long enough. It runs on
// the UI thread, where locking is allowed.
func (a *app) checkIdle() {
	if !a.online || a.idleLockDelay() <= 0 {
		a.idleUnlockedAt = time.Time{}
		return
	}
	if a.idleUnlockedAt.IsZero() {
		a.idleUnlockedAt = time.Now()
		return
	}
	if a.idleExpired(time.Now(), a.idleUnlockedAt) {
		a.lock()
	}
}

func (a *app) idleExpired(now time.Time, unlockedAt time.Time) bool {
	return lockWhenIdleAfter(a.online, idleTime(), now.Sub(unlockedAt), a.idleLockDelay())
}
