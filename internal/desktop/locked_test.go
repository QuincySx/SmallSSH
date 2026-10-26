package desktop

import (
	"context"
	"math"
	"path/filepath"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"
	"sshclient/internal/broker"
	"sshclient/internal/systemauth"
	"sshclient/internal/vault"
)

func TestVaultLandingCenteredAndControlsFitAcrossStates(t *testing.T) {
	for _, variant := range []string{"system", "password", "create"} {
		for _, dark := range []bool{false, true} {
			create := variant == "create"
			a := &app{ctx: context.Background(), create: create}
			if variant != "password" {
				a.authStatus = systemauth.Status{Enabled: true, Available: true, Name: "Touch ID"}
			}
			tt := ui.NewTester(a.view, 1000, 760)
			tt.SetDark(dark)
			card, ok := tt.Find("保险库卡片")
			if !ok || math.Abs(float64(card.X+card.W/2-500)) > 1 || math.Abs(float64(card.Y+card.H/2-380)) > 30 || card.W > 394 || card.X < 0 || card.Y < 0 || card.Y+card.H > 760 {
				t.Fatal("vault card is not centered or exceeds minimum window", variant, dark, card)
			}
			if variant == "system" {
				if tt.HasText("主密码") || tt.HasText("确认解锁") || card.H > 230 {
					t.Fatal("system-ready landing did not stay compact", dark, card)
				}
				method, _ := tt.Find("触控 ID")
				if err := tt.Click("触控 ID"); err != nil {
					t.Fatal(err)
				}
				card, _ = tt.Find("保险库卡片")
				if !a.unlockState.passwordMode || a.busy || !tt.HasText("主密码") {
					t.Fatal("system method did not expand the password form")
				}
				action, ok := tt.Find("触控 ID")
				if !ok || math.Abs(float64(action.X+action.W-method.X-method.W)) > 1 || action.H < 32 {
					t.Fatal("expanded fingerprint action moved away from its original slot", method, action)
				}
			}
			for _, label := range []string{"主密码", "确认解锁"} {
				r, ok := tt.Find(label)
				if !ok || r.H < 36 || r.X < card.X || r.Y < card.Y || r.X+r.W > card.X+card.W || r.Y+r.H > card.Y+card.H {
					t.Fatal("unlock control outside card or too small", label, r)
				}
			}
			if err := tt.Click("确认解锁"); err != nil {
				t.Fatal(err)
			}
			if a.busy {
				t.Fatal("empty password submitted")
			}
			if create {
				if !tt.HasText("确认主密码") || !tt.HasText("请使用较长、独立的主密码") || tt.HasText("触控 ID") {
					t.Fatal("creation controls are incorrect")
				}
			} else if tt.HasText("触控 ID") != (variant == "system") {
				t.Fatal("password field system action does not match availability")
			}
			a.pass = "test-only-secret-not-visible"
			a.confirm = a.pass
			a.busy, a.unlockState.busy = true, true
			tt.Frame()
			tt.Key(0, ui.KeyEnter)
			if a.pass != "test-only-secret-not-visible" || !a.busy {
				t.Fatal("busy form submitted again")
			}
			if tt.HasText("test-only-secret-not-visible") {
				t.Fatal("password was rendered as text")
			}
		}
	}
}

func TestVaultLandingEnterExpandsPasswordWithoutAuthentication(t *testing.T) {
	a := &app{ctx: context.Background(), authStatus: systemauth.Status{Enabled: true, Available: true, Name: "Touch ID"}}
	tt := ui.NewTester(a.view, 1000, 760)
	if a.busy || a.unlockState.busy || a.unlockState.systemAttempt {
		t.Fatal("offline rendering started authentication")
	}
	tt.Key(0, ui.KeyEnter)
	if !a.unlockState.passwordMode || !tt.HasText("主密码") || !tt.HasText("确认解锁") || a.busy {
		t.Fatal("Enter did not expand the password form without submitting")
	}
	tt.Type("isolated-keyboard-fallback")
	if a.pass != "isolated-keyboard-fallback" || tt.HasText(a.pass) {
		t.Fatal("expanded password field did not retain focus or hide its value")
	}
}

func TestVaultLandingPasswordChoiceSurvivesSystemStatusArrival(t *testing.T) {
	a := &app{ctx: context.Background()}
	tt := ui.NewTester(a.view, 1000, 760)
	tt.Type("isolated-password-choice")
	a.authStatus = systemauth.Status{Enabled: true, Available: true, Name: "Touch ID"}
	tt.Frame()
	if !a.unlockState.passwordMode || !tt.HasText("主密码") || a.pass != "isolated-password-choice" {
		t.Fatal("asynchronous system status hid or reset an edited password")
	}
}

func TestVaultLandingTypingCancelsEmbeddedTouchIDInPlace(t *testing.T) {
	a := &app{ctx: context.Background(), authStatus: systemauth.Status{Enabled: true, Available: true, Name: "Touch ID"}}
	state := a.ensureUnlockState()
	cancelled := 0
	state.passwordMode, state.busy, state.systemAttempt = true, true, true
	state.cancel = func() { cancelled++ }
	state.presentation = systemauth.NewEmbeddedPresentation(func(fn func()) { fn() })
	t.Cleanup(state.presentation.Close)
	a.busy = true
	tt := ui.NewTester(a.view, 1000, 760)
	if err := tt.Click("主密码"); err != nil {
		t.Fatal(err)
	}
	tt.Type("isolated-native-password-fallback")
	if cancelled != 1 || a.busy || state.busy || state.systemAttempt || !state.passwordMode || a.pass != "isolated-native-password-fallback" {
		t.Fatal("typing required a separate cancellation or lost the fallback password")
	}
	if !tt.HasText("主密码") || !tt.HasText("触控 ID") || tt.HasText(a.pass) {
		t.Fatal("native cancellation hid the password route or exposed its value")
	}
}

func TestVaultLandingEnterShowsWrongPasswordErrorInsideCard(t *testing.T) {
	dir := t.TempDir()
	v, err := vault.New([]byte("isolated landing test master password"))
	if err != nil {
		t.Fatal(err)
	}
	if err = vault.Save(broker.VaultPath(dir), v, true); err != nil {
		t.Fatal(err)
	}
	v.Close()
	updates := make(chan func(), 4)
	a := &app{ctx: context.Background(), dir: dir, dispatch: func(fn func()) { updates <- fn }}
	tt := ui.NewTester(a.view, 1280, 900)
	if err = tt.Click("主密码"); err != nil {
		t.Fatal(err)
	}
	tt.Type("incorrect synthetic password")
	tt.Key(0, ui.KeyEnter)
	if !a.busy {
		t.Fatal("Enter did not submit the password field")
	}
	select {
	case update := <-updates:
		update()
	case <-time.After(15 * time.Second):
		t.Fatal("unlock error did not return")
	}
	tt.Frame()
	card, _ := tt.Find("保险库卡片")
	feedback, ok := tt.Find("解锁错误")
	if a.online || a.busy || a.pass != "" || !ok || a.unlockError == "" || feedback.Y < card.Y || feedback.Y+feedback.H > card.Y+card.H {
		t.Fatal("failed unlock did not show inline error or clear input")
	}
	if err = tt.Click("主密码"); err != nil {
		t.Fatal(err)
	}
	tt.Type("correcting input")
	if a.unlockError != "" {
		t.Fatal("editing did not clear stale error")
	}
	a.dir = filepath.Join(t.TempDir(), "ssh-client-workbench-preview-qa")
	tt.Frame()
	if !tt.HasText("测试保险库 · 不要录入真实凭据") {
		t.Fatal("test vault marker hidden")
	}
}
