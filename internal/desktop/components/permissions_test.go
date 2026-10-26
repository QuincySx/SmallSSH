package components

import (
	"github.com/egoist/mygo/ui"
	"testing"
)

func TestPermissionMatrixKeyboardAndEditableAccountKeepNumericInput(t *testing.T) {
	checks := PermissionChecks(0644)
	owner := "service"
	tt := ui.NewTester(func(c *ui.Context) {
		ui.Column(c).Fill().Padding(16).Gap(12).Children(func() {
			PermissionMatrix(c, &checks, false)
			EditableChoice(c, &owner, []string{"root", "service"}, "所属用户", false)
			Button(c, "其他控件")
		})
	}, 480, 360)
	if err := tt.Click("用户组 · 写"); err != nil {
		t.Fatal(err)
	}
	if PermissionMode(checks) != 0664 {
		t.Fatal("checkbox did not change group permission", PermissionMode(checks))
	}
	tt.Key(0, ui.KeySpace)
	if PermissionMode(checks) != 0644 {
		t.Fatal("Space did not toggle focused permission")
	}
	if err := tt.Click("选择所属用户"); err != nil {
		t.Fatal(err)
	}
	tt.Key(ui.Cmd, ui.KeyA)
	tt.Type("12345")
	if err := tt.Click("其他控件"); err != nil {
		t.Fatal(err)
	}
	if owner != "12345" {
		t.Fatal("numeric UID was discarded on blur", owner)
	}
	if err := tt.Click("所属用户"); err != nil {
		t.Fatal(err)
	}
	tt.Key(ui.Cmd, ui.KeyA)
	tt.Key(0, ui.KeyBackspace)
	if err := tt.Click("root"); err != nil {
		t.Fatal(err)
	}
	if owner != "root" {
		t.Fatal("existing account choice did not apply", owner)
	}
}
