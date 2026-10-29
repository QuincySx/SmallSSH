package desktop

import (
	"sync"
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
	if !online || idle <= 0 || unlockedFor <= 0 {
		return false
	}
	return idle >= idleLockAfter && unlockedFor >= idleLockAfter
}

// watchIdle starts the watcher once per process, from the first rendered frame.
//
// The clock itself is the only thing that runs off the UI thread: every read of
// app state, and the lock, happen in a UI callback, so nothing here touches
// app.online or the power API from two goroutines at once.
func (a *app) watchIdle() {
	idleWatch.Do(func() {
		var done <-chan struct{}
		if a.ctx != nil {
			done = a.ctx.Done()
		}
		go func() {
			ticker := time.NewTicker(idleTick)
			defer ticker.Stop()
			for {
				select {
				case <-done:
					return
				case <-ticker.C:
				}
				a.postUI(a.checkIdle)
			}
		}()
	})
}

// checkIdle locks the vault once it has been left alone long enough. It runs on
// the UI thread, where locking is allowed.
func (a *app) checkIdle() {
	if !a.online {
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
	return lockWhenIdle(a.online, idleTime(), now.Sub(unlockedAt))
}

var idleWatch sync.Once
