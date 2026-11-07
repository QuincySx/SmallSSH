package desktop

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestBehaviorPreferencesRoundTripAndRollback(t *testing.T) {
	dir := t.TempDir()
	if err := os.Chmod(dir, 0700); err != nil {
		t.Fatal(err)
	}
	a := &app{dir: dir, online: true}
	no := false
	p := behaviorPreferences{IdleLockEnabled: &no, IdleLockMinutes: 20, SessionIdleEnabled: &no, SessionIdleMinutes: 30}
	if err := a.applyBehaviorPreferences(p); err != nil {
		t.Fatal(err)
	}
	b := &app{dir: dir}
	b.loadBehavior()
	if b.idleLockDelay() != 0 || b.sessionIdleSeconds() != 0 || b.behavior.IdleLockMinutes != 20 || b.behavior.SessionIdleMinutes != 30 {
		t.Fatalf("preferences did not round trip: %+v", b.behavior)
	}
	old := b.behavior
	b.dir = ""
	yes := true
	if err := b.applyBehaviorPreferences(behaviorPreferences{IdleLockEnabled: &yes}); err == nil || b.behavior != old {
		t.Fatal("failed save changed preferences")
	}
}

func TestBehaviorPreferencesInvalidFilesDefault(t *testing.T) {
	for _, data := range []string{"", `{`, `{"version":2}`, `{"version":1} trailing`, strings.Repeat(" ", 8193), `{"version":1,"idleLockEnabled":"false"}`} {
		dir := t.TempDir()
		if data != "" {
			if err := os.WriteFile(filepath.Join(dir, "behavior.json"), []byte(data), 0600); err != nil {
				t.Fatal(err)
			}
		}
		a := &app{dir: dir}
		a.loadBehavior()
		if a.idleLockDelay() != 15*time.Minute || a.sessionIdleSeconds() != 900 {
			t.Fatalf("invalid configuration did not default: %q", data)
		}
	}
	p := behaviorPreferences{IdleLockMinutes: 7, SessionIdleMinutes: 60}
	a := &app{behavior: p}
	if a.idleLockDelay() != 15*time.Minute || a.sessionIdleSeconds() != 900 {
		t.Fatal("invalid minutes did not default")
	}
}

func TestBehaviorIdlePolicyUsesBothClocks(t *testing.T) {
	previous := idleTime
	defer func() { idleTime = previous }()
	idleTime = func() time.Duration { return 30 * time.Minute }
	no := false
	a := &app{online: true, behavior: behaviorPreferences{IdleLockEnabled: &no}}
	start := time.Now()
	if a.idleExpired(start.Add(30*time.Minute), start) {
		t.Fatal("disabled idle lock expired")
	}
	a.behavior = behaviorPreferences{IdleLockMinutes: 20}
	if a.idleExpired(start.Add(15*time.Minute), start) || !a.idleExpired(start.Add(20*time.Minute), start) {
		t.Fatal("20 minute boundary was not respected")
	}
	idleTime = func() time.Duration { return 19 * time.Minute }
	if a.idleExpired(start.Add(30*time.Minute), start) {
		t.Fatal("system idle clock was ignored")
	}
}

func TestBehaviorPreferencesRenameFailurePreservesState(t *testing.T) {
	dir := t.TempDir()
	if err := os.Chmod(dir, 0700); err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(dir, "behavior.json")
	if err := os.Mkdir(target, 0700); err != nil {
		t.Fatal(err)
	}
	yes := true
	original := behaviorPreferences{Version: 1, IdleLockEnabled: &yes, IdleLockMinutes: 20, SessionIdleMinutes: 10}
	unlockedAt := time.Date(2020, 1, 2, 3, 4, 5, 0, time.UTC)
	a := &app{dir: dir, online: true, behavior: original, idleUnlockedAt: unlockedAt}
	no := false
	if err := a.applyBehaviorPreferences(behaviorPreferences{IdleLockEnabled: &no}); err == nil {
		t.Fatal("rename over a directory unexpectedly succeeded")
	}
	if a.behavior != original || !a.idleUnlockedAt.Equal(unlockedAt) {
		t.Fatal("failed rename changed the preferences or idle clock")
	}
	info, err := os.Stat(target)
	if err != nil || !info.IsDir() {
		t.Fatal("failed rename changed the destination directory", err)
	}
	matches, err := filepath.Glob(filepath.Join(dir, ".behavior-*"))
	if err != nil || len(matches) != 0 {
		t.Fatal("failed rename left temporary files", matches, err)
	}
}
