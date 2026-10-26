package desktop

import (
	"testing"

	"github.com/egoist/mygo/ui"
	"sshclient/internal/approval"
	"sshclient/internal/auditlog"
	"sshclient/internal/desktop/components"
)

func TestApprovalCategoryNavigationRemainsAvailableWithoutRequests(t *testing.T) {
	for _, collapsed := range []bool{false, true} {
		for _, dark := range []bool{false, true} {
			a := &app{sidebarHidden: collapsed}
			a.approvalHistory.loaded = true
			tt := ui.NewTester(a.approvalView, 560, 760)
			tt.SetDark(dark)
			region, width := "授权分类", approvalCategoryWidth
			if collapsed {
				region, width = "授权分类窄栏", components.RailWidth
			}
			navigation, ok := tt.Find(region)
			if !ok || navigation.X != 0 || navigation.W != width {
				t.Fatal("category navigation did not stay at the page edge", collapsed, navigation)
			}
			for index, category := range approvalCategories {
				label := "授权分类：" + category.title
				control, ok := tt.Find(label)
				if !ok || control.X < navigation.X || control.X+control.W > navigation.X+navigation.W || control.Y < 0 || control.Y+control.H > 760 {
					t.Fatal("category is inaccessible in an empty center", collapsed, label, control)
				}
				if collapsed && (control.W != 36 || control.H != components.NavigationItemHeight) {
					t.Fatal("collapsed category is not a compact icon control", control)
				}
				if err := tt.Click(label); err != nil {
					t.Fatal(err)
				}
				empty := "暂无请求"
				if index == 2 {
					empty = "暂无访问历史"
				}
				if a.approvalTab != index || !tt.HasText(empty) || a.busy || a.authSequence != 0 {
					t.Fatal("empty category selection changed a decision", index, a.approvalTab)
				}
			}
			if _, ok := tt.Find("授权中心"); ok {
				t.Fatal("the center added a duplicate page heading")
			}
			if collapsed {
				if _, ok := tt.Find("待处理"); ok {
					t.Fatal("collapsed navigation still displays category text")
				}
			}
		}
	}
}

func TestApprovalCategorySelectionClearsAuthenticationWithoutDeciding(t *testing.T) {
	for _, collapsed := range []bool{false, true} {
		a, pending := reviewFixture("full")
		active, history := pending, pending
		active.ID, active.Status, active.Operation.Endpoint = "active-category-fixture", "approved", "deploy@192.0.2.20:22"
		history.ID, history.Status, history.Operation.Endpoint = "history-category-fixture", "denied", "deploy@192.0.2.30:22"
		a.requests = []approval.Request{pending, active, history}
		a.approvalHistory = approvalHistoryState{loaded: true, events: []auditlog.Event{{HostName: history.Operation.Endpoint, Event: "denied", Kind: "grant"}}}
		a.sidebarHidden = collapsed
		tt := ui.NewTester(a.approvalView, 1000, 760)
		for _, choice := range []struct {
			index int
			item  approval.Request
		}{{1, active}, {2, history}, {0, pending}} {
			a.ownerPass, a.approvalPasswordMode = "synthetic-prior-confirmation", true
			if err := tt.Click("授权分类：" + approvalCategories[choice.index].title); err != nil {
				t.Fatal(err)
			}
			selected := choice.item.ID
			if choice.index == 2 {
				selected = ""
			}
			if a.approvalTab != choice.index || a.selectedRequest != selected || a.ownerPass != "" || a.approvalPasswordMode || !tt.HasText(choice.item.Operation.Endpoint) {
				t.Fatal("category retained authentication or selected another request", collapsed, choice.index, a.selectedRequest)
			}
			if a.busy || a.authSequence != 0 || a.status != "" || a.requests[0].Status != "pending" || a.requests[1].Status != "approved" || a.requests[2].Status != "denied" {
				t.Fatal("category navigation initiated an approval or revocation")
			}
		}
		if err := tt.Click("授权分类：活动"); err != nil {
			t.Fatal(err)
		}
		// Both widths use the same native focus group: arrows move the
		// focus, while Enter or Space explicitly activates a category.
		a.selectApprovalCategory(2)
		tt.Frame()
		a.ownerPass = "synthetic-keyboard-confirmation"
		tt.Key(0, ui.KeyEnter)
		if a.approvalTab != 1 || a.selectedRequest != active.ID || a.ownerPass != "" {
			t.Fatal("keyboard activation did not select its focused category", collapsed)
		}
		a.ownerPass = "synthetic-arrow-confirmation"
		tt.Key(0, ui.KeyDown)
		if a.approvalTab != 1 || a.ownerPass == "" || !tt.Focused("授权分类：历史") {
			t.Fatal("category arrow navigation did not move focus independently", collapsed)
		}
		tt.Key(0, ui.KeySpace)
		if a.approvalTab != 2 || a.selectedRequest != "" || a.ownerPass != "" {
			t.Fatal("category Space activation retained authentication", collapsed)
		}

		if a.busy || a.authSequence != 0 || a.status != "" {
			t.Fatal("keyboard category selection initiated a decision")
		}
	}
}

func TestApprovalCategoryCollapseKeepsRequestSelectionAndPasswordFocus(t *testing.T) {
	for _, width := range []int{560, 1000} {
		a, first := reviewFixture("read")
		second := first
		second.ID, second.Operation.Endpoint = "collapse-target-fixture", "deploy@192.0.2.20:2222"
		a.requests = []approval.Request{first, second}
		a.selectedRequest = second.ID
		tt := ui.NewTester(a.approvalView, width, 760)
		if err := tt.Click("保险库主密码"); err != nil {
			t.Fatal(err)
		}
		tt.Type("synthetic-focus-confirmation")
		categoryBounds := map[string]ui.Rect{}
		iconBounds := map[string]ui.Rect{}
		for _, category := range approvalCategories {
			label := "授权分类：" + category.title
			categoryBounds[label], _ = tt.Find(label)
			iconBounds[label], _ = tt.Find(category.title + "图标")
		}
		for _, collapsed := range []bool{true, false} {
			a.sidebarHidden = collapsed
			tt.Frame()
			if a.selectedRequest != second.ID || a.ownerPass != "synthetic-focus-confirmation" || !tt.Focused("保险库主密码") || !tt.HasText(second.Operation.Endpoint) {
				t.Fatal("category collapse replaced the active review or authentication input", width, collapsed)
			}
			for _, category := range approvalCategories {
				label := "授权分类：" + category.title
				item, ok := tt.Find(label)
				icon, iconOK := tt.Find(category.title + "图标")
				if !ok || !iconOK || item.H != components.NavigationItemHeight || item.Y != categoryBounds[label].Y || icon != iconBounds[label] {
					t.Fatal("approval sidebar width change moved a category or its icon", width, collapsed, label, item, icon)
				}
			}
			selection := "授权请求列表"
			if width == 560 {
				selection = "选择授权请求"
			}
			if _, ok := tt.Find(selection); !ok {
				t.Fatal("category collapse hid request navigation", width, collapsed)
			}
			for _, label := range []string{"保险库主密码", "不允许", "允许"} {
				bounds, ok := tt.Find(label)
				if !ok || bounds.X < 0 || bounds.X+bounds.W > float32(width) || bounds.Y < 0 || bounds.Y+bounds.H > 760 {
					t.Fatal("category navigation clipped an authentication action", width, collapsed, label, bounds)
				}
			}
		}
	}
}

func TestApprovalLayoutUsesContentWidthAfterItsOwnNavigation(t *testing.T) {
	for _, check := range []struct {
		width     float32
		collapsed bool
		compact   bool
	}{{939, false, true}, {940, false, false}, {835, true, true}, {836, true, false}, {900, false, true}, {900, true, false}} {
		if got := approvalContentIsCompact(check.width, check.collapsed); got != check.compact {
			t.Fatal("request navigation ignored available content width", check, got)
		}
	}
}
