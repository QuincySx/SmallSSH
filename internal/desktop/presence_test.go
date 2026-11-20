package desktop

import (
	"errors"
	"reflect"
	"testing"
)

type fakePresenceTray struct{ events *[]string }

func (f *fakePresenceTray) destroy() { *f.events = append(*f.events, "destroy") }
func (f *fakePresenceTray) relabel() { *f.events = append(*f.events, "relabel") }

func fakePresence(a *app, events *[]string) {
	a.presence.effects = &presenceEffects{
		dockSupported: true, traySupported: true,
		createTray: func() (presenceTray, error) {
			*events = append(*events, "create")
			return &fakePresenceTray{events}, nil
		},
		setDock: func(on bool) {
			if on {
				*events = append(*events, "dock-on")
			} else {
				*events = append(*events, "dock-off")
			}
		},
		hide: func() { *events = append(*events, "hide") }, mainNeedsRestore: func() bool { return true }, show: func() { *events = append(*events, "show") }, focus: func() { *events = append(*events, "focus") },
		post: func(f func()) { f() }, save: func(p behaviorPreferences) error { *events = append(*events, "save"); a.behavior = p; return nil },
	}
}

func TestPresenceEntryTransactionAndFailures(t *testing.T) {
	a := &app{}
	var events []string
	fakePresence(a, &events)
	no := false
	p := behaviorPreferences{BackgroundMode: "application", ShowDockIcon: &no, ShowTrayIcon: true}
	if err := a.applyPresence(p); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(events, []string{"create", "save", "dock-on"}) {
		t.Fatal("incorrect entry ordering", events)
	}
	events = nil
	yes := true
	p.ShowDockIcon, p.ShowTrayIcon = &yes, false
	if err := a.applyPresence(p); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(events, []string{"save", "dock-on", "destroy"}) {
		t.Fatal("old entry removed before new entry", events)
	}
	events = nil
	p.ShowDockIcon, p.ShowTrayIcon = &no, true
	a.presence.effects.createTray = func() (presenceTray, error) {
		events = append(events, "create-failed")
		return nil, errors.New("synthetic")
	}
	if a.applyPresence(p) == nil || !reflect.DeepEqual(events, []string{"create-failed"}) || !behaviorEnabled(a.behavior.ShowDockIcon) {
		t.Fatal("tray failure lost Dock", events)
	}
	events = nil
	a.presence.effects.createTray = func() (presenceTray, error) {
		events = append(events, "create")
		return &fakePresenceTray{&events}, nil
	}
	a.presence.effects.save = func(behaviorPreferences) error {
		events = append(events, "save-failed")
		return errors.New("synthetic")
	}
	if a.applyPresence(p) == nil || !reflect.DeepEqual(events, []string{"create", "save-failed", "destroy"}) || !behaviorEnabled(a.behavior.ShowDockIcon) {
		t.Fatal("failed save changed entry", events)
	}
}

func TestPresenceCloseQuitVetoAndRestore(t *testing.T) {
	a := &app{behavior: behaviorPreferences{BackgroundMode: "application"}}
	var events []string
	var pending []func()
	fakePresence(a, &events)
	a.presence.effects.post = func(f func()) { pending = append(pending, f) }
	if !a.presenceClose(func() { events = append(events, "prevent") }) || !reflect.DeepEqual(events, []string{"prevent", "hide", "dock-on"}) {
		t.Fatal("normal close did not hide", events)
	}
	a.beginPresenceQuit()
	if a.presenceClose(func() { events = append(events, "prevent") }) {
		t.Fatal("quit was vetoed")
	}
	a.beginPresenceQuit()
	pending[0]()
	if !a.presence.quitting || a.presenceClose(func() { events = append(events, "prevent") }) {
		t.Fatal("old reset changed a newer quit")
	}
	pending[1]()
	if !a.presenceClose(func() { events = append(events, "prevent") }) {
		t.Fatal("veto did not restore close behavior")
	}
	a.behavior.BackgroundMode = "off"
	if a.presenceClose(func() { events = append(events, "prevent") }) {
		t.Fatal("off should permit close")
	}
}

func TestPresenceStartupFailureAndExplicitRestore(t *testing.T) {
	no := false
	a := &app{behavior: behaviorPreferences{BackgroundMode: "application", ShowDockIcon: &no, ShowTrayIcon: true}, startup: startupState{background: true}}
	var events []string
	fakePresence(a, &events)
	a.presence.effects.createTray = func() (presenceTray, error) {
		events = append(events, "create-failed")
		return nil, errors.New("synthetic")
	}
	a.presenceActivate(false)
	a.showFromPresence()
	a.restorePresence()
	if err := a.applyPresence(a.behavior); err == nil {
		t.Fatal("hidden development startup accepted a presence change")
	}
	if len(events) != 0 {
		t.Fatal("development background was revealed", events)
	}
	a.startup.revealed = true
	a.restorePresence()
	if behaviorEnabled(a.behavior.ShowDockIcon) || !a.behavior.ShowTrayIcon || !a.presence.dockFallback || a.presence.warning == "" || !reflect.DeepEqual(events, []string{"create-failed", "dock-on"}) {
		t.Fatal("startup fallback failed or changed requested settings", events)
	}
	events = nil
	a.showFromPresence()
	if !reflect.DeepEqual(events, []string{"dock-on", "show", "focus"}) {
		t.Fatal("explicit restore failed", events)
	}
}

func TestPresenceOffKeepsDockWhileAWindowIsOpen(t *testing.T) {
	no := false
	a := &app{}
	var events []string
	fakePresence(a, &events)
	p := behaviorPreferences{BackgroundMode: "off", ShowDockIcon: &no}
	if err := a.applyPresence(p); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(events, []string{"save", "dock-on"}) {
		t.Fatal("off mode hid the visible workbench from Dock", events)
	}
	events = nil
	a.restorePresence()
	if !reflect.DeepEqual(events, []string{"dock-on"}) || a.presence.dockFallback {
		t.Fatal("off startup did not keep the visible workbench in Dock", events)
	}
}

func TestPresenceActivateRestoresMainEvenWhenOtherWindowVisible(t *testing.T) {
	for _, mainState := range []string{"hidden", "minimized"} {
		t.Run(mainState, func(t *testing.T) {
			a := &app{}
			var events []string
			fakePresence(a, &events)
			a.presence.effects.mainNeedsRestore = func() bool { return true }
			a.presenceActivate(true)
			if !reflect.DeepEqual(events, []string{"dock-on", "show", "focus"}) {
				t.Fatal("main window was not restored", events)
			}
		})
	}
}

func TestPresenceBackgroundKeepsApprovalQueuedUntilUserRestores(t *testing.T) {
	a, r, now := popupFixture()
	a.behavior.BackgroundMode = "application"
	a.requests = append(a.requests, r)
	var events []string
	fakePresence(a, &events)
	if !a.presenceClose(func() {}) || !a.presence.backgrounded {
		t.Fatal("close did not enter background")
	}
	if _, ok := a.nextApprovalPopup(now); ok || a.notified[approvalNoticeKey(r)] {
		t.Fatal("background request consumed a notification")
	}
	a.showApproval(r) // The background guard must return before native UI calls.
	a.showFromPresence()
	if a.presence.backgrounded {
		t.Fatal("explicit restore did not leave background")
	}
	if got, ok := a.nextApprovalPopup(now); !ok || got.ID != r.ID {
		t.Fatal("restored request did not become eligible")
	}
}

func TestPresenceRequiresRealTrayWithoutDock(t *testing.T) {
	a := &app{behavior: behaviorPreferences{BackgroundMode: "application"}}
	var events []string
	fakePresence(a, &events)
	a.presence.effects.dockSupported = false
	prevented := false
	if !a.presenceClose(func() { prevented = true }) || !prevented || a.presence.backgrounded || len(events) != 0 || a.presence.warning == "" {
		t.Fatal("unrecoverable close hid the window", events)
	}
	a.presence.quitting = true
	prevented = false
	if a.presenceClose(func() { prevented = true }) || prevented {
		t.Fatal("quit did not pass through")
	}
	a.presence.quitting = false
	p := behaviorPreferences{BackgroundMode: "application"}
	if a.applyPresence(p) == nil {
		t.Fatal("saved application mode without an entry")
	}
	p.ShowTrayIcon = true
	a.presence.effects.createTray = func() (presenceTray, error) { return nil, errors.New("synthetic") }
	if a.applyPresence(p) == nil {
		t.Fatal("saved application mode with a failed tray")
	}
	a.behavior = p
	a.restorePresence()
	prevented = false
	if !a.presenceClose(func() { prevented = true }) || !prevented || a.presence.backgrounded || len(events) != 0 {
		t.Fatal("tray failure hid the window", events)
	}
	a.presence.effects.createTray = func() (presenceTray, error) {
		events = append(events, "create")
		return &fakePresenceTray{&events}, nil
	}
	if err := a.applyPresence(p); err != nil {
		t.Fatal(err)
	}
	events = nil
	prevented = false
	if !a.presenceClose(func() { prevented = true }) || !prevented || !a.presence.backgrounded || !reflect.DeepEqual(events, []string{"hide"}) {
		t.Fatal("working tray did not allow backgrounding", events)
	}
}
