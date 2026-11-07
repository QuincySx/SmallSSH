package desktop

import (
	"os"
	"testing"

	"github.com/egoist/mygo/ui"
	"sshclient/internal/desktop/components"
)

func TestIdleSettingsSwitchAndSelectionPersist(t *testing.T) {
	dir := t.TempDir()
	if err := os.Chmod(dir, 0700); err != nil {
		t.Fatal(err)
	}
	a := &app{dir: dir}
	tt := ui.NewTester(func(c *ui.Context) {
		components.ApplyTheme(c, false)
		ui.Column(c).Fill().Padding(16).Children(func() { a.idleSettings(c) })
	}, 400, 800)
	if err := tt.Click("保险库闲置等待时间"); err != nil {
		t.Fatal(err)
	}
	if err := tt.Click("20 分钟"); err != nil {
		t.Fatal(err)
	}
	if err := tt.Click("保险库闲置自动锁定开关"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if a.idleLockDelay() != 0 || a.behavior.IdleLockMinutes != 20 {
		t.Fatal("switch failed to preserve selected minutes")
	}
	b := &app{dir: dir}
	b.loadBehavior()
	if b.idleLockDelay() != 0 || b.behavior.IdleLockMinutes != 20 {
		t.Fatal("redrawn preference did not persist")
	}
	if err := tt.Click("保险库闲置自动锁定开关"); err != nil {
		t.Fatal(err)
	}
	if a.behavior.IdleLockMinutes != 20 || a.idleLockDelay().Minutes() != 20 {
		t.Fatal("reenabling lost the selection")
	}
}
