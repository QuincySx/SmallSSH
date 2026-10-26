package components

import (
	"testing"

	"github.com/egoist/mygo/ui"
)

func TestAuthenticationPasswordHasDirectAccessibleAction(t *testing.T) {
	for _, width := range []int{200, 320} {
		password := "isolated-password-value"
		actions, submissions := 0, 0
		disabled := false
		tt := ui.NewTester(func(c *ui.Context) {
			ui.Column(c).Fill().Padding(4).Children(func() {
				input, clicked := AuthenticationPassword(c, &password, "Password", "Touch ID", true, disabled)
				if clicked {
					actions++
				}
				if input.Submitted() {
					submissions++
				}
			})
		}, width, 70)
		for _, label := range []string{"Password", "Touch ID"} {
			bounds, ok := tt.Find(label)
			if !ok || bounds.H < 30 || bounds.W < 30 || bounds.X < 0 || bounds.X+bounds.W > float32(width) {
				t.Fatal("password control is clipped", width, label, bounds)
			}
		}
		if err := tt.Click("Touch ID"); err != nil {
			t.Fatal(err)
		}
		if actions != 1 || submissions != 0 || password != "isolated-password-value" {
			t.Fatal("direct system action submitted or changed the password")
		}
		if err := tt.Click("Password"); err != nil {
			t.Fatal(err)
		}
		// This is the tester's isolated clipboard, not the system clipboard.
		tt.SetClipboard("unchanged-clipboard")
		tt.Command("selectAll")
		tt.Command("copy")
		if tt.Clipboard() != "unchanged-clipboard" {
			t.Fatal("password input allowed copying its secret")
		}
		tt.Key(0, ui.KeyEnter)
		if submissions != 1 || actions != 1 {
			t.Fatal("password Enter did not stay on the password route")
		}
		disabled = true
		tt.Frame()
		if err := tt.Click("Touch ID"); err != nil {
			t.Fatal(err)
		}
		tt.Key(0, ui.KeyEnter)
		if actions != 1 || submissions != 1 {
			t.Fatal("disabled password control accepted an action")
		}
	}
}

func TestAuthenticationPasswordWithoutSystemActionUsesEntireField(t *testing.T) {
	password := ""
	tt := ui.NewTester(func(c *ui.Context) {
		ui.Column(c).Fill().Children(func() {
			_, clicked := AuthenticationPassword(c, &password, "Password", "Touch ID", false, false)
			if clicked {
				t.Fatal("unavailable system verification was invoked")
			}
		})
	}, 240, 50)
	if tt.HasText("Touch ID") {
		t.Fatal("unavailable system action was displayed")
	}
	bounds, ok := tt.Find("Password")
	if !ok || bounds.W < 230 {
		t.Fatal("password-only field reserved empty action space", bounds)
	}
}

func TestAuthenticationPasswordNativeAnchorKeepsButtonAndKeyboard(t *testing.T) {
	password := ""
	var bounds ui.Rect
	clicks, submissions := 0, 0
	tt := ui.NewTester(func(c *ui.Context) {
		ui.Column(c).Fill().Children(func() {
			input, action := AuthenticationPasswordWithAnchor(c, &password, "Password", "Touch ID", true, false, true)
			bounds = action.Bounds()
			if action.Clicked() {
				clicks++
			}
			if input.Submitted() {
				submissions++
			}
		})
	}, 240, 60)
	tt.Frame()
	if bounds.W != 36 || bounds.H != 34 || bounds.X < 0 || bounds.X+bounds.W > 240 {
		t.Fatal("native authentication anchor moved outside its button", bounds)
	}
	if err := tt.Click("Touch ID"); err != nil {
		t.Fatal(err)
	}
	if clicks != 1 || submissions != 0 {
		t.Fatal("native icon placeholder lost its button action")
	}
	if err := tt.Click("Password"); err != nil {
		t.Fatal(err)
	}
	tt.Type("isolated-native-anchor-password")
	tt.Key(0, ui.KeyEnter)
	if clicks != 1 || submissions != 1 {
		t.Fatal("native placeholder interfered with password keyboard handling")
	}
}
