package components

import (
	"testing"

	"github.com/egoist/mygo/ui"
)

func TestDirectoryTreeKeepsLeafAlignmentAndSeparateDisclosure(t *testing.T) {
	selected, toggled, navigated := 0, -1, -1
	state := ui.ListState{Selected: &selected}
	rows := []TreeItem{
		{ID: "/", Name: "/", Parent: -1, Branch: true, Expanded: true},
		{ID: "/assets", Name: "assets", Depth: 1, Parent: 0, Branch: true},
		{ID: "/logs", Name: "logs", Depth: 1, Parent: 0},
	}
	tt := ui.NewTester(func(c *ui.Context) {
		DirectoryTree(c, &state, len(rows), func(i int) TreeItem { return rows[i] }, TreeOptions{
			Label: "目录树", OnToggle: func(i int) { toggled = i }, OnSelect: func(i int) { navigated = i },
		})
	}, 220, 200)
	branch, branchOK := tt.Find("assets")
	leaf, leafOK := tt.Find("logs")
	if !branchOK || !leafOK || branch.X != leaf.X {
		t.Fatal("empty sibling folder drifted horizontally", branch, leaf)
	}
	if err := tt.Click("展开或折叠 /assets"); err != nil {
		t.Fatal(err)
	}
	if toggled != 1 || selected != 0 || navigated != -1 {
		t.Fatal("disclosure changed the current directory", selected, navigated, toggled)
	}
	if err := tt.Click("logs"); err != nil {
		t.Fatal(err)
	}
	if selected != 2 || navigated != 2 {
		t.Fatal("empty folder cannot be selected", selected, navigated)
	}
}
