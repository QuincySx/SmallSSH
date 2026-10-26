package desktop

import (
	"fmt"
	"sshclient/internal/desktop/components"

	"github.com/egoist/mygo/ui"
	"sshclient/internal/sshconn"
)

// File panes deliberately have no action toolbar. The tree and list share
// contextual actions; transfers keep an immutable snapshot of both locations.
const (
	toolFiles = iota
	toolTransfers
	toolCommands
	toolDiagnostics
)

func (a *app) showTool(index int) {
	if index == toolTransfers {
		a.showTransfers()
		return
	}
	a.dock, a.toolsHidden, a.page = index, false, 1
	if a.toolsDetached {
		a.detachTools()
	}
}

func (a *app) fileArea(c *ui.Context) {
	a.ensureFileLayout()
	if a.dock == toolTransfers {
		a.dock = toolFiles // Reserved persisted ID; transfers now have a global entry.
	}
	ui.Column(c).Fill().Gap(0).Label(tr(c, "工具面板")).Children(func() {
		components.PanelBar(c, &a.dock, []components.PanelTab{{ID: toolFiles, Title: tr(c, "文件")}, {ID: toolCommands, Title: tr(c, "命令")}, {ID: toolDiagnostics, Title: tr(c, "服务器状态")}}, func() {
			if a.fileExpanded && !a.renderingToolsWindow {
				if components.Button(c, tr(c, "恢复分栏")).Label(tr(c, "恢复终端分栏")).Height(28).Clicked() {
					a.fileExpanded = false
				}
			}
			switch components.PanelLayoutMenu(c, &a.toolsLayoutOpen, a.toolsDockRight(c), a.fileExpanded, a.toolsDetached) {
			case components.PanelDetach:
				a.detachTools()
			case components.PanelDockBottom:
				a.setToolsDock(false)
			case components.PanelDockRight:
				a.setToolsDock(true)
			case components.PanelToggleExpanded:
				a.fileExpanded = !a.fileExpanded
				a.contentFocus = true
			case components.PanelHide:
				a.setToolsHidden(true)
			}
			if components.SVGButton(c, components.CloseIcon, tr(c, "收起工具面板")).Tooltip(tr(c, "收起工具面板 · Cmd/Ctrl+Shift+J")).Clicked() {
				a.setToolsHidden(true)
			}
		})
		if a.toolsDetached != a.renderingToolsWindow || a.toolsHidden {
			return
		}
		switch a.dock {
		case toolCommands:
			a.commandsView(c)
		case toolDiagnostics:
			a.diagnosticsView(c)
		default:
			a.fileWindows(c, a.fileLayout)
		}
	})
}

func (a *app) paneSide(p *filePane) string {
	if len(a.fileLayout.panes()) <= 1 {
		return ""
	}
	return a.filePaneLabel(p)
}

func (a *app) browserPane(c *ui.Context, p *filePane) {
	a.ensureFilePaneID(p)
	pane := ui.Column(c).FillWidth().Grow(1).MinHeight(0).MinWidth(0).Gap(0).Key(p.id).Label(a.filePaneLabel(p))
	pane.Children(func() {
		if width := pane.Bounds().W; width > 0 {
			if fitted := min(p.treeWidth, max(60, width*.4)); fitted != p.treeWidth {
				p.treeWidth = fitted
				c.Invalidate()
			}
		}
		// The header and body share the actual split, including its native
		// divider limits. There is no separate toolbar offset to keep in sync.
		ui.Split(c, &p.treeWidth, func() {
			ui.Column(c).Fill().MinWidth(0).MinHeight(0).Gap(0).Key("directory-column").Children(func() {
				a.fileLocationHeader(c, p)
				b := p.browser()
				if p.endpoint != "" && a.online && a.server != nil && a.filePaneVisible(p) {
					if _, loaded := b.cache[b.dir]; !loaded && b.loading[b.dir] == nil && b.loadError[b.dir] == "" {
						a.loadDirectory(p, b.dir, true)
					}
				}
				a.directoryTree(c, p, b)
			})
		}, func() {
			ui.Column(c).Fill().MinWidth(0).MinHeight(0).Gap(0).Key("files-column").Children(func() {
				a.filePathHeader(c, p)
				a.fileList(c, p, p.browser())
			})
		}).FillWidth().Grow(1).MinHeight(0)
		b := p.browser()
		text := fmt.Sprintf(tr(c, "%d 项"), len(b.cache[b.dir]))
		if _, loaded := b.cache[b.dir]; !loaded && len(b.loading) == 0 {
			text = ""
		}
		if n := b.chosen.Len(); n > 0 {
			text += fmt.Sprintf(tr(c, " · 已选 %d 项"), n)
		}
		if len(b.loading) > 0 {
			text += tr(c, " · 正在读取…")
		}
		if b.loadError[b.dir] != "" {
			text = tr(c, "读取失败 · ") + b.loadError[b.dir]
		}
		ui.Row(c).FillWidth().Shrink(0).Padding(3, 8).Gap(8).Children(func() {
			ui.Text(c, text).SingleLine().Grow(1).FontSize(11).TextColor(c.Theme().TextMuted).Tooltip(text)
			if b.loadError[b.dir] != "" && components.Button(c, tr(c, "重试")).Clicked() {
				a.loadDirectory(p, b.dir, true)
			}
		})
	})
	if pane.FocusWithin() {
		a.activeFilePane = p
	}
}

func (a *app) fileLocationHeader(c *ui.Context, p *filePane) {
	choice, label := p.endpoint, a.endpointLabel(p.endpoint)
	if p.endpoint != "" && p.endpoint != localEndpoint {
		found := false
		for _, connection := range a.connections {
			if connection.ID == p.endpoint {
				found = true
				break
			}
		}
		if !found {
			label = reviewText(p.endpoint)
		}
	}
	tip, followLabel := label, tr(c, "文件来源跟随终端")
	if p.follow {
		choice = followTerminalEndpoint
		tip = tr(c, "跟随当前终端 · ") + reviewText(a.endpointName(p.endpoint))
	}
	if s := a.currentTerminal(); p.follow && s != nil && s.target.Kind == "docker-terminal" {
		followLabel = tr(c, "容器宿主机文件来源")
		tip = tr(c, "浏览容器所在 SSH 服务器的文件（宿主机） · ") + reviewText(a.endpointName(p.endpoint))
	}
	ui.Row(c).FillWidth().Height(components.FileHeaderHeight).Shrink(0).MinWidth(0).
		Background(c.Theme().Background.Mix(c.Theme().Surface, .6)).
		BorderWidth(0, 0, 1, 0).BorderColor(c.Theme().Border).Children(func() {
		selectPane := ui.SelectBase(c, &choice)
		selectPane.Trigger.FillWidth().MinWidth(0).Height(components.FileHeaderHeight).
			Padding(0, 8).Gap(4).Justify(ui.Start).Label(a.paneSide(p) + tr(c, "文件位置")).Tooltip(tip).Children(func() {
			ui.Text(c, label).Grow(1).MinWidth(0).FontSize(components.BodyFont).SingleLine()
			if p.follow {
				ui.Icon(c, components.FollowIcon).Size(14, 14).Shrink(0).TextColor(c.Theme().TextMuted).Label(followLabel).Tooltip(tip)
			}
			ui.Icon(c, components.ChevronDownIcon).Size(12, 12).Shrink(0).TextColor(c.Theme().TextMuted)
		})
		selectPane.Popup(func(panel ui.Element) {
			panel.MinWidth(180).Padding(4).Background(c.Theme().Surface).Border(1, c.Theme().Border)
			selectPane.Item(followTerminalEndpoint).Padding(6, 10).Children(func() { ui.Text(c, tr(c, "跟随当前终端")) })
			selectPane.Item(localEndpoint).Padding(6, 10).Children(func() { ui.Text(c, tr(c, "本机")) })
			for _, host := range a.connections {
				selectPane.Item(host.ID).Padding(6, 10).Children(func() { ui.Text(c, reviewText(host.Name)) })
			}
		})
		if selectPane.Trigger.Changed() {
			a.chooseFileEndpoint(p, choice)
			b := p.browser()
			if _, loaded := b.cache[b.dir]; !loaded {
				a.loadDirectory(p, b.dir, true)
			}
		}
	})
}

func (a *app) filePathHeader(c *ui.Context, p *filePane) {
	side, b := a.paneSide(p), p.browser()
	ui.Row(c).FillWidth().Height(components.FileHeaderHeight).Shrink(0).MinWidth(0).Gap(0).
		AlignItems(ui.Center).BorderWidth(0, 0, 1, 0).BorderColor(c.Theme().Border).Children(func() {
		// One border DIP + seven padding DIPs align the editable text with
		// the table header's eight-DIP content inset.
		c.Key("directory-path")
		input := ui.TextInput(c, &b.address).Grow(1).MinWidth(28).Height(28).Padding(4, 7).
			FontSize(components.BodyFont).Background(ui.Transparent).Border(1, ui.Transparent).
			Label(side + tr(c, "目录路径")).Tooltip(tr(c, "输入目录路径，按 Enter 打开"))
		if input.Submitted() {
			a.navigateDirectory(p, b.address)
		}
		if components.SVGButton(c, components.RefreshIcon, side+tr(c, "刷新目录")).Size(28, 28).Shrink(0).
			Tooltip(tr(c, "刷新（F5）")).Disabled(b.loading[b.dir] != nil).Clicked() {
			a.loadDirectory(p, b.dir, true)
		}
		components.SVGButton(c, components.MoreIcon, side+tr(c, "文件窗口菜单")).Size(28, 28).Shrink(0).
			Tooltip(tr(c, "文件窗口布局")).Menu(func(m *ui.Menu) {
			a.activeFilePane = p
			a.fileWindowsMenu(m, p)
		})
	})
}

func (a *app) fileList(c *ui.Context, p *filePane, b *directoryBrowser) {
	endpoint, dir := p.endpoint, b.dir
	fs := b.cache[dir]
	b.table.Selected, b.table.Selection = &b.selected, &b.chosen
	table := components.FileList(c, &b.table, len(fs), func(i int) components.FileEntry {
		f := fs[i]
		return components.FileEntry{ID: f.Name, Name: reviewText(f.Name), Directory: f.Directory, Bytes: uint64(max(f.Size, 0)), Modified: f.Modified}
	}, components.FileListOptions{Label: a.paneSide(p) + tr(c, "文件列表"), ShowModified: a.fileExpanded || a.renderingToolsWindow, Decorate: func(cell ui.Element, i int) {
		f := fs[i]
		cell.ContextMenu(func(m *ui.Menu) {
			if !b.chosen.Has(f.Name) {
				b.chosen.Clear()
				b.chosen.Add(f.Name)
				b.selected = i
			}
			a.fileContextMenu(m, p, dir, chosenFiles(b))
		})
		if cell.Clicked() {
			b.chooseFile(fs, i, cell.ClickModifiers())
		}
		if cell.DoubleClicked() {
			a.openEntries(p, dir, []sshconn.File{f})
		}
		if !f.Directory {
			cell.Drag(fileDrag{endpoint: endpoint, dir: dir, file: f, browser: b})
		}
		target := dir
		if f.Directory {
			target = joinPath(endpoint == localEndpoint, dir, f.Name)
		}
		a.fileDropTarget(c, cell, p, target)
	}})
	table.ContextMenu(func(m *ui.Menu) { a.directoryContextMenu(m, p, b, dir) })
	a.fileDropTarget(c, table, p, dir)
	if table.Submitted() {
		a.openPaneFile(p)
	}
	if b.table.Shortcut(c, 0, ui.KeyF5) {
		a.loadDirectory(p, dir, true)
	}
	if b.table.Shortcut(c, ui.Alt, ui.KeyUp) {
		a.navigateDirectory(p, parentDir(endpoint == localEndpoint, dir))
	}
	if b.table.Shortcut(c, 0, ui.KeyF2) {
		a.fileOperation(p, "rename")
	}
	if b.table.Shortcut(c, 0, ui.KeyDelete) || b.table.Shortcut(c, ui.Cmd, ui.KeyBackspace) {
		a.fileOperation(p, "remove")
	}
}

// A cell owns its pointer when it is a drag source. Forward ordinary clicks
// to the same keyed selection used by the native table's keyboard handling.
func (b *directoryBrowser) chooseFile(files []sshconn.File, index int, mods ui.Modifiers) {
	if mods&ui.Shift != 0 {
		anchor := b.anchor
		if anchor < 0 || anchor >= len(files) {
			anchor = b.selected
		}
		if anchor < 0 || anchor >= len(files) {
			anchor = index
		}
		if mods&ui.Cmd == 0 {
			b.chosen.Clear()
		}
		for i := min(anchor, index); i <= max(anchor, index); i++ {
			b.chosen.Add(files[i].Name)
		}
	} else if mods&ui.Cmd != 0 {
		name := files[index].Name
		if b.chosen.Has(name) {
			b.chosen.Remove(name)
		} else {
			b.chosen.Add(name)
		}
		b.anchor = index
	} else {
		b.chosen.Clear()
		b.chosen.Add(files[index].Name)
		b.anchor = index
	}
	b.selected = index
}

func (a *app) otherPane(p *filePane) *filePane {
	panes := a.fileLayout.panes()
	if len(panes) == 2 {
		if panes[0] == p {
			return panes[1]
		}
		if panes[1] == p {
			return panes[0]
		}
	}
	return nil
}

func (a *app) fileContextMenu(m *ui.Menu, p *filePane, dir string, files []sshconn.File) {
	// Capture a pane containing the original browser: a later terminal switch
	// cannot redirect a menu action or an outstanding confirmation.
	endpoint, b := p.endpoint, p.browser()
	source := &filePane{endpoint: endpoint, states: map[string]*directoryBrowser{endpoint: b}}
	if m.Item(tr(nil, "打开 / 编辑")).Disabled(len(files) != 1).Chosen() {
		a.openEntries(source, dir, files)
	}
	other := a.otherPane(p)
	can := other != nil && endpoint != "" && other.endpoint != "" && (endpoint == localEndpoint) != (other.endpoint == localEndpoint)
	for _, f := range files {
		if f.Directory {
			can = false
		}
	}
	label := tr(nil, "上传到另一栏")
	if endpoint != localEndpoint {
		label = tr(nil, "下载到另一栏")
	}
	if len(a.fileLayout.panes()) <= 2 {
		if m.Item(label).Disabled(!can || len(files) == 0).Chosen() {
			a.transferEntries(endpoint, dir, files, other.endpoint, other.browser().dir)
		}
	} else {
		m.Submenu(tr(nil, "传输到窗口"), func(m *ui.Menu) {
			for _, destination := range a.fileLayout.panes() {
				if destination == p {
					continue
				}
				valid := validDrop(endpoint, destination.endpoint) && len(files) > 0
				for _, file := range files {
					valid = valid && !file.Directory
				}
				label := a.filePaneLabel(destination) + " · " + a.endpointName(destination.endpoint) + " · " + destination.browser().dir
				if m.Item(reviewText(label)).Disabled(!valid).Chosen() {
					a.transferEntries(endpoint, dir, files, destination.endpoint, destination.browser().dir)
				}
			}
		})
	}
	m.Separator()
	a.fileMutationMenu(m, source, dir, files, false)
	m.Separator()
	a.directoryActions(m, source, dir)
	m.Separator()
	a.fileFollowMenu(m, p)
}

func (a *app) fileMutationMenu(m *ui.Menu, source *filePane, dir string, files []sshconn.File, root bool) {
	if m.Item(tr(nil, "重命名 / 移动")).Disabled(root || len(files) != 1).Chosen() {
		a.fileOperationAt(source, dir, files, "rename")
	}
	if m.Item(tr(nil, "权限 / 属性")).Disabled(len(files) != 1).Chosen() {
		a.fileOperationAt(source, dir, files, "chmod")
	}
	if m.Item(tr(nil, "删除")).Disabled(root || len(files) != 1).Chosen() {
		a.fileOperationAt(source, dir, files, "remove")
	}
}

func (a *app) treeContextMenu(m *ui.Menu, p *filePane, b *directoryBrowser, dir string) {
	endpoint := p.endpoint
	source := &filePane{endpoint: endpoint, states: map[string]*directoryBrowser{endpoint: b}}
	local := endpoint == localEndpoint
	parent := parentDir(local, dir)
	if m.Item(tr(nil, "打开文件夹")).Chosen() {
		a.navigateDirectory(source, dir)
	}
	m.Separator()
	if parent == dir {
		if m.Item(tr(nil, "权限 / 属性")).Chosen() {
			a.openFileProperties(source, dir)
		}
		m.Item(tr(nil, "重命名 / 移动")).Disabled(true)
		m.Item(tr(nil, "删除")).Disabled(true)
	} else {
		a.fileMutationMenu(m, source, parent, []sshconn.File{{Name: baseName(local, dir), Directory: true}}, false)
	}
	m.Separator()
	a.directoryActions(m, source, dir)
	m.Separator()
	a.fileFollowMenu(m, p)
}

func (a *app) openEntries(p *filePane, dir string, files []sshconn.File) {
	if len(files) != 1 {
		return
	}
	if files[0].Directory {
		a.navigateDirectory(p, joinPath(p.endpoint == localEndpoint, dir, files[0].Name))
		return
	}
	name, endpoint := joinPath(p.endpoint == localEndpoint, dir, files[0].Name), p.endpoint
	open := func(string) { a.readEditor(endpoint, name) }
	if a.editPath != "" && a.edit.Buffer.Modified() {
		a.ask(tr(nil, "打开另一个文件？"), a.editPath, tr(nil, "当前修改尚未保存。继续会放弃这些修改。"), "", "", tr(nil, "放弃并打开"), open)
	} else {
		open("")
	}
}

func (a *app) directoryContextMenu(m *ui.Menu, p *filePane, b *directoryBrowser, dir string) {
	if m.Item(tr(nil, "打开文件夹")).Chosen() {
		a.navigateDirectory(p, dir)
	}
	a.directoryActions(m, p, dir)
	m.Separator()
	a.fileFollowMenu(m, p)
}

func (a *app) fileFollowMenu(m *ui.Menu, p *filePane) {
	if m.Item(tr(nil, "跟随当前终端")).Checked(p.follow).Chosen() {
		if p.follow {
			p.follow = false
		} else {
			a.chooseFileEndpoint(p, followTerminalEndpoint)
		}
	}
}

func (a *app) directoryActions(m *ui.Menu, p *filePane, dir string) {
	if m.Item(tr(nil, "新建文件夹")).Disabled(p.endpoint == "").Chosen() {
		a.fileOperationAt(p, dir, nil, "mkdir")
	}
	if m.Item(tr(nil, "新建文本文件")).Disabled(p.endpoint == "").Chosen() {
		a.fileOperationAt(p, dir, nil, "new")
	}
	m.Separator()
	if m.Item(tr(nil, "刷新")).Disabled(p.endpoint == "").Chosen() {
		a.loadDirectory(p, dir, dir == p.browser().dir)
	}
	if m.Item(tr(nil, "上一级")).Disabled(parentDir(p.endpoint == localEndpoint, dir) == dir).Chosen() {
		a.navigateDirectory(p, parentDir(p.endpoint == localEndpoint, dir))
	}
}

type fileDrag struct {
	endpoint, dir string
	file          sshconn.File
	browser       *directoryBrowser
}

func (d fileDrag) entries() []sshconn.File {
	if d.browser != nil && d.browser.dir == d.dir && d.browser.chosen.Has(d.file.Name) {
		return chosenFiles(d.browser)
	}
	return []sshconn.File{d.file}
}

func (a *app) fileDropTarget(c *ui.Context, e ui.Element, p *filePane, dir string) {
	if d, ok := ui.DragOver[fileDrag](e); ok && validDrop(d.endpoint, p.endpoint) {
		e.Background(c.Theme().Accent.Alpha(0.14))
	}
	if d, ok := ui.Drop[fileDrag](e); ok && validDrop(d.endpoint, p.endpoint) {
		a.transferEntries(d.endpoint, d.dir, d.entries(), p.endpoint, dir)
	}
}

func validDrop(from, to string) bool {
	return from != "" && to != "" && (from == localEndpoint) != (to == localEndpoint)
}

func (a *app) cancelDirectoryLoads() {
	for _, p := range a.allFilePanes() {
		cancelPaneLoads(p)
	}
}

func cancelPaneLoads(p *filePane) {
	if p == nil {
		return
	}
	for _, b := range p.states {
		for _, request := range b.loading {
			request.cancel()
		}
		clear(b.loading)
	}
}
