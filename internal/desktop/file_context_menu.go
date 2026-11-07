package desktop

import (
	"fmt"
	"slices"

	"github.com/egoist/mygo/ui"
	"sshclient/internal/desktop/components"
	"sshclient/internal/sshconn"
)

// fileMenu lets native menus and the nonmodal list popup use the same action
// definitions. A popup collects the items, then replays the chosen ordinal and
// label, just as ui.Menu does for a native menu.
type fileMenu struct {
	native *ui.Menu
	run    *fileMenuRun
	items  []*fileMenuItem
}

type fileMenuRun struct {
	sequence, choice int
	label            string
}

type fileMenuItem struct {
	native    *ui.MenuItem
	run       *fileMenuRun
	id        int
	label     string
	disabled  bool
	checked   bool
	separator bool
	submenu   bool
	children  []*fileMenuItem
}

func (m *fileMenu) Item(label string) *fileMenuItem {
	if m.native != nil {
		return &fileMenuItem{native: m.native.Item(label)}
	}
	m.run.sequence++
	item := &fileMenuItem{run: m.run, id: m.run.sequence, label: label}
	m.items = append(m.items, item)
	return item
}

func (m *fileMenu) Separator() {
	if m.native != nil {
		m.native.Separator()
		return
	}
	m.Item("").separator = true
}

func (m *fileMenu) Submenu(label string, build func(*fileMenu)) {
	if m.native != nil {
		m.native.Submenu(label, func(menu *ui.Menu) { build(&fileMenu{native: menu}) })
		return
	}
	item := m.Item(label)
	item.submenu = true
	child := &fileMenu{run: m.run}
	build(child)
	item.children = child.items
}

func (item *fileMenuItem) Disabled(disabled bool) *fileMenuItem {
	if item.native != nil {
		item.native.Disabled(disabled)
	}
	item.disabled = disabled
	return item
}

func (item *fileMenuItem) Checked(checked bool) *fileMenuItem {
	if item.native != nil {
		item.native.Checked(checked)
	}
	item.checked = checked
	return item
}

func (item *fileMenuItem) Chosen() bool {
	if item.native != nil {
		return item.native.Chosen()
	}
	return !item.disabled && !item.submenu && !item.separator && item.run.choice == item.id && item.run.label == item.label
}

type fileMenuLocation struct {
	pane     *filePane
	endpoint string
	browser  *directoryBrowser
	dir      string
}

type fileContextPopup struct {
	open         bool
	row, col     int
	x, y         float32
	endpoint     string
	browser      *directoryBrowser
	dir          string
	name         string
	files        []sshconn.File
	generation   int
	online       bool
	sequence     uint64
	tool         *workspaceToolTab
	source       *session
	locations    []fileMenuLocation
	items        []*fileMenuItem
	submenu      *fileMenuItem
	focusPending bool
}

func (a *app) fileContextPopupValid(p *filePane, popup *fileContextPopup) bool {
	if popup == nil || !popup.open || p.contextPopup != popup || a.generation != popup.generation || a.online != popup.online || p.endpoint != popup.endpoint || p.states[popup.endpoint] != popup.browser {
		return false
	}
	b := popup.browser
	if b.dir != popup.dir || b.seq != popup.sequence || b.loadError[popup.dir] != "" || popup.row < 0 || popup.row >= len(b.cache[popup.dir]) || b.cache[popup.dir][popup.row].Name != popup.name {
		return false
	}
	if popup.tool != nil && (!a.online || a.workspaceTools[popup.tool.kind] != popup.tool || popup.tool.source != popup.source) {
		return false
	}
	// A destination changing while the popup is open must not redirect a
	// transfer whose item ordinal was collected for the previous layout.
	panes := a.fileLayout.panes()
	if len(panes) != len(popup.locations) {
		return false
	}
	for i, location := range popup.locations {
		if panes[i] != location.pane || location.pane.endpoint != location.endpoint || location.pane.states[location.endpoint] != location.browser || location.browser.dir != location.dir {
			return false
		}
	}
	files := chosenFiles(b)
	return slices.Equal(files, popup.files)
}

func (a *app) validateFileContextPopup(p *filePane) {
	if p.contextPopup != nil && !a.fileContextPopupValid(p, p.contextPopup) {
		p.contextPopup.open = false
		p.contextPopup = nil
	}
}

func (a *app) fileCellContextMenu(c *ui.Context, cell ui.Element, p *filePane, b *directoryBrowser, dir string, row, col int) {
	tool, generation, online := a.renderingWorkspaceTool, a.generation, a.online
	endpoint, name := p.endpoint, b.cache[dir][row].Name
	var source *session
	if tool != nil {
		source = tool.source
	}
	cell.HandleInput(func(event ui.InputEvent) bool {
		if event.Kind != ui.InputPointerDown {
			return false
		}
		if event.Button == 0 {
			if p.contextPopup != nil {
				p.contextPopup.open = false
				p.contextPopup = nil
			}
			return false
		}
		if event.Button != 1 {
			return false
		}
		if a.generation != generation || a.online != online || p.endpoint != endpoint || p.states[endpoint] != b || b.dir != dir || row >= len(b.cache[dir]) || b.cache[dir][row].Name != name || tool != nil && (!a.online || a.workspaceTools[tool.kind] != tool || tool.source != source) {
			return true
		}
		for _, pane := range a.allFilePanes() {
			if pane.contextPopup != nil {
				pane.contextPopup.open = false
				pane.contextPopup = nil
			}
		}
		if !b.chosen.Has(name) {
			b.chosen.Clear()
			b.chosen.Add(name)
			b.selected, b.anchor = row, row
		}
		a.activeFilePane = p
		popup := &fileContextPopup{open: true, row: row, col: col, x: event.X, y: event.Y, endpoint: p.endpoint, browser: b, dir: dir, name: name,
			files: slices.Clone(chosenFiles(b)), generation: generation, online: online, sequence: b.seq, tool: tool, source: source, focusPending: true}
		for _, pane := range a.fileLayout.panes() {
			popup.locations = append(popup.locations, fileMenuLocation{pane, pane.endpoint, pane.browser(), pane.browser().dir})
		}
		menu := &fileMenu{run: &fileMenuRun{}}
		a.fileContextActions(menu, p, dir, popup.files)
		popup.items = menu.items
		p.contextPopup = popup
		return true
	})
	popup := p.contextPopup
	if popup == nil || popup.row != row || popup.col != col {
		return
	}
	ui.PopoverBase(c.Key(fmt.Sprintf("file-context:%p", popup)), cell, &popup.open, func(panel ui.Element) {
		panel.AttachTo(cell, ui.AnchorTopLeft, ui.AnchorTopLeft).Margin(popup.y, 0, 0, popup.x).
			Width(252).Padding(4).Radius(8).Background(c.Theme().Background).Border(1, c.Theme().Border).
			Shadow(0, 6, 20, 0, ui.RGBA(0, 0, 0, .18)).Role(ui.RoleMenu).Label(tr(c, "文件操作菜单"))
		items := popup.items
		submenu := popup.submenu
		if popup.submenu != nil {
			items = popup.submenu.children
		}
		var rows []ui.Element
		ui.Scroll(c).FillWidth().MaxHeight(460).Children(func() {
			ui.Column(c).FillWidth().Gap(0).Children(func() {
				if popup.submenu != nil {
					back := filePopupButton(c, tr(c, "返回"), false, false, false)
					rows = append(rows, back)
					if back.Clicked() || back.Shortcut(0, ui.KeyLeft) {
						popup.submenu, popup.focusPending = nil, true
					}
				}
				for _, item := range items {
					if item.separator {
						ui.Box(c).FillWidth().Height(1).Margin(4, 6).Background(c.Theme().Border)
						continue
					}
					button := filePopupButton(c.Key(item.id), item.label, item.disabled, item.checked, item.submenu)
					if !item.disabled {
						rows = append(rows, button)
					}
					if button.Clicked() || item.submenu && button.Shortcut(0, ui.KeyRight) {
						if item.submenu {
							popup.submenu, popup.focusPending = item, true
						} else {
							a.chooseFileContextItem(p, popup, item)
						}
					}
				}
			})
		})
		if popup.focusPending && popup.submenu == submenu && len(rows) > 0 {
			rows[0].Focus()
			popup.focusPending = false
		}
		for i, row := range rows {
			if row.Shortcut(0, ui.KeyDown) {
				rows[(i+1)%len(rows)].Focus().ScrollIntoView()
			}
			if row.Shortcut(0, ui.KeyUp) {
				rows[(i+len(rows)-1)%len(rows)].Focus().ScrollIntoView()
			}
		}
		if popup.submenu != nil && panel.Shortcut(0, ui.KeyLeft) {
			popup.submenu, popup.focusPending = nil, true
		}
	})
	if !popup.open && p.contextPopup == popup {
		p.contextPopup = nil
	}
}

func filePopupButton(c *ui.Context, label string, disabled, checked, submenu bool) ui.Element {
	return components.Button(c, "", components.ButtonOptions{Variant: components.Quiet, Disabled: disabled}).
		FillWidth().Height(30).Radius(4).Padding(4, 8).Label(label).Role(ui.RoleMenuItem).Children(func() {
		mark := ""
		if checked {
			mark = "✓"
		}
		ui.Text(c, mark).Width(16).FontSize(12)
		ui.Text(c, label).Grow(1).SingleLine().FontSize(12)
		if submenu {
			ui.Text(c, "›").FontSize(14)
		}
	})
}

func (a *app) chooseFileContextItem(p *filePane, popup *fileContextPopup, item *fileMenuItem) {
	if !a.fileContextPopupValid(p, popup) || item.disabled || item.separator || item.submenu {
		return
	}
	popup.open, p.contextPopup = false, nil
	previous := a.renderingWorkspaceTool
	a.renderingWorkspaceTool = popup.tool
	defer func() { a.renderingWorkspaceTool = previous }()
	a.fileContextActions(&fileMenu{run: &fileMenuRun{choice: item.id, label: item.label}}, p, popup.dir, popup.files)
}
