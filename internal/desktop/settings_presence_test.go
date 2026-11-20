package desktop

import (
	"errors"
	"os"
	"testing"

	"github.com/egoist/mygo/ui"
)

func TestPresenceSettingsTesterPersistsChoices(t *testing.T) {
	dir := t.TempDir()
	if err := os.Chmod(dir, 0700); err != nil {
		t.Fatal(err)
	}
	a := &app{dir: dir, page: 4, settingsSection: settingsPresence}
	var events []string
	fakePresence(a, &events)
	a.presence.effects.save = a.applyBehaviorPreferences
	tt := settingsTester(a, 560, 760)
	if err := tt.Click("后台运行方式选择"); err != nil {
		t.Fatal(err)
	}
	if err := tt.Click("关闭窗口后驻留"); err != nil {
		t.Fatal(err)
	}
	if err := tt.Click("菜单栏图标开关"); err != nil {
		t.Fatal(err)
	}
	if err := tt.Click("Dock 图标开关"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if a.behavior.BackgroundMode != "application" || !a.behavior.ShowTrayIcon || behaviorEnabled(a.behavior.ShowDockIcon) {
		t.Fatal("controls did not save", a.behavior)
	}
	b := &app{dir: dir}
	b.loadBehavior()
	if b.behavior.BackgroundMode != "application" || !b.behavior.ShowTrayIcon || behaviorEnabled(b.behavior.ShowDockIcon) {
		t.Fatal("settings did not reload", b.behavior)
	}
}

func TestPresenceSettingsRejectsLosingLastEntry(t *testing.T) {
	a := &app{behavior: behaviorPreferences{BackgroundMode: "application"}}
	var events []string
	fakePresence(a, &events)
	tt := ui.NewTester(func(c *ui.Context) { a.presenceSettings(c) }, 400, 600)
	if err := tt.Click("Dock 图标开关"); err != nil {
		t.Fatal(err)
	}
	if !behaviorEnabled(a.behavior.ShowDockIcon) || a.presence.saveError == "" || len(events) != 0 {
		t.Fatal("lost recovery entry", events)
	}
	if err := tt.Click("关闭后台设置提示"); err != nil || a.presence.saveError != "" {
		t.Fatal("save error cannot be dismissed", err)
	}
	// An unrelated app-wide status never shows up as this page's error.
	a.status = "传输完成"
	tt.Frame()
	if tt.HasText("传输完成") {
		t.Fatal("global status leaked into background settings")
	}
}

func TestPresenceSettingsRecoverFromTrayStartupFailure(t *testing.T) {
	dir := t.TempDir()
	if err := os.Chmod(dir, 0700); err != nil {
		t.Fatal(err)
	}
	no := false
	a := &app{dir: dir, page: 4, settingsSection: settingsPresence, behavior: behaviorPreferences{BackgroundMode: "application", ShowDockIcon: &no, ShowTrayIcon: true}}
	var events []string
	fakePresence(a, &events)
	a.presence.effects.save = a.applyBehaviorPreferences
	a.presence.effects.createTray = func() (presenceTray, error) { return nil, errors.New("synthetic") }
	a.restorePresence()
	if !a.presence.dockFallback || a.presence.warning == "" {
		t.Fatal("missing fallback warning")
	}
	// An unrelated behavior save must preserve the requested tray and warning.
	next := a.behavior
	next.IdleLockMinutes = 20
	if err := a.applyBehaviorPreferences(next); err != nil {
		t.Fatal(err)
	}
	a.status = ""
	tt := settingsTester(a, 560, 760)
	if !tt.HasText("菜单栏图标不可用") || !a.behavior.ShowTrayIcon || behaviorEnabled(a.behavior.ShowDockIcon) {
		t.Fatal("unrelated save erased fallback or requested preference")
	}
	if err := tt.Click("Dock 图标开关"); err != nil {
		t.Fatal(err)
	}
	if !behaviorEnabled(a.behavior.ShowDockIcon) || !a.behavior.ShowTrayIcon || !a.presence.dockFallback || a.presence.warning == "" {
		t.Fatal("could not explicitly retain Dock during tray failure")
	}
	if err := tt.Click("菜单栏图标开关"); err != nil {
		t.Fatal(err)
	}
	if a.behavior.ShowTrayIcon || a.presence.dockFallback || a.presence.warning != "" {
		t.Fatal("could not disable the failed tray")
	}
	b := &app{dir: dir}
	b.loadBehavior()
	if !behaviorEnabled(b.behavior.ShowDockIcon) || b.behavior.ShowTrayIcon || b.behavior.IdleLockMinutes != 20 {
		t.Fatal("recovery choices did not persist")
	}
}

func TestPresenceSettingsRecoverFromTrayFailureWithoutDock(t *testing.T) {
	dir := t.TempDir()
	if err := os.Chmod(dir, 0700); err != nil {
		t.Fatal(err)
	}
	a := &app{dir: dir, page: 4, settingsSection: settingsPresence, behavior: behaviorPreferences{BackgroundMode: "application", ShowTrayIcon: true}}
	var events []string
	fakePresence(a, &events)
	a.presence.effects.dockSupported = false
	a.presence.effects.save = a.applyBehaviorPreferences
	a.presence.effects.createTray = func() (presenceTray, error) { return nil, errors.New("synthetic") }
	a.restorePresence()
	tt := settingsTester(a, 560, 760)
	if !tt.HasText("通知区域图标不可用") {
		t.Fatal("tray failure warning missing")
	}
	if err := tt.Click("后台运行方式选择"); err != nil {
		t.Fatal(err)
	}
	if err := tt.Click("关闭窗口时退出"); err != nil {
		t.Fatal(err)
	}
	if a.behavior.BackgroundMode != "off" || !a.behavior.ShowTrayIcon || !a.presence.trayFailed || a.presence.warning == "" {
		t.Fatal("off did not preserve failed tray request and warning")
	}
	if err := tt.Click("通知区域图标开关"); err != nil {
		t.Fatal(err)
	}
	if a.behavior.ShowTrayIcon || a.presence.trayFailed || a.presence.warning != "" {
		t.Fatal("failed tray could not be disabled")
	}
	b := &app{dir: dir}
	b.loadBehavior()
	if b.behavior.BackgroundMode != "off" || b.behavior.ShowTrayIcon {
		t.Fatal("recovery was not saved")
	}
}
