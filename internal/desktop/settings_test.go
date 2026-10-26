package desktop

import (
	"context"
	"fmt"
	"math"
	"strings"
	"testing"

	"github.com/egoist/mygo/ui"
	"sshclient/internal/desktop/components"
	"sshclient/internal/systemauth"
)

func settingsTester(a *app, width, height int) *ui.Tester {
	return ui.NewTester(func(c *ui.Context) {
		components.ApplyTheme(c, false)
		ui.Column(c).Fill().Background(c.Theme().Background).Children(func() { a.settingsView(c) })
	}, width, height)
}

func TestSettingsDefaultAndUnknownCategoryShowWorkspace(t *testing.T) {
	for _, selected := range []string{"", "removed-category"} {
		t.Run(selected, func(t *testing.T) {
			a := &app{page: 4, settingsSection: selected}
			tt := settingsTester(a, 900, 600)
			if a.settingsSection != settingsWorkspace || !tt.HasText("连接侧栏") || !tt.HasText("恢复默认布局") {
				t.Fatal("settings did not open a usable default category", a.settingsSection, tt.Texts())
			}
			if _, ok := tt.Find("主密码确认设置"); ok {
				t.Fatal("workspace category rendered security controls")
			}
			for _, placeholder := range []string{"AI", "1Password", "终端字体"} {
				if tt.HasText(placeholder) {
					t.Fatal("settings exposes an unimplemented preference", placeholder)
				}
			}
		})
	}
}

func TestSettingsCategoryPointerAndKeyboardClearMasterPassword(t *testing.T) {
	a := &app{page: 4, authStatus: systemauth.Status{Available: true, Name: "Touch ID"}}
	tt := settingsTester(a, 900, 600)
	a.ownerPass = "synthetic category switch password"
	if err := tt.Click("设置分类：安全与解锁"); err != nil {
		t.Fatal(err)
	}
	if a.settingsSection != settingsSecurity || a.ownerPass != "" || !tt.HasText("确认主密码") || !tt.Focused("设置分类：安全与解锁") {
		t.Fatal("pointer selection did not switch category with native focus or clear the password", a.settingsSection)
	}
	cancelled := 0
	a.authCancel = func() { cancelled++ }
	a.ownerPass = "synthetic password to discard"
	tt.Key(0, ui.KeyUp)
	if a.settingsSection != settingsSecurity || a.ownerPass == "" || cancelled != 0 || !tt.Focused("设置分类：工作区") {
		t.Fatal("arrow navigation activated a category before confirmation", a.settingsSection, cancelled)
	}
	tt.Key(0, ui.KeyEnter)
	if a.settingsSection != settingsWorkspace || a.ownerPass != "" || cancelled != 1 || !tt.Focused("设置分类：工作区") {
		t.Fatal("keyboard selection retained the password or in-flight authentication", a.settingsSection, cancelled)
	}
	tt.Key(0, ui.KeyDown)
	tt.Key(0, ui.KeySpace)
	if a.settingsSection != settingsSecurity || !tt.HasText("启用 Touch ID") {
		t.Fatal("category keyboard navigation did not select security")
	}
	tt.Key(0, ui.KeyHome)
	tt.Key(0, ui.KeyEnter)
	tt.Key(0, ui.KeyTab)
	if a.settingsSection != settingsWorkspace || !tt.Focused("界面语言选择") {
		t.Fatal("category navigation is not one Tab stop before its preferences")
	}
	if a.page != 4 || a.contentFocus {
		t.Fatal("category selection navigated away or requested terminal focus")
	}
}

func TestSettingsWorkspaceControlsChangeActualLayoutWithoutLeavingSettings(t *testing.T) {
	a := &app{page: 4}
	tt := settingsTester(a, 900, 600)
	for _, action := range []struct {
		label string
		check func() bool
	}{
		{"工作区侧栏设置", func() bool { return a.sidebarHidden }},
		{"工作区侧栏设置", func() bool { return !a.sidebarHidden }},
		{"工作区工具设置", func() bool { return a.toolsHidden }},
		{"工作区工具设置", func() bool { return !a.toolsHidden }},
		{"右侧", func() bool { return a.toolsRight && !a.toolsHidden }},
		{"底部", func() bool { return !a.toolsRight && !a.toolsHidden }},
	} {
		if err := tt.Click(action.label); err != nil {
			t.Fatal(err)
		}
		if !action.check() || a.page != 4 || a.contentFocus {
			t.Fatal("settings control failed to update its real preference or stole terminal focus", action.label)
		}
		navigation := "设置分类"
		if a.sidebarHidden {
			navigation = "设置分类窄栏"
		}
		if _, found := tt.Find(navigation); !found {
			t.Fatal("settings sidebar did not follow its shared layout preference", action.label, navigation)
		}
	}
	for _, label := range []string{"连接侧栏", "工具面板", "终端区域"} {
		if _, ok := tt.Find(label); ok {
			// The first two also have parameter text, so only a large rectangle
			// would indicate that a workbench pane was accidentally rendered.
			r, _ := tt.Find(label)
			if label == "终端区域" || r.H > 60 {
				t.Fatal("a workspace preference rendered its panel inside settings", label, r)
			}
		}
	}
}

func TestSettingsResetKeepsSettingsAndClosesDetachedTools(t *testing.T) {
	window := &fakeToolsWindow{onClosed: func() {}}
	a := &app{
		ctx: context.Background(), online: true, page: 4,
		sidebarHidden: true, toolsRight: true, toolsHidden: true,
		toolsDetached: true, toolsWin: window, fileExpanded: true,
	}
	tt := settingsTester(a, 900, 600)
	if err := tt.Click("恢复默认布局"); err != nil {
		t.Fatal(err)
	}
	window.finishPendingUpdates()
	if a.page != 4 || a.settingsSection != settingsWorkspace || a.contentFocus || a.sidebarHidden || a.toolsHidden || a.toolsRight || a.fileExpanded || a.toolsDetached || a.toolsWin != nil {
		t.Fatal("reset did not restore the workbench layout while retaining settings")
	}
	if window.hidden != 1 || window.closed != 1 || window.shown != 0 {
		t.Fatal("explicit layout reset did not close detached tools without opening a window", window)
	}
	if _, found := tt.Find("设置分类"); !found {
		t.Fatal("layout reset did not expand the settings sidebar")
	}
}

func TestSettingsControlsFitNarrowWindowAndContentScrolls(t *testing.T) {
	a := &app{page: 4, authStatus: systemauth.Status{Available: true, Name: "Touch ID"}}
	tt := settingsTester(a, 560, 760)
	for _, control := range []struct {
		label      string
		minW, minH float32
	}{
		{"工作区侧栏设置", 36, 20},
		{"工作区工具设置", 36, 20},
		{"工作区工具停靠位置", 90, 30},
		{"恢复默认布局", 90, 32},
	} {
		r, ok := tt.Find(control.label)
		if !ok || r.X < 152 || r.X+r.W > 560 || r.W < control.minW || r.H < control.minH {
			t.Fatal("preference control is clipped at the minimum width", control.label, r)
		}
	}
	tt.SetSize(560, 240)
	viewport, ok := tt.Find("设置内容")
	if !ok || viewport.W < 300 || viewport.H != 240 {
		t.Fatal("narrow settings lost its independent content viewport", viewport)
	}
	before, foundBefore := tt.Find("恢复默认布局")
	tt.Scroll(viewport.X+viewport.W/2, viewport.Y+viewport.H/2, 0, 10000)
	after, foundAfter := tt.Find("恢复默认布局")
	if !foundAfter || after.H < 32 || after.Y < viewport.Y || after.Y+after.H > viewport.Y+viewport.H || foundBefore && before == after {
		t.Fatal("short settings content cannot scroll to the reset control", before, after)
	}
	if _, ok := tt.Find("设置分类：安全与解锁"); !ok {
		t.Fatal("content scrolling moved the category navigation away")
	}
	tt.SetSize(1920, 760)
	container, ok := tt.Find("设置内容容器")
	viewport, _ = tt.Find("设置内容")
	if !ok || container.W > 721 || container.W < 719 || math.Abs(float64(container.X+container.W/2-viewport.X-viewport.W/2)) > 1 {
		t.Fatal("wide settings did not center a readable content column", container, viewport)
	}
	if err := tt.Click("设置分类：安全与解锁"); err != nil {
		t.Fatal(err)
	}
	tt.SetSize(560, 760)
	for _, label := range []string{"主密码确认设置", "重新检测", "启用 Touch ID"} {
		r, ok := tt.Find(label)
		if !ok || r.X < 152 || r.X+r.W > 560 || r.H < 36 {
			t.Fatal("security control clipped at the minimum width", label, r)
		}
	}
}

func TestSettingsSecurityRetainsAvailabilityCancellationAndFeedback(t *testing.T) {
	for _, state := range []struct {
		available, enabled bool
		expect             string
	}{
		{true, false, "启用 Touch ID"},
		{true, true, "停用 Touch ID"},
		{false, true, "停用 Touch ID"},
		{false, false, "不可用"},
	} {
		t.Run(fmt.Sprint(state.available, state.enabled), func(t *testing.T) {
			cancelled := false
			a := &app{
				page: 4, settingsSection: settingsSecurity,
				authStatus: systemauth.Status{Available: state.available, Enabled: state.enabled, Name: "Touch ID", Reason: "测试设备尚未配置系统验证"},
				status:     "系统验证失败，请重试", authCancel: func() { cancelled = true },
			}
			tt := settingsTester(a, 560, 760)
			for _, text := range []string{state.expect, "重新检测", "取消验证", "系统验证失败，请重试"} {
				if !tt.HasText(text) {
					t.Fatal("security settings lost authentication state or error feedback", text, tt.Texts())
				}
			}
			_, hasPassword := tt.Find("主密码确认设置")
			if hasPassword != (state.available || state.enabled) {
				t.Fatal("security settings changed the master-password requirement")
			}
			if !state.available && !tt.HasText(a.authStatus.Reason) {
				t.Fatal("security settings hid the unavailable reason")
			}
			if err := tt.Click("取消验证"); err != nil {
				t.Fatal(err)
			}
			if !cancelled || a.page != 4 {
				t.Fatal("cancel action did not reach the existing authentication flow")
			}
			if hasPassword {
				if err := tt.Click("主密码确认设置"); err != nil {
					t.Fatal(err)
				}
				tt.Type("synthetic password only")
				if a.ownerPass == "" || tt.HasText(a.ownerPass) {
					t.Fatal("security settings did not retain a masked master-password field")
				}
			}
		})
	}
	a := &app{page: 4, settingsSection: settingsSecurity, status: strings.Repeat("测试错误详情。", 80)}
	tt := settingsTester(a, 560, 240)
	viewport, _ := tt.Find("设置内容")
	tt.Scroll(viewport.X+viewport.W/2, viewport.Y+viewport.H/2, 0, 10000)
	feedback, ok := tt.Find("安全设置反馈")
	if !ok || feedback.Y+feedback.H > viewport.Y+viewport.H || feedback.X+feedback.W > 560 {
		t.Fatal("long security feedback cannot scroll into view", feedback, viewport)
	}
}

func TestSettingsSidebarCollapsePreservesSecuritySelectionPasswordAndFocus(t *testing.T) {
	cancelled := 0
	a := &app{
		page: 4, settingsSection: settingsSecurity,
		authStatus: systemauth.Status{Available: true, Name: "Touch ID"},
		authCancel: func() { cancelled++ },
	}
	tt := settingsTester(a, 1000, 600)
	if err := tt.Click("主密码确认设置"); err != nil {
		t.Fatal(err)
	}
	categoryBounds := map[string]ui.Rect{}
	iconBounds := map[string]ui.Rect{}
	for _, category := range settingsCategories {
		label := "设置分类：" + category.title
		categoryBounds[label], _ = tt.Find(label)
		iconBounds[label], _ = tt.Find(category.title + "图标")
	}

	password := "synthetic sidebar toggle password"
	tt.Type(password)
	for _, hidden := range []bool{true, false, true, false} {
		a.sidebarHidden = hidden
		tt.Frame()
		if a.settingsSection != settingsSecurity || a.ownerPass != password || cancelled != 0 || !tt.Focused("主密码确认设置") || a.contentFocus {
			t.Fatal("sidebar toggle changed the selected category, password, authentication or input focus", hidden, a.settingsSection, cancelled)
		}
		for _, category := range settingsCategories {
			label := "设置分类：" + category.title
			item, ok := tt.Find(label)
			icon, iconOK := tt.Find(category.title + "图标")
			if !ok || !iconOK || item.H != components.NavigationItemHeight || item.Y != categoryBounds[label].Y || icon != iconBounds[label] {
				t.Fatal("settings sidebar width change moved a category or replaced its icon geometry", hidden, label, item, icon)
			}
		}
		_, categoryMounted := tt.Find("设置分类")
		viewport, ok := tt.Find("设置内容")
		expectedX := float32(152)
		if hidden {
			expectedX = components.RailWidth
			rail, ok := tt.Find("设置分类窄栏")
			if !ok || rail.W != components.RailWidth || rail.H != 600 {
				t.Fatal("collapsed settings lost its category rail", rail)
			}
		}
		if categoryMounted == hidden || !ok || viewport.X != expectedX || viewport.W != 1000-expectedX || viewport.Y != 0 || viewport.H != 600 {
			t.Fatal("sidebar toggle did not preserve the content viewport beside its category navigation", hidden, categoryMounted, viewport)
		}
		tt.Type("!")
		password += "!"
		if a.ownerPass != password || tt.HasText(password) {
			t.Fatal("password input lost its edit state or masking after sidebar toggle", hidden)
		}
	}
	if err := tt.Click("设置分类：工作区"); err != nil {
		t.Fatal(err)
	}
	if a.settingsSection != settingsWorkspace || a.ownerPass != "" || cancelled != 1 {
		t.Fatal("restored category navigation did not clear the password and cancel verification", a.settingsSection, cancelled)
	}
	tt.Key(0, ui.KeyDown)
	if a.settingsSection != settingsWorkspace || !tt.Focused("设置分类：安全与解锁") {
		t.Fatal("restored category arrow navigation did not move focus", a.settingsSection)
	}
	tt.Key(0, ui.KeyEnter)
	if a.settingsSection != settingsSecurity || a.ownerPass != "" || !tt.Focused("设置分类：安全与解锁") {
		t.Fatal("restored category navigation lost keyboard behavior", a.settingsSection)
	}
}

func TestSettingsSidebarCollapsePreservesContentScroll(t *testing.T) {
	a := &app{page: 4}
	tt := settingsTester(a, 1000, 180)
	viewport, _ := tt.Find("设置内容")
	tt.Scroll(viewport.X+viewport.W/2, viewport.Y+viewport.H/2, 0, 10000)
	before, ok := tt.Find("恢复默认布局")
	if !ok || before.Y < viewport.Y || before.Y+before.H > viewport.Y+viewport.H {
		t.Fatal("cannot reach the reset control before toggling the sidebar", before, viewport)
	}
	for _, hidden := range []bool{true, false} {
		a.sidebarHidden = hidden
		tt.Frame()
		after, ok := tt.Find("恢复默认布局")
		currentViewport, _ := tt.Find("设置内容")
		if !ok || math.Abs(float64(after.Y-currentViewport.Y-before.Y+viewport.Y)) > 1 || after.Y+after.H > currentViewport.Y+currentViewport.H {
			t.Fatal("sidebar toggle reset the category's content scroll position", hidden, before, after, currentViewport)
		}
	}
}

func TestSettingsCollapsedControlsFitNarrowWindowAndScroll(t *testing.T) {
	for _, category := range []struct {
		id       string
		controls []string
		last     string
	}{
		{settingsWorkspace, []string{"工作区侧栏设置", "工作区工具设置", "工作区工具停靠位置", "恢复默认布局"}, "恢复默认布局"},
		{settingsSecurity, []string{"主密码确认设置", "重新检测", "启用 Touch ID"}, "安全设置反馈"},
	} {
		t.Run(category.id, func(t *testing.T) {
			a := &app{
				page: 4, settingsSection: category.id, sidebarHidden: true,
				authStatus: systemauth.Status{Available: true, Name: "Touch ID"},
				status:     "合成的安全设置反馈",
			}
			tt := settingsTester(a, 560, 760)
			if _, ok := tt.Find("设置分类"); ok {
				t.Fatal("collapsed settings kept the expanded category sidebar mounted")
			}
			viewport, ok := tt.Find("设置内容")
			if !ok || viewport.X != components.RailWidth || viewport.W != 560-components.RailWidth || viewport.Y != 0 || viewport.H != 760 {
				t.Fatal("collapsed settings did not fill the page beside its 48px rail", viewport)
			}
			rail, ok := tt.Find("设置分类窄栏")
			if !ok || rail.W != components.RailWidth || rail.H != 760 {
				t.Fatal("collapsed settings lost its full-height category rail", rail)
			}
			for _, label := range []string{"设置分类：工作区", "设置分类：安全与解锁"} {
				item, ok := tt.Find(label)
				if !ok || item.W != 36 || item.H != components.NavigationItemHeight || item.X < 0 || item.X+item.W > components.RailWidth {
					t.Fatal("collapsed settings lost its clickable category", label, item)
				}
			}
			for _, label := range category.controls {
				r, ok := tt.Find(label)
				if !ok || r.X < components.RailWidth || r.X+r.W > 560 || r.W < 32 || r.H < 20 {
					t.Fatal("collapsed settings clipped a preference at the minimum width", label, r)
				}
			}
			tt.SetSize(560, 180)
			viewport, _ = tt.Find("设置内容")
			tt.Scroll(viewport.X+viewport.W/2, viewport.Y+viewport.H/2, 0, 10000)
			last, ok := tt.Find(category.last)
			if !ok || last.Y < viewport.Y || last.Y+last.H > viewport.Y+viewport.H || last.X+last.W > 560 {
				t.Fatal("collapsed settings cannot scroll to its final control or feedback", category.last, last, viewport)
			}
		})
	}
}

func TestSettingsCollapsedCategoryPointerAndKeyboardClearPasswordAndCancelAuthentication(t *testing.T) {
	cancelled := 0
	a := &app{
		page: 4, sidebarHidden: true,
		authStatus: systemauth.Status{Available: true, Name: "Touch ID"},
	}
	tt := settingsTester(a, 560, 760)
	a.ownerPass = "synthetic password before category switch"
	if err := tt.Click("设置分类：安全与解锁"); err != nil {
		t.Fatal(err)
	}
	if a.settingsSection != settingsSecurity || a.ownerPass != "" || !tt.HasText("确认主密码") || !tt.Focused("设置分类：安全与解锁") {
		t.Fatal("collapsed category click did not select security and clear password", a.settingsSection)
	}
	a.authCancel = func() { cancelled++ }
	a.ownerPass = "synthetic in-flight authentication password"
	tt.Key(0, ui.KeyUp)
	if a.settingsSection != settingsSecurity || !tt.Focused("设置分类：工作区") {
		t.Fatal("collapsed arrow navigation did not move focus to workspace")
	}
	tt.Key(0, ui.KeyEnter)
	if a.settingsSection != settingsWorkspace || a.ownerPass != "" || cancelled != 1 {
		t.Fatal("collapsed keyboard category activation retained password or authentication", a.settingsSection, cancelled)
	}
	tt.Key(0, ui.KeyDown)
	tt.Key(0, ui.KeySpace)
	if a.settingsSection != settingsSecurity || !tt.HasText("启用 Touch ID") {
		t.Fatal("collapsed category lost keyboard access to security")
	}
	tt.Key(0, ui.KeyTab)
	if !tt.Focused("主密码确认设置") || a.page != 4 || a.contentFocus {
		t.Fatal("collapsed categories did not retain a single Tab stop before settings controls")
	}
}
