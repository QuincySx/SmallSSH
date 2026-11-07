package desktop

import (
	"slices"
	"strings"
	"testing"

	"github.com/egoist/mygo/ui"
	"sshclient/internal/sshconn"
)

func filePopupFixture(t *testing.T) (*app, *filePane, *directoryBrowser, *ui.Tester) {
	t.Helper()
	a, p, b := filePopupModel()
	tester := ui.NewTester(func(c *ui.Context) { a.browserPane(c, p) }, 800, 600)
	return a, p, b, tester
}

func filePopupModel() (*app, *filePane, *directoryBrowser) {
	a := &app{}
	a.ensurePanes()
	p := a.primaryFilePane
	p.endpoint, p.treeHidden = "web", true
	b := p.browser()
	a.applyDirectory(b, false, "/srv", []sshconn.File{{Name: "first.txt", Size: 11}, {Name: "second.txt", Size: 22}}, b.seq, true)
	return a, p, b
}

func filePopupItem(t *testing.T, popup *fileContextPopup, label string) *fileMenuItem {
	t.Helper()
	for _, item := range popup.items {
		if item.label == label {
			return item
		}
	}
	t.Fatalf("missing popup action %q", label)
	return nil
}

func TestFilePopupRetargetsOpenMenuAndPinsActionToNewRow(t *testing.T) {
	a, p, b, tt := filePopupFixture(t)
	second, ok := tt.Find("second.txt")
	if !ok {
		t.Fatal("second row missing")
	}
	// The second press is on the visible part of the row, outside the popup.
	if err := tt.RightClick("first.txt"); err != nil {
		t.Fatal(err)
	}
	old := p.contextPopup
	if old == nil || !b.chosen.Has("first.txt") || len(tt.Menu()) != 0 {
		t.Fatal("row did not open its nonmodal popup", b.chosen, tt.Menu())
	}
	oldDelete := filePopupItem(t, old, "删除")
	tt.RightClickAt(second.X+second.W/4, second.Y+second.H/2)
	if p.contextPopup == nil || p.contextPopup == old || p.contextPopup.row != 1 || b.selected != 1 || b.anchor != 1 || b.chosen.Len() != 1 || !b.chosen.Has("second.txt") {
		t.Fatal("open popup did not switch selection and target", p.contextPopup, b.selected, b.anchor, b.chosen)
	}
	a.chooseFileContextItem(p, old, oldDelete)
	if a.prompt.open {
		t.Fatal("replaced popup remained actionable")
	}
	if err := tt.Click("重命名 / 移动"); err != nil {
		t.Fatal(err)
	}
	if a.prompt.target != "web · /srv/second.txt" || p.contextPopup != nil {
		t.Fatal("popup action did not use its new row", a.prompt.target)
	}
}

func TestFilePopupKeepsMultiSelectionAndSizeCellOwnsTarget(t *testing.T) {
	a, p, b, tt := filePopupFixture(t)
	if err := tt.Click("first.txt"); err != nil {
		t.Fatal(err)
	}
	if err := tt.ClickWith(ui.Cmd, "second.txt"); err != nil {
		t.Fatal(err)
	}
	if err := tt.RightClick("22 B"); err != nil {
		t.Fatal(err)
	}
	if p.contextPopup == nil || p.contextPopup.row != 1 || p.contextPopup.col != 1 || b.chosen.Len() != 2 {
		t.Fatal("size cell lost the selected group or row", p.contextPopup, b.chosen)
	}
	if err := tt.Click("删除"); err != nil {
		t.Fatal(err)
	}
	if !a.prompt.open || !strings.Contains(a.prompt.message, "first.txt") || !strings.Contains(a.prompt.message, "second.txt") {
		t.Fatal("group action lost its captured files", a.prompt)
	}
}

func TestFilePopupOutsidePressPassesThroughAndKeyboardCloses(t *testing.T) {
	a, p, b := filePopupModel()
	clicks := 0
	tt := ui.NewTester(func(c *ui.Context) {
		ui.Column(c).Fill().Children(func() {
			if ui.Button(c, "外部动作").Clicked() {
				clicks++
			}
			a.browserPane(c, p)
		})
	}, 800, 600)
	if err := tt.RightClick("first.txt"); err != nil {
		t.Fatal(err)
	}
	if err := tt.Click("外部动作"); err != nil {
		t.Fatal(err)
	}
	if clicks != 1 || p.contextPopup != nil {
		t.Fatal("outside press was consumed by the popup", clicks, p.contextPopup)
	}
	if err := tt.RightClick("second.txt"); err != nil {
		t.Fatal(err)
	}
	tt.Key(0, ui.KeyDown)
	tt.Key(0, ui.KeyUp)
	if !tt.Focused("打开 / 编辑") {
		t.Fatal("popup arrows did not return to the first action")
	}
	tt.Key(0, ui.KeyEscape)
	if p.contextPopup != nil || !b.chosen.Has("second.txt") {
		t.Fatal("Escape did not close the popup while retaining selection")
	}
}

func TestFilePopupTabAndEnterActivateFocusedAction(t *testing.T) {
	a, p, _, tt := filePopupFixture(t)
	if err := tt.RightClick("first.txt"); err != nil {
		t.Fatal(err)
	}
	tt.Key(0, ui.KeyTab)
	if !tt.Focused("下载到本机…") {
		t.Fatal("Tab did not reach the next enabled action")
	}
	tt.Key(0, ui.KeyTab)
	if !tt.Focused("重命名 / 移动") {
		t.Fatal("Tab did not skip the disabled transfer action")
	}
	tt.Key(0, ui.KeyEnter)
	if a.prompt.target != "web · /srv/first.txt" || p.contextPopup != nil {
		t.Fatal("Enter did not activate the focused file action", a.prompt)
	}
}

func TestFilePopupRejectsStaleContext(t *testing.T) {
	for _, change := range []string{"generation", "endpoint", "directory", "selection", "lock", "tool-source", "closed-tool"} {
		t.Run(change, func(t *testing.T) {
			a, p, b := filePopupModel()
			a.online = true
			tool := &workspaceToolTab{kind: toolFiles, source: &session{host: "web"}}
			a.workspaceTools = map[int]*workspaceToolTab{toolFiles: tool}
			tt := ui.NewTester(func(c *ui.Context) {
				a.renderingWorkspaceTool = tool
				a.browserPane(c, p)
				a.renderingWorkspaceTool = nil
			}, 800, 600)
			if err := tt.RightClick("first.txt"); err != nil {
				t.Fatal(err)
			}
			popup := p.contextPopup
			if popup == nil {
				t.Fatal("popup did not open")
			}
			item := filePopupItem(t, popup, "删除")
			switch change {
			case "generation":
				a.generation++
			case "endpoint":
				p.endpoint = "another"
			case "directory":
				b.dir = "/another"
			case "selection":
				b.chosen.Clear()
				b.chosen.Add("second.txt")
			case "lock":
				a.online = false
			case "tool-source":
				tool.source = &session{host: "another"}
			case "closed-tool":
				delete(a.workspaceTools, toolFiles)
			}
			a.chooseFileContextItem(p, popup, item)
			if a.prompt.open || a.renderingWorkspaceTool != nil {
				t.Fatal("stale popup mutated application state or leaked its tool scope")
			}
		})
	}
}

func TestFilePopupRetainsAllSharedActionsAndTransferSubmenu(t *testing.T) {
	a, p, _, tt := filePopupFixture(t)
	a.ensureFileLayout()
	a.splitFilePane(p, false, true)
	a.splitFilePane(p, true, true)
	tt.Frame()
	if err := tt.RightClick("first.txt"); err != nil {
		t.Fatal(err)
	}
	popup := p.contextPopup
	if popup == nil {
		t.Fatal("popup did not open")
	}
	var labels []string
	for _, item := range popup.items {
		labels = append(labels, item.label)
	}
	for _, label := range []string{"打开 / 编辑", "下载到本机…", "传输到窗口", "重命名 / 移动", "权限 / 属性", "删除", "新建文件夹", "新建文本文件", "刷新", "上一级", "跟随当前终端"} {
		if !slices.Contains(labels, label) {
			t.Fatal("shared action missing from popup", label, labels)
		}
	}
	transfer := filePopupItem(t, popup, "传输到窗口")
	if len(transfer.children) != 2 {
		t.Fatal("transfer submenu lost a destination", transfer.children)
	}
	if err := tt.Click("传输到窗口"); err != nil {
		t.Fatal(err)
	}
	if !tt.HasText(transfer.children[0].label) {
		t.Fatal("transfer destinations are not reachable")
	}
}
