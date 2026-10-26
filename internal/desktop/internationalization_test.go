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
	for _, label := range []string{"Workbench", "Access grants", "Settings", "New connection", "Search connections", "设置"} {
		if _, ok := tt.Find(label); !ok {
			t.Fatal("English workspace is missing a localized control or changed user text", label)
		}
	}
	for _, page := range []struct{ tab, region string }{
		{"Settings", "Settings categories"},
		{"Access grants", "Grant categories"},
		{"Workbench", "Connection sidebar"},
	} {
		if err := tt.Click(page.tab); err != nil {
			t.Fatal(err)
		}
		if _, ok := tt.Find(page.region); !ok {
			t.Fatal("English page navigation failed", page)
		}
		if page.tab == "Settings" {
			if !tt.HasText("Language") {
				t.Fatal("language preference is untranslated")
			}
		}
	}
	if a.sessions[0] != first || a.sessions[1] != second || a.connections[0].Name != "设置" {
		t.Fatal("translation or page switching changed connection data")
	}
	a.sidebarHidden = true
	tt.Frame()
	for _, tab := range []string{"Settings", "Access grants", "Workbench"} {
		if err := tt.Click(tab); err != nil {
			t.Fatal(err)
		}
		if !a.sidebarHidden {
			t.Fatal("language navigation lost the shared compact state", tab)
		}
	}
	first.target.Kind = "docker-terminal"
	first.name = "容器 · demo"
	if got := a.toolsWindowTitle(); got != "SSH tools · Container · demo" || first.name != "容器 · demo" {
		t.Fatal("detached window title is untranslated or changed the container source", got)
	}
	// Changing the view language must not change server identities or selection.
	tt.SetLocale("zh-Hans")
	if _, ok := tt.Find("工作台"); !ok || a.connections[0].Name != "设置" || a.selected != "a" {
		t.Fatal("context locale change corrupted navigation or user state")
	}
}
