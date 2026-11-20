package desktop

import (
	"testing"
	"time"

	"github.com/egoist/mygo/ui"
	"sshclient/internal/sshdata"
)

func TestFileHeadersSortSizeAndTimeWithoutChangingSelection(t *testing.T) {
	a := fileFollowDemo()
	p := a.primaryFilePane
	p.treeHidden = true
	b := p.browser()
	dir := b.dir
	old := time.Unix(100, 0)
	a.applyDirectory(b, false, dir, []sshdata.File{
		{Name: "folder", Directory: true},
		{Name: "alpha.txt", Size: 100, Modified: old},
		{Name: "beta.txt", Size: 4, Modified: old.Add(time.Hour)},
		{Name: "zeta.txt", Size: 40, Modified: old.Add(time.Minute)},
	}, b.seq, true)
	tt := ui.NewTester(func(c *ui.Context) { a.browserPane(c, p) }, 950, 520)
	click := func(label string) {
		t.Helper()
		if err := tt.Click(label); err != nil {
			t.Fatal(err)
		}
	}
	checkOrder := func(names ...string) {
		t.Helper()
		bottom := float32(-1)
		for _, name := range names {
			rect, found := tt.Find(name)
			if !found || rect.Y <= bottom {
				t.Fatal("unexpected row order", names, b.fileRows)
			}
			bottom = rect.Y
		}
		selection := chosenFiles(b)
		if len(selection) != 1 || selection[0].Name != "alpha.txt" || b.anchor < 0 || b.fileRows[b.anchor].Name != "alpha.txt" {
			t.Fatal("sorting changed the selected item or range anchor", selection, b.anchor)
		}
	}
	click("alpha.txt")
	click("大小")
	checkOrder("folder", "beta.txt", "zeta.txt", "alpha.txt")
	click("大小")
	checkOrder("folder", "alpha.txt", "zeta.txt", "beta.txt")
	click("修改时间")
	checkOrder("folder", "alpha.txt", "zeta.txt", "beta.txt")
	click("修改时间")
	checkOrder("folder", "beta.txt", "zeta.txt", "alpha.txt")
	if err := tt.RightClick("beta.txt"); err != nil {
		t.Fatal(err)
	}
	if p.contextPopup == nil || p.contextPopup.name != "beta.txt" {
		t.Fatal("sorted row's context menu lost the file", p.contextPopup)
	}
	tt.Key(0, ui.KeyEscape)
	click("alpha.txt")
	tt.SetSize(340, 520)
	tt.Frame()
	if _, visible := tt.Find("修改时间"); visible {
		t.Fatal("test pane is not narrow enough for the menu fallback")
	}
	click("文件窗口菜单")
	if err := tt.ChooseMenuItem("按修改时间排序", "升序"); err != nil {
		t.Fatal(err)
	}
	checkOrder("folder", "alpha.txt", "zeta.txt", "beta.txt")
}
