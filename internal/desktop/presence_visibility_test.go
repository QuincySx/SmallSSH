package desktop

import (
	"fmt"
	"testing"
	"time"
)

func TestPresenceForegroundDockIsIndependentOfBackgroundPreference(t *testing.T) {
	no := false
	a := &app{behavior: behaviorPreferences{BackgroundMode: "application", ShowDockIcon: &no, ShowTrayIcon: true}}
	var events []string
	fakePresence(a, &events)
	a.restorePresence()
	if len(events) == 0 || events[len(events)-1] != "dock-on" {
		t.Fatal("visible workbench missing from Dock", events)
	}
	events = nil
	if !a.presenceClose(func() {}) {
		t.Fatal("did not enter residency")
	}
	if len(events) == 0 || events[len(events)-1] != "dock-off" {
		t.Fatal("background Dock preference ignored", events)
	}
	events = nil
	a.showFromPresence()
	if len(events) < 3 || events[0] != "dock-on" || events[1] != "show" {
		t.Fatal("Dock was not restored before showing the workbench", events)
	}
	if behaviorEnabled(a.behavior.ShowDockIcon) {
		t.Fatal("foreground visibility rewrote background preference")
	}
}

func TestPresenceHidesOwnedWindowsAndNeverRevivesAClosedVNC(t *testing.T) {
	no := false
	a := &app{behavior: behaviorPreferences{BackgroundMode: "application", ShowDockIcon: &no, ShowTrayIcon: true}}
	var events []string
	fakePresence(a, &events)
	a.restorePresence()
	tools, file, hidden := &fakeToolsWindow{}, &fakeToolsWindow{}, &fakeToolsWindow{}
	tools.Show()
	file.Show()
	a.toolsWin, a.editorWin, a.settingsWin = tools, file, hidden
	viewer := &fakeToolsWindow{onClosed: func() {}}
	viewer.Show()
	cancelled := 0
	run := &vncRun{window: viewer, cancel: func() { cancelled++ }}
	a.vnc.runs = []*vncRun{run}
	a.presenceClose(func() {})
	if tools.IsVisible() || file.IsVisible() || viewer.IsVisible() || hidden.hidden != 0 {
		t.Fatal("auxiliary windows left on desktop", tools.visible, file.visible, viewer.visible, hidden.hidden)
	}
	if cancelled != 0 || viewer.closed != 0 {
		t.Fatal("hiding disconnected VNC")
	}
	a.stopVNC(run)
	viewer.finishPendingUpdates()
	a.showFromPresence()
	if !tools.IsVisible() || !file.IsVisible() || hidden.shown != 0 {
		t.Fatal("restore did not preserve the previously visible window set")
	}
	if viewer.shown != 1 || viewer.closed != 1 || cancelled != 1 {
		t.Fatal("closed VNC window resurrected", viewer.shown, viewer.closed, cancelled)
	}
}

func TestVNCLateWindowCreationStaysHiddenDuringResidency(t *testing.T) {
	a, updates, windows := vncUIFixture(t)
	t.Cleanup(a.closeVNCs)
	no := false
	a.behavior = behaviorPreferences{BackgroundMode: "application", ShowDockIcon: &no, ShowTrayIcon: true}
	var events []string
	fakePresence(a, &events)
	a.restorePresence()
	a.startVNC()
	a.presenceClose(func() {})
	select {
	case update := <-updates:
		update()
	case <-time.After(5 * time.Second):
		t.Fatal("synthetic VNC creation did not finish")
	}
	if len(*windows) != 1 || (*windows)[0].shown != 0 {
		t.Fatal("late VNC creation leaked a desktop window", len(*windows))
	}
	a.showFromPresence()
	if (*windows)[0].shown != 1 {
		t.Fatal("requested VNC window did not restore")
	}
}

func TestPresenceVisibilityNeverRevealsHiddenDevelopmentStartup(t *testing.T) {
	no := false
	a := &app{startup: startupState{background: true}, behavior: behaviorPreferences{BackgroundMode: "application", ShowDockIcon: &no, ShowTrayIcon: true}}
	var events []string
	fakePresence(a, &events)
	window := &fakeToolsWindow{}
	a.toolsWin = window
	if a.showPresenceWindow(window) {
		t.Fatal("hidden startup showed a window")
	}
	a.showFromPresence()
	if len(events) != 0 || window.shown != 0 {
		t.Fatal("hidden startup created a Dock/window entry", events)
	}
	a.startup.revealed = true
	a.restorePresence()
	a.showFromPresence()
	if window.shown != 1 {
		t.Fatal("explicit development reveal lost pending window")
	}
}

func TestPresenceModeAndDockEntryMatrix(t *testing.T) {
	for _, mode := range []string{"off", "application"} {
		for _, dock := range []bool{false, true} {
			for _, tray := range []bool{false, true} {
				t.Run(fmt.Sprintf("%s-dock-%t-tray-%t", mode, dock, tray), func(t *testing.T) {
					a := &app{}
					var events []string
					fakePresence(a, &events)
					p := behaviorPreferences{BackgroundMode: mode, ShowDockIcon: &dock, ShowTrayIcon: tray}
					err := a.applyPresence(p)
					if mode == "application" && !dock && !tray {
						if err == nil {
							t.Fatal("unrecoverable residency accepted")
						}
						return
					}
					if err != nil {
						t.Fatal(err)
					}
					lastDock := func() string {
						for i := len(events) - 1; i >= 0; i-- {
							if events[i] == "dock-on" || events[i] == "dock-off" {
								return events[i]
							}
						}
						return ""
					}
					if lastDock() != "dock-on" {
						t.Fatal("open workbench had no Dock entry", events)
					}
					prevented := a.presenceClose(func() {})
					if mode == "off" {
						if prevented || a.presence.backgrounded {
							t.Fatal("exit mode entered residency")
						}
						return
					}
					if !prevented || !a.presence.backgrounded {
						t.Fatal("residency not entered")
					}
					want := "dock-off"
					if dock {
						want = "dock-on"
					}
					if lastDock() != want {
						t.Fatal("residency ignored its entry preference", events)
					}
					a.showFromPresence()
					if a.presence.backgrounded || lastDock() != "dock-on" {
						t.Fatal("restore did not regain its Dock entry", events)
					}
				})
			}
		}
	}
}
