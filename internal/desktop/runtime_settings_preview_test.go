//go:build preview_qa

package desktop

import (
	"fmt"
	"image/png"
	"os"
	"path/filepath"
	"testing"

	"github.com/egoist/mygo/ui"
)

func TestRuntimeSettingsSnapshotsWithoutWindows(t *testing.T) {
	if os.Getenv("SSH_CLIENT_PREVIEW_QA") != "1" {
		t.Skip("opt-in offline runtime settings snapshots")
	}
	dir := os.Getenv("SSH_CLIENT_UI_SNAPSHOT_DIR")
	if !filepath.IsAbs(dir) {
		t.Fatal("absolute snapshot directory required")
	}
	if err := os.MkdirAll(dir, 0700); err != nil {
		t.Fatal(err)
	}
	write := func(name string, tt *ui.Tester) {
		t.Helper()
		path := filepath.Join(dir, name+".png")
		f, err := os.OpenFile(path, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0600)
		if err != nil {
			t.Fatal(err)
		}
		err = png.Encode(f, tt.Image())
		closeErr := f.Close()
		if err != nil || closeErr != nil {
			t.Fatal(err, closeErr)
		}
		t.Log(path)
	}
	no := false
	variants := []struct {
		name, section string
		behavior      behaviorPreferences
		labels        []string
	}{
		{"security-idle-off", settingsSecurity, behaviorPreferences{IdleLockEnabled: &no, IdleLockMinutes: 20, SessionIdleEnabled: &no, SessionIdleMinutes: 15}, []string{"保险库闲置自动锁定开关", "保险库闲置等待时间", "新 CLI 会话闲置断开开关", "新 CLI 会话默认时长"}},
		{"security-idle-custom", settingsSecurity, behaviorPreferences{IdleLockMinutes: 20, SessionIdleMinutes: 5}, []string{"保险库闲置自动锁定开关", "保险库闲置等待时间", "新 CLI 会话闲置断开开关", "新 CLI 会话默认时长"}},
		{"presence-application", settingsPresence, behaviorPreferences{BackgroundMode: "application", ShowDockIcon: &no, ShowTrayIcon: true}, []string{"后台运行方式选择", "Dock 图标开关", "菜单栏图标开关"}},
		{"presence-off", settingsPresence, behaviorPreferences{BackgroundMode: "off"}, []string{"后台运行方式选择", "Dock 图标开关", "菜单栏图标开关"}},
	}
	for _, variant := range variants {
		for _, size := range []struct{ width, height int }{{1280, 900}, {560, 760}} {
			name := fmt.Sprintf("runtime-%s-%dx%d", variant.name, size.width, size.height)
			t.Run(name, func(t *testing.T) {
				a := &app{page: 4, settingsSection: variant.section, behavior: variant.behavior}
				var events []string
				fakePresence(a, &events)
				tt := settingsTester(a, size.width, size.height)
				for _, label := range variant.labels {
					r, ok := tt.Find(label)
					if !ok || r.X < 0 || r.X+r.W > float32(size.width) {
						t.Fatal("missing or horizontally clipped control", label, r)
					}
				}
				write(name, tt)
			})
		}
	}
	for _, style := range []struct {
		name, locale string
		dark         bool
	}{
		{"light", "zh-Hans", false}, {"dark", "zh-Hans", true}, {"english", "en", false},
	} {
		t.Run("presence-minimum-"+style.name, func(t *testing.T) {
			a := &app{page: 4, settingsSection: settingsPresence, behavior: behaviorPreferences{BackgroundMode: "application", ShowDockIcon: &no, ShowTrayIcon: true}}
			var events []string
			fakePresence(a, &events)
			tt := settingsTester(a, 720, 640)
			tt.SetLocale(style.locale)
			tt.SetDark(style.dark)
			label := "驻留时显示 Dock"
			if style.locale == "en" {
				label = "Keep Dock icon in background"
			}
			bounds, ok := tt.Find(label)
			if !ok || bounds.H < 11 || bounds.X < 0 || bounds.X+bounds.W > 720 || bounds.Y+bounds.H > 640 {
				t.Fatal("presence label clipped", label, bounds)
			}
			write("runtime-presence-minimum-"+style.name, tt)
		})
	}
	a, r := reviewFixture("full")
	a.behavior.SessionIdleEnabled = &no
	state := &approvalPromptState{}
	tt := ui.NewTester(func(c *ui.Context) { a.approvalPrompt(c, r, state, func(bool, bool) {}, func() {}) }, 420, 620)
	for _, label := range []string{"会话闲置自动断开开关", "闲置不断开"} {
		bounds, ok := tt.Find(label)
		if !ok || bounds.X < 0 || bounds.X+bounds.W > 420 {
			t.Fatal("missing or horizontally clipped approval control", label, bounds)
		}
	}
	write("runtime-approval-idle-off-420x620", tt)
}
