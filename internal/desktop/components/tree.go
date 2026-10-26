package components

import "github.com/egoist/mygo/ui"

// TreeItem is one row in a cached, flattened tree. The caller owns lazy loading
// and rebuilding the index. Item is evaluated only for visible/adjacent rows.
type TreeItem struct {
	ID, Name         string
	Depth, Parent    int
	Branch, Expanded bool
}

type TreeOptions struct {
	Label              string
	OnSelect, OnToggle func(int)
	Decorate           func(ui.Element, int)
}

func VirtualTree(c *ui.Context, state *ui.ListState, count int, item func(int) TreeItem, o TreeOptions) ui.Element {
	return virtualTree(c, state, count, item, o, nil)
}

// DirectoryTree shares the file list's icon/name presentation. Branch means
// expandable, not directory: an empty folder still has a folder icon.
func DirectoryTree(c *ui.Context, state *ui.ListState, count int, item func(int) TreeItem, o TreeOptions) ui.Element {
	return virtualTree(c, state, count, item, o, func(r TreeItem, selected bool) {
		FileName(c, FileEntry{Name: r.Name, Directory: true}, FileNameOptions{Expanded: r.Expanded && r.Branch, Selected: selected})
	})
}

func virtualTree(c *ui.Context, state *ui.ListState, count int, item func(int) TreeItem, o TreeOptions, content func(TreeItem, bool)) ui.Element {
	defer fileViewTheme(c)()
	state.Key = func(i int) any { return item(i).ID }
	state.Label = func(i int) string { return item(i).ID }
	list := List(c, state, count, func(i int) {
		r := item(i)
		row := ui.Row(c).FillWidth().MinWidth(0).Height(FileRowHeight).Padding(0, 6).
			PaddingStart(float32(min(max(r.Depth, 0), 12))*TreeIndent + 3).Gap(2).AlignItems(ui.Center)
		row.Children(func() {
			if r.Branch {
				arrow := ChevronRightIcon
				if r.Expanded {
					arrow = ChevronDownIcon
				}
				button := SVGButton(c, arrow, tr(c, "展开或折叠 ")+r.ID).Size(16, 22).Shrink(0)
				if button.Clicked() && o.OnToggle != nil {
					o.OnToggle(i)
				}
			} else {
				ui.Spacer(c).Width(16).Grow(0).Shrink(0)
			}
			if content != nil {
				content(r, state.Selected != nil && *state.Selected == i)
			} else {
				ui.Text(c, r.Name).SingleLine().FontSize(BodyFont).Tooltip(r.ID)
			}
		})
		if o.Decorate != nil {
			o.Decorate(row, i)
		}
	}).Fill().Padding(3).Background(c.Theme().Background.Mix(c.Theme().Surface, .6)).Label(o.Label)
	selected := -1
	if state.Selected != nil {
		selected = *state.Selected
	}
	if selected < 0 || selected >= count {
		return list
	}
	if list.Changed() && o.OnSelect != nil {
		o.OnSelect(selected)
	}
	if list.Submitted() && o.OnToggle != nil {
		o.OnToggle(selected)
	}
	r := item(selected)
	if state.Shortcut(c, 0, ui.KeyRight) {
		if r.Branch && !r.Expanded {
			if o.OnToggle != nil {
				o.OnToggle(selected)
			}
		} else if r.Branch && selected+1 < count && item(selected+1).Parent == selected && o.OnSelect != nil {
			o.OnSelect(selected + 1)
		}
	}
	if state.Shortcut(c, 0, ui.KeyLeft) {
		if r.Expanded {
			if o.OnToggle != nil {
				o.OnToggle(selected)
			}
		} else if r.Parent >= 0 && r.Parent < count && o.OnSelect != nil {
			o.OnSelect(r.Parent)
		}
	}
	return list
}
