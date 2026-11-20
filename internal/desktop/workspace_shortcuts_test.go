package desktop

import (
	"context"
	"fmt"
	"slices"
	"strings"
	"testing"

	"github.com/egoist/mygo/ui"
	"sshclient/internal/desktop/components"
)

func TestWorkspaceKeysFollowEachPlatformWithoutBareCtrlLetters(t *testing.T) {
	for _, goos := range []string{"darwin", "windows", "linux"} {
		newTab, next, previous, settings, sidebar := workspaceKeysFor(goos)
		if goos == "darwin" {
			if newTab != "Cmd+T" || settings != "Cmd+," || next == "" || previous == "" || sidebar == "" {
				t.Fatal("macOS lost its standard chords", newTab, next, previous, settings, sidebar)
			}
			continue
		}
		if newTab != "Ctrl+Shift+T" || settings != "" || sidebar != "" {
			t.Fatal("Windows/Linux chord changed", goos, newTab, settings, sidebar)
		}
		for _, key := range []string{newTab, next, previous} {
			if strings.HasPrefix(key, "Ctrl+") && !strings.Contains(key, "Shift+") && !strings.HasSuffix(key, "+Tab") {
				t.Fatal("a bare Ctrl chord would be taken from the shell", goos, key)
			}
		}
	}
}

func TestMenuShortcutsAreUniqueAndSettingsUsesTheAppMenu(t *testing.T) {
	a := &app{}
	seen := map[string]string{}
	var settings *menuAction
	for _, group := range a.menuGroups() {
		for i, action := range group.actions {
			if action.shortcut != "" {
				if other, ok := seen[action.shortcut]; ok {
					t.Fatal("two menu items share a shortcut", action.shortcut, other, action.label)
				}
				seen[action.shortcut] = action.label
			}
			if action.label == "设置…" {
				settings = &group.actions[i]
			}
		}
	}
	newTab, next, previous, settingsKey, _ := workspaceKeys()
	for key, want := range map[string]string{newTab: "新建终端标签", next: "下一个标签", previous: "上一个标签"} {
		if seen[key] != want {
			t.Fatal("menu shortcut missing", key, want, seen[key])
		}
	}
	if settings == nil || !settings.appMenu || settings.shortcut != settingsKey {
		t.Fatal("Settings is not the application-menu item with the platform chord", settings)
	}
}

func TestCycleWorkspaceTabWrapsAndOnlyActivates(t *testing.T) {
	for _, canvas := range []bool{false, true} {
		a := workspaceModelFixture(3)
		a.openWorkspaceTool(toolCommands)
		if canvas {
			a.setWorkspaceCanvas(true)
		}
		var order []workspaceTab
		for _, group := range a.terminalGroups() {
			order = append(order, a.workspaceTabs(group)...)
		}
		groups := len(a.terminalGroups())
		if len(order) != 4 {
			t.Fatal("fixture should show three terminals and one tool tab", canvas, len(order))
		}
		a.activateWorkspaceTab(a.workspaceTabGroup(order[0]), order[0])
		for step, want := range []int{1, 2, 3, 0} {
			a.cycleWorkspaceTab(1)
			if got := a.selectedWorkspaceTab(a.activeTerminalGroup); got != order[want] {
				t.Fatal("next tab did not follow the strip order", canvas, step, want)
			}
		}
		a.cycleWorkspaceTab(-1)
		if got := a.selectedWorkspaceTab(a.activeTerminalGroup); got != order[3] {
			t.Fatal("previous tab did not wrap to the end", canvas)
		}
		var after []workspaceTab
		for _, group := range a.terminalGroups() {
			after = append(after, a.workspaceTabs(group)...)
		}
		if len(a.terminalGroups()) != groups || !slices.Equal(after, order) || len(a.sessions) != 3 {
			t.Fatal("cycling tabs changed the layout or tab order", canvas)
		}
		a.prompt.open = true
		a.cycleWorkspaceTab(1)
		if got := a.selectedWorkspaceTab(a.activeTerminalGroup); got != order[3] {
			t.Fatal("a modal prompt did not keep the keyboard", canvas)
		}
	}
	single := workspaceModelFixture(1)
	before := single.selectedWorkspaceTab(single.activeTerminalGroup)
	single.cycleWorkspaceTab(1)
	if single.selectedWorkspaceTab(single.activeTerminalGroup) != before {
		t.Fatal("a single tab should stay selected")
	}
}

func TestWorkspaceTabWidthSharesNarrowStrips(t *testing.T) {
	for _, test := range []struct {
		available float32
		count     int
		want      float32
	}{
		{500, 1, 210}, {100, 1, 100}, {5, 1, 20}, // one tab keeps the previous rule
		{1200, 4, 210},                       // wide strips are unchanged
		{600, 4, 600/4 - workspaceTabChrome}, // shared fairly
		{300, 4, workspaceTabMinWidth},       // never below a readable width
		{50, 3, 50},                          // nor wider than the strip
	} {
		if got := workspaceTabWidth(test.available, test.count); got != test.want {
			t.Fatal("tab width", test.available, test.count, got, test.want)
		}
	}
}

func TestNarrowTopStripShowsMoreThanOneLongTab(t *testing.T) {
	a := &app{ctx: context.Background(), online: true, page: 1, toolsHidden: true, sidebarHidden: true}
	for i := 0; i < 4; i++ {
		a.sessions = append(a.sessions, &session{name: fmt.Sprintf("production-database-%d-primary", i), host: fmt.Sprint(i)})
	}
	a.ensureTerminalGroups()
	a.setWorkspaceCanvas(true)
	a.activateTerminal(a.sessions[0], true)
	tt := ui.NewTester(a.view, 640, 520)
	tt.SetTitleBar(ui.TitleBar{Height: 32, Left: 78, Right: 0})
	tt.Frame()
	strip, ok := tt.Find("顶部工作区选项卡")
	if !ok {
		t.Fatal("missing top tab strip")
	}
	visible := 0
	for _, s := range a.sessions {
		r, found := tt.Find("顶部标签：" + s.name)
		if found && r.W > 0 && r.X >= strip.X && r.X+r.W <= strip.X+strip.W+.5 {
			visible++
		}
	}
	if visible < 2 {
		t.Fatal("one long tab still fills the narrow strip", strip, visible)
	}
}

func TestEmptyCommandsOfferTheirNextStep(t *testing.T) {
	a := &app{snippetSelected: -1}
	tt := ui.NewTester(a.commandsView, 760, 360)
	if !tt.HasText("暂无常用命令") {
		t.Fatal("empty state missing", tt.Texts())
	}
	if err := tt.Click("新建命令"); err != nil || !a.snippetOpen {
		t.Fatal("empty state did not start a new command", err)
	}
	b := &app{snippets: commandFixture(), snippetSelected: -1, snippetSearch: "no-such-command"}
	tt = ui.NewTester(b.commandsView, 760, 360)
	if !tt.HasText("没有符合条件的命令") {
		t.Fatal("filtered empty state missing", tt.Texts())
	}
	if err := tt.Click("清除搜索"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if b.snippetSearch != "" || !tt.HasText("部署前端") {
		t.Fatal("clearing the search did not restore the list", b.snippetSearch, tt.Texts())
	}
}

func TestFileModifiedColumnFollowsTheListWidth(t *testing.T) {
	a := fileFollowDemo()
	// A plain bottom dock: before, it always hid the column whatever its width.
	tt := ui.NewTester(a.fileArea, 1200, 420)
	tt.Frame()
	if !tt.HasText("修改时间") {
		t.Fatal("a wide docked list hides the modified column", tt.Texts())
	}
	tt.SetSize(440, 420)
	tt.Frame()
	if tt.HasText("修改时间") {
		t.Fatal("a narrow list squeezes the name beside the modified column")
	}
	if b := a.primaryFilePane.browser(); b.tableWidth <= 0 || b.tableWidth >= 380 {
		t.Fatal("list width was not observed after layout", b.tableWidth)
	}
}

func TestSettingsSegmentedControlMarksTheChoice(t *testing.T) {
	selected := 1
	tt := ui.NewTester(func(c *ui.Context) {
		ui.Row(c).Children(func() { components.Segmented(c, &selected, "底部", "右侧") })
	}, 320, 80)
	if err := tt.Click("底部"); err != nil || selected != 0 {
		t.Fatal("segment did not choose", err, selected)
	}
	image := tt.Image()
	pick := func(label string) [4]uint8 {
		r, ok := tt.Find(label)
		if !ok {
			t.Fatal("segment missing", label)
		}
		// Sample the segment padding just left of the label, not a glyph.
		p := image.RGBAAt(int(r.X-6), int(r.Y+r.H/2))
		return [4]uint8{p.R, p.G, p.B, p.A}
	}
	if chosen, other := pick("底部"), pick("右侧"); chosen == other {
		t.Fatal("chosen segment looks like the others", chosen, other)
	}
}
