package desktop

import (
	"testing"

	"github.com/egoist/mygo/ui"
	"sshclient/internal/client"
)

func TestEnglishWorkspaceKeepsUserTextAndSharedSidebarState(t *testing.T) {
	// MyGo's supported process override also localizes pure presentation
	// helpers, without opening the app or saving any OS language preference.
	t.Setenv("MYGO_LOCALE", "en")
	a := fileFollowDemo()
	a.dir = t.TempDir()
	a.client = client.Client{Dir: a.dir}
	a.connections[0].Name = "设置" // User data deliberately matches a message key.
	first, second := a.sessions[0], a.sessions[1]
	tt := ui.NewTester(a.view, 1000, 760)
	tt.SetLocale("en")
	for _, label := range []string{"Global navigation", "Settings", "New connection", "Search connections", "设置"} {
		if _, ok := tt.Find(label); !ok {
			t.Fatal("English workspace is missing a localized control or changed user text", label)
		}
	}
	if err := tt.Click("Settings"); err != nil {
		t.Fatal(err)
	}
	settings := ui.NewTester(a.settingsWindowView, 1080, 760)
	settings.SetLocale("en")
	if _, ok := settings.Find("Settings categories"); !ok || !settings.HasText("Language") {
		t.Fatal("English settings window is missing its navigation or language preference")
	}
	if err := settings.Click("Settings category: Access grants"); err != nil {
		t.Fatal(err)
	}
	if a.settingsSection != settingsApprovals || a.page != 1 {
		t.Fatal("English approval category did not retain the workspace")
	}
	if err := settings.Click("Close settings"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if _, ok := tt.Find("Connection sidebar"); !ok {
		t.Fatal("closing settings lost the connection sidebar")
	}
	if a.sessions[0] != first || a.sessions[1] != second || a.connections[0].Name != "设置" {
		t.Fatal("translation or page switching changed connection data")
	}
	a.sidebarHidden = true
	tt.Frame()
	if err := tt.Click("Settings"); err != nil {
		t.Fatal(err)
	}
	settings.Frame()
	if !a.sidebarHidden || a.settingsSection != settingsWorkspace {
		t.Fatal("settings lost the compact workspace state")
	}
	if err := settings.Click("Close settings"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	first.target.Kind = "docker-terminal"
	first.name = "容器 · demo"
	if got := a.toolsWindowTitle(); got != "SSH tools · Container · demo" || first.name != "容器 · demo" {
		t.Fatal("detached window title is untranslated or changed the container source", got)
	}
	// Changing the view language must not change server identities or selection.
	tt.SetLocale("zh-Hans")
	if _, ok := tt.Find("全局导航栏"); !ok || a.connections[0].Name != "设置" || a.selected != "a" {
		t.Fatal("context locale change corrupted navigation or user state")
	}
}
