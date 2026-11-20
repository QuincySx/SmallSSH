package desktop

import (
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"time"

	"sshclient/internal/localipc"
)

type behaviorPreferences struct {
	BackgroundMode     string `json:"backgroundMode,omitempty"`
	ShowDockIcon       *bool  `json:"showDockIcon,omitempty"`
	ShowTrayIcon       bool   `json:"showTrayIcon,omitempty"`
	Version            int    `json:"version"`
	IdleLockEnabled    *bool  `json:"idleLockEnabled"`
	IdleLockMinutes    int    `json:"idleLockMinutes"`
	SessionIdleEnabled *bool  `json:"sessionIdleEnabled"`
	SessionIdleMinutes int    `json:"sessionIdleMinutes"`
	// QuickUnlock keeps a system-verification envelope in the local service's
	// memory after a password unlock; missing means on.
	QuickUnlock *bool `json:"quickUnlock,omitempty"`
}

func behaviorEnabled(value *bool) bool { return value == nil || *value }

func behaviorMinutes(value int, session bool) int {
	switch value {
	case 5, 10, 15, 20, 30:
		return value
	case 60:
		if !session {
			return value
		}
	}
	return 15
}

func (a *app) idleLockDelay() time.Duration {
	if !behaviorEnabled(a.behavior.IdleLockEnabled) {
		return 0
	}
	return time.Duration(behaviorMinutes(a.behavior.IdleLockMinutes, false)) * time.Minute
}

func (a *app) sessionIdleSeconds() int {
	if !behaviorEnabled(a.behavior.SessionIdleEnabled) {
		return 0
	}
	return behaviorMinutes(a.behavior.SessionIdleMinutes, true) * 60
}

func (a *app) loadBehavior() {
	a.behavior = behaviorPreferences{}
	if a.dir == "" {
		return
	}
	name := filepath.Join(a.dir, "behavior.json")
	info, err := os.Lstat(name)
	if err != nil || !info.Mode().IsRegular() || info.Size() > 8192 {
		return
	}
	f, err := os.Open(name)
	if err != nil {
		return
	}
	defer f.Close()
	info, err = f.Stat()
	if err != nil || !info.Mode().IsRegular() || info.Size() > 8192 {
		return
	}
	data, err := io.ReadAll(io.LimitReader(f, 8193))
	if err != nil || len(data) > 8192 {
		return
	}
	var p behaviorPreferences
	if json.Unmarshal(data, &p) != nil || p.Version != 1 {
		return
	}
	if p.BackgroundMode != "application" {
		p.BackgroundMode = "off"
	}
	p.IdleLockMinutes = behaviorMinutes(p.IdleLockMinutes, false)
	p.SessionIdleMinutes = behaviorMinutes(p.SessionIdleMinutes, true)
	a.behavior = p
}

func (a *app) applyBehaviorPreferences(next behaviorPreferences) error {
	if a.dir == "" {
		return errors.New("empty preferences directory")
	}
	next.Version = 1
	next.IdleLockMinutes = behaviorMinutes(next.IdleLockMinutes, false)
	next.SessionIdleMinutes = behaviorMinutes(next.SessionIdleMinutes, true)
	if err := localipc.Prepare(a.dir); err != nil {
		return err
	}
	f, err := os.CreateTemp(a.dir, ".behavior-")
	if err != nil {
		return err
	}
	name := f.Name()
	defer os.Remove(name)
	err = json.NewEncoder(f).Encode(next)
	closed := f.Close()
	if err != nil {
		return err
	}
	if closed != nil {
		return closed
	}
	if err = os.Rename(name, filepath.Join(a.dir, "behavior.json")); err != nil {
		return err
	}
	oldDelay := a.idleLockDelay()
	a.behavior = next
	if a.idleLockDelay() != oldDelay {
		a.idleUnlockedAt = time.Time{}
		if a.online && a.idleLockDelay() > 0 {
			a.idleUnlockedAt = time.Now()
		}
	}
	return nil
}
