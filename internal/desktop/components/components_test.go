package components

import (
	"cmp"
	"fmt"
	"slices"
	"strings"
	"testing"

	"github.com/egoist/mygo/ui"
)

func TestButtonStatesKeyboardAndThemeIsolation(t *testing.T) {
	for _, dark := range []bool{false, true} {
		t.Run(fmt.Sprint(dark), func(t *testing.T) {
			clicks := 0
			tt := ui.NewTester(func(c *ui.Context) {
				original := c.Theme()
				ui.Column(c).Padding(12).Gap(8).Children(func() {
					if Button(c, "删除", ButtonOptions{Variant: Destructive}).Clicked() {
						clicks++
					}
					if Button(c, "不可用", ButtonOptions{Disabled: true}).Clicked() {
						t.Fatal("disabled action invoked")
					}
					if Button(c, "处理中", ButtonOptions{Variant: Primary, Busy: true}).Clicked() {
						t.Fatal("busy action invoked")
					}
					IconButton(c, "↻", "刷新")
				})
				if c.Theme() != original {
					t.Fatal("button leaked its theme into siblings")
				}
			}, 460, 260)
			tt.SetDark(dark)
			if err := tt.Click("删除"); err != nil {
				t.Fatal(err)
			}
			tt.Key(0, ui.KeyEnter)
			if clicks != 2 {
				t.Fatal("native click/keyboard activation lost", clicks)
			}
			for _, label := range []string{"不可用", "处理中"} {
				if err := tt.Click(label); err != nil {
					t.Fatal(err)
				}
				tt.Key(0, ui.KeyEnter)
			}
			if clicks != 2 {
				t.Fatal("disabled action stole activation", clicks)
			}
			if _, ok := tt.Find("刷新"); !ok {
				t.Fatal("icon missing accessible action name")
			}
		})
	}
}

func TestButtonGroupArrowKeysSkipDisabledActions(t *testing.T) {
	chosen := ""
	tt := ui.NewTester(func(c *ui.Context) {
		ButtonGroup(c, func() {
			for _, label := range []string{"第一项", "不可用", "最后一项"} {
				if Button(c, label, ButtonOptions{Disabled: label == "不可用"}).Clicked() {
					chosen = label
				}
			}
		}).Label("操作组")
	}, 500, 100)
	if err := tt.Click("第一项"); err != nil {
		t.Fatal(err)
	}
	tt.Key(0, ui.KeyRight)
	tt.Key(0, ui.KeyEnter)
	if chosen != "最后一项" {
		t.Fatal("group did not use native arrow-key navigation", chosen)
	}
}

func TestPanelBarPreservesNativeTabKeysAndSparseIDs(t *testing.T) {
	selected, closed := 3, false
	tt := ui.NewTester(func(c *ui.Context) {
		PanelBar(c, &selected, []PanelTab{{0, "文件"}, {2, "命令"}, {3, "服务器状态"}}, func() {
			if SVGButton(c, CloseIcon, "收起工具面板").Clicked() {
				closed = true
			}
		})
	}, 330, 100)
	for _, dark := range []bool{false, true} {
		tt.SetDark(dark)
		if err := tt.Click("文件"); err != nil {
			t.Fatal(err)
		}
		tt.Key(0, ui.KeyRight)
		if selected != 2 {
			t.Fatal("native tab navigation lost the persistent ID", selected)
		}
		tt.Key(0, ui.KeyRight)
		if selected != 3 {
			t.Fatal(selected)
		}
		close, ok := tt.Find("收起工具面板")
		if !ok || close.X+close.W > 330 || close.Y < 0 || close.H < 28 {
			t.Fatal("action disappeared in a narrow dock", close)
		}
	}
	if err := tt.Click("收起工具面板"); err != nil || !closed {
		t.Fatal("vector action lost its accessible click", err)
	}
}

func TestButtonVariantsKeepHoverAndPressedFeedback(t *testing.T) {
	for _, variant := range []ButtonVariant{Secondary, Primary, Destructive, Quiet} {
		t.Run(fmt.Sprint(variant), func(t *testing.T) {
			tt := ui.NewTester(func(c *ui.Context) {
				ui.Column(c).Fill().Padding(20).Background(c.Theme().Background).Children(func() {
					Button(c, "操作", ButtonOptions{Variant: variant}).Width(140)
				})
			}, 300, 120)
			for _, dark := range []bool{false, true} {
				tt.SetDark(dark)
				tt.Move(250, 100)
				r, _ := tt.Find("操作")
				sample := func() any { return tt.Image().RGBAAt(int(r.X+8), int(r.Y+r.H/2)) }
				idle := sample()
				tt.Move(r.X+r.W/2, r.Y+r.H/2)
				hover := sample()
				tt.Press(r.X+r.W/2, r.Y+r.H/2)
				pressed := sample()
				tt.Release(r.X+r.W/2, r.Y+r.H/2)
				if idle == hover || hover == pressed {
					t.Fatal("state feedback missing", dark, idle, hover, pressed)
				}
			}
		})
	}
}

func TestDialogKeepsValidationFocusAndExplicitAcceptance(t *testing.T) {
	open, value, problem := true, "", ""
	accepted, behind := 0, 0
	tt := ui.NewTester(func(c *ui.Context) {
		if Button(c, "背景操作").Clicked() {
			behind++
		}
		Dialog(c, &open, DialogOptions{Title: "新建文件", Target: "/srv"}, func() {
			ui.Field(c, "名称", func() { ui.TextInput(c, &value).Label("名称").AutoFocus() }).Error(problem)
		}, func() {
			if Button(c, "取消").Clicked() {
				open = false
			}
			if PrimaryButton(c, "创建").Clicked() {
				if strings.Contains(value, "/") || value == "" {
					problem = "请输入有效名称"
					return
				}
				accepted++
				open = false
			}
		})
	}, 800, 520)
	for range 8 {
		tt.Key(0, ui.KeyTab)
		if tt.Focused("背景操作") {
			t.Fatal("focus escaped the modal")
		}
	}
	if err := tt.Click("名称"); err != nil {
		t.Fatal(err)
	}
	tt.Type("../bad")
	if err := tt.Click("创建"); err != nil {
		t.Fatal(err)
	}
	if !open || value != "../bad" || accepted != 0 || !tt.HasText("请输入有效名称") {
		t.Fatal("validation dismissed dialog or lost input", open, value, tt.Texts())
	}
	tt.Key(0, ui.KeyEscape)
	if open || accepted != 0 || behind != 0 {
		t.Fatal("Escape invoked an action")
	}
	open = true
	value = "ok.txt"
	problem = ""
	tt.Frame()
	if err := tt.Click("创建"); err != nil {
		t.Fatal(err)
	}
	if open || accepted != 1 {
		t.Fatal("explicit accept failed")
	}
}

func TestTableSortRefreshAndIdentityStayTogether(t *testing.T) {
	items := []struct {
		id   string
		size int
	}{{"zeta", 2}, {"alpha", 100}, {"beta", 10}}
	selected := 2
	state := ui.ListState{Selected: &selected}
	order := ui.SortOrder{Column: "name"}
	state.Sort = &order
	var visible []string
	tt := ui.NewTester(func(c *ui.Context) {
		rows := slices.Clone(items)
		slices.SortStableFunc(rows, func(a, b struct {
			id   string
			size int
		}) int {
			v := strings.Compare(a.id, b.id)
			if order.Column == "size" {
				v = cmp.Compare(a.size, b.size)
			}
			if order.Descending {
				return -v
			}
			return v
		})
		visible = nil
		state.Key = func(i int) any { return rows[i].id }
		state.Label = func(i int) string { return rows[i].id }
		Table(c, &state, []ui.TableColumn{{ID: "name", Title: "名称", Sortable: true}, {ID: "size", Title: "大小", Sortable: true}}, len(rows), func(i, col int) {
			if col == 0 {
				visible = append(visible, rows[i].id)
				CellText(c, rows[i].id)
			} else {
				CellText(c, fmt.Sprint(rows[i].size))
			}
		}).Fill()
	}, 600, 400)
	if err := tt.Click("beta"); err != nil {
		t.Fatal(err)
	}
	if err := tt.Click("大小"); err != nil {
		t.Fatal(err)
	}
	// No explicit Frame: the component must request the sorted frame itself.
	if !slices.Equal(visible, []string{"zeta", "beta", "alpha"}) || order.Column != "size" || order.Descending {
		t.Fatal("header did not refresh numeric sort", order, visible)
	}
	if selected != 1 {
		t.Fatal("selection lost its key after sorting", selected)
	}
	if err := tt.Click("大小"); err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(visible, []string{"alpha", "beta", "zeta"}) || !order.Descending {
		t.Fatal(order, visible)
	}
}

func TestVirtualTreeKeyboardAndLargeDataStayVirtual(t *testing.T) {
	selected, accesses, toggled, navigated := 0, 0, -1, -1
	state := ui.ListState{Selected: &selected}
	tt := ui.NewTester(func(c *ui.Context) {
		VirtualTree(c, &state, 100000, func(i int) TreeItem {
			accesses++
			if i == 0 {
				return TreeItem{ID: "/", Name: "/", Parent: -1, Branch: true, Expanded: true}
			}
			return TreeItem{ID: fmt.Sprintf("/%06d", i), Name: fmt.Sprintf("folder-%06d", i), Depth: 1, Parent: 0, Branch: true}
		}, TreeOptions{Label: "目录树", OnToggle: func(i int) { toggled = i }, OnSelect: func(i int) { navigated = i }})
	}, 280, 400)
	accesses = 0
	tt.Frame()
	if accesses > 200 {
		t.Fatal("tree traversed the full index on a frame", accesses)
	}
	if err := tt.Click("folder-000001"); err != nil {
		t.Fatal(err)
	}
	if selected != 1 || navigated != 1 {
		t.Fatal(selected, navigated)
	}
	tt.Key(0, ui.KeyRight)
	if toggled != 1 {
		t.Fatal("Right did not expand current folder")
	}
	tt.Key(0, ui.KeyLeft)
	if navigated != 0 {
		t.Fatal("Left did not navigate to parent")
	}
	tt.Scroll(120, 200, 0, 10000)
	if tt.HasText("folder-000001") {
		t.Fatal("tree did not scroll")
	}
}

func TestFileListUsesRawIdentityAndVirtualRows(t *testing.T) {
	selected, accesses := -1, 0
	var chosen ui.Selection[string]
	state := ui.ListState{Selected: &selected, Selection: &chosen}
	tt := ui.NewTester(func(c *ui.Context) {
		FileList(c, &state, 100000, func(i int) FileEntry {
			accesses++
			return FileEntry{ID: fmt.Sprintf("raw-%d", i), Name: fmt.Sprintf("file-%06d.txt", i), Bytes: 2048}
		}, FileListOptions{Label: "文件列表", ShowModified: true})
	}, 700, 400)
	accesses = 0
	tt.Frame()
	if accesses > 300 {
		t.Fatal("file list rendered all entries", accesses)
	}
	if !tt.HasText("2.0 KiB") {
		t.Fatal("shared file size missing")
	}
	if err := tt.Click("file-000001.txt"); err != nil {
		t.Fatal(err)
	}
	if !chosen.Has("raw-1") {
		t.Fatal("display label replaced the actual file identity")
	}
	tt.Scroll(400, 200, 0, 10000)
	if tt.HasText("file-000001.txt") {
		t.Fatal("file list did not scroll")
	}
}
