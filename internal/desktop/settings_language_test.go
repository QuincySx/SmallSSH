package desktop

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/egoist/mygo/ui"
	"sshclient/internal/desktop/components"
)

func TestLanguageChoiceWhitelistAndTransitions(t *testing.T) {
	choice := ""
	var applied []string
	saved := 0
	save := func() error { saved++; return nil }
	apply := func(tag string) { applied = append(applied, tag) }
	for _, invalid := range []string{"fr", "en-US", "EN", "zh", " ", "en-XA", "../../en"} {
		if changed, err := changeLanguageChoice(&choice, invalid, save, apply); changed || err != nil {
			t.Fatalf("invalid choice %q was accepted", invalid)
		}
	}
	if choice != "" || saved != 0 || len(applied) != 0 {
		t.Fatal("invalid input caused a preference write or locale change")
	}
	for _, next := range []string{"en", "", "zh-Hans", ""} {
		if changed, err := changeLanguageChoice(&choice, next, save, apply); !changed || err != nil || choice != next {
			t.Fatalf("mode change to %q failed: %v", next, err)
		}
	}
	if saved != 4 || strings.Join(applied, ",") != "en,,zh-Hans," {
		t.Fatal("selection mode did not save independently of the effective language", saved, applied)
	}
	if changed, err := changeLanguageChoice(&choice, "", save, apply); changed || err != nil || saved != 4 {
		t.Fatal("unchanged selection caused writes")
	}
	failed := errors.New("synthetic write failure")
	if changed, err := changeLanguageChoice(&choice, "en", func() error { return failed }, apply); changed || !errors.Is(err, failed) || choice != "" || len(applied) != 4 {
		t.Fatal("failed save did not retain the previous language")
	}
}

func TestLanguageChoicePersistsWithoutNativePreferenceAccess(t *testing.T) {
	for _, choice := range []string{"", "zh-Hans", "en"} {
		t.Run(choice, func(t *testing.T) {
			dir := t.TempDir()
			if err := os.Chmod(dir, 0700); err != nil {
				t.Fatal(err)
			}
			a := &app{dir: dir, languageChoice: choice, sidebarWidth: 220, terminalShare: 65}
			if err := a.saveLayout(); err != nil {
				t.Fatal(err)
			}
			restored := &app{dir: dir}
			restored.loadLayout()
			if restored.languageChoice != choice || restored.sidebarWidth != 220 || restored.terminalShare != 65 {
				t.Fatalf("preference did not round trip: %#v", restored)
			}
		})
	}
	for _, data := range []string{
		`{"version":1,"right":true,"sidebar":210}`,
		`{"version":2,"localeChoice":"invalid","sidebar":210}`,
	} {
		dir := t.TempDir()
		if err := os.WriteFile(filepath.Join(dir, "layout.json"), []byte(data), 0600); err != nil {
			t.Fatal(err)
		}
		a := &app{dir: dir}
		a.loadLayout()
		if a.languageChoice != "" || a.sidebarWidth != 210 {
			t.Fatal("older or invalid language preference did not follow the system")
		}
	}
}

func TestLanguageSettingsRendersInContextLocaleWithoutWritingOnRender(t *testing.T) {
	a := &app{dir: t.TempDir(), languageChoice: "en"}
	if err := os.Chmod(a.dir, 0700); err != nil {
		t.Fatal(err)
	}
	called := 0
	tt := ui.NewTester(func(c *ui.Context) {
		components.ApplyTheme(c, false)
		ui.Column(c).Fill().Padding(16).Children(func() {
			a.languageSettingsWith(c, func(string) { called++ })
		})
	}, 360, 180)
	if !tt.HasText("界面语言") || !tt.HasText("English") || called != 0 {
		t.Fatal("initial render changed language or lost the selection", tt.Texts())
	}
	tt.SetLocale("en")
	if !tt.HasText("Language") || !tt.HasText("English") || tt.HasText("界面语言") || called != 0 {
		t.Fatal("English context did not translate the language setting", tt.Texts())
	}
	if _, err := os.Stat(filepath.Join(a.dir, "layout.json")); !os.IsNotExist(err) {
		t.Fatal("rendering wrote application preferences", err)
	}
	control, ok := tt.Find("Interface language selection")
	if !ok || control.X < 0 || control.X+control.W > 360 || control.H < 20 {
		t.Fatal("language selector does not fit a narrow content column", control)
	}
	if err := tt.Click("Interface language selection"); err != nil {
		t.Fatal(err)
	}
	if err := tt.Click("Follow system"); err != nil {
		t.Fatal(err)
	}
	if called != 1 || a.languageChoice != "" {
		t.Fatal("user selection did not apply follow-system mode", called, a.languageChoice)
	}
	restored := &app{dir: a.dir, languageChoice: "en"}
	restored.loadLayout()
	if restored.languageChoice != "" {
		t.Fatal("follow-system mode was not persisted")
	}
}
