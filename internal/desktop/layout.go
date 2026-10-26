package desktop

import (
	"fmt"
	"sshclient/internal/desktop/components"
	"strings"

	"github.com/egoist/mygo/ui"
	"github.com/egoist/mygo/ui/editor"
	"sshclient/internal/broker"
	"sshclient/internal/vault"
)

func (a *app) deleteConnection(d vault.Summary) {
	a.ask(tr(nil, "删除连接？"), d.Name+" · "+d.User+"@"+d.Host, tr(nil, "删除本机连接资料及已保存凭据，撤销现有授权。该连接的编辑标签会关闭，未保存内容会丢失；服务器上的文件保留。"), "", "", tr(nil, "删除连接"), func(value string) {
		owner, err := a.unlockedOwner()
		if err != nil {
			a.status = err.Error()
			return
		}
		cli, gen, ctx := a.client, a.generation, a.operationContext()
		a.work(func() (func(), error) {
			e := cli.DeleteProfile(ctx, d.ID, owner)
			return func() {
				if gen != a.generation {
					return
				}
				if cancel := a.connecting[d.ID]; cancel != nil {
					cancel()
				}
				a.connections = filterConnections(a.connections, d.ID)
				var active *session
				if a.active >= 0 && a.active < len(a.sessions) {
					active = a.sessions[a.active]
				}
				kept := []*session{}
				for _, s := range a.sessions {
					if s.host == d.ID {
						if s.term != nil {
							s.term.Close()
						}
					} else {
						kept = append(kept, s)
					}
				}
				a.sessions = kept
				a.active = max(0, min(a.active, len(kept)-1))
				for i, s := range kept {
					if s == active {
						a.active = i
						break
					}
				}
				if a.editHost == d.ID {
					a.editPath = ""
					a.editHash = ""
					a.editHost = ""
					a.editShown = false
					a.edit = &editor.State{Buffer: editor.NewBuffer("")}
				}
				if a.selected == d.ID {
					a.selected = ""
					if len(kept) > 0 {
						a.selected = kept[a.active].host
					}
				}
				if a.details != nil && a.details.ID == d.ID {
					a.cancelConnectionEditor()
					a.details = nil
				}
				for _, pane := range a.allFilePanes() {
					if pane != nil {
						cancelPaneLoads(pane)
						delete(pane.states, d.ID)
						if pane.endpoint == d.ID {
							pane.endpoint = localEndpoint
						}
					}
				}
				a.syncFollowingFilePanes()
				a.status = tr(nil, "本机连接已删除")
			}, e
		})
	})
	a.prompt.secret = false
	a.prompt.danger = true
}
func filterConnections(values []vault.Summary, id string) []vault.Summary {
	out := make([]vault.Summary, 0, len(values))
	for _, d := range values {
		if d.ID != id {
			out = append(out, d)
		}
	}
	return out
}

type promptState struct {
	docker                                       *dockerDialogState
	properties                                   *filePropertiesState
	open                                         bool
	detached                                     bool
	title, target, message, value, field, accept string
	secret, multiline                            bool
	danger                                       bool
	error                                        string
	generation                                   int
	validate                                     func(string) error
	confirm                                      func(string)
}

func (a *app) ask(title, target, message, field, value, accept string, fn func(string)) {
	a.cancelFileProperties()
	a.cancelDockerDialog()
	a.prompt = promptState{open: true, detached: a.toolsWindowFocused(), title: title, target: target, message: message, field: field, value: value, accept: accept, confirm: fn, generation: a.generation}
	a.invalidateWindows()
}

func (a *app) promptView(c *ui.Context) {
	if a.prompt.docker != nil {
		a.dockerDialogView(c)
		return
	}
	if a.prompt.properties != nil {
		a.filePropertiesView(c)
		return
	}
	submit := false
	components.Dialog(c, &a.prompt.open, components.DialogOptions{
		Title: a.prompt.title, Target: reviewText(a.prompt.target), Description: a.prompt.message,
	}, func() {
		if a.prompt.field != "" {
			ui.Field(c, a.prompt.field, func() {
				if a.prompt.multiline {
					ui.TextArea(c, &a.prompt.value).Height(140).FillWidth().Label(a.prompt.field)
				} else {
					input := ui.TextInput(c, &a.prompt.value).FillWidth().Height(components.FormHeight).Label(a.prompt.field).AutoFocus()
					if a.prompt.secret {
						input.Password()
					}
					if input.Changed() {
						a.prompt.error = ""
					}
					submit = input.Submitted()
				}
			}).Error(a.prompt.error)
		}
	}, func() {
		if components.Button(c, tr(c, "取消")).Height(components.FormHeight).Clicked() {
			a.prompt = promptState{}
			return
		}
		variant := components.Primary
		if a.prompt.danger {
			variant = components.Destructive
		}
		button := components.Button(c, a.prompt.accept, components.ButtonOptions{Variant: variant, Disabled: a.busy}).Height(components.FormHeight)
		if button.Clicked() || submit && !a.busy {
			if a.prompt.generation != a.generation || !a.online {
				a.prompt = promptState{}
				return
			}
			if a.prompt.validate != nil {
				if err := a.prompt.validate(a.prompt.value); err != nil {
					a.prompt.error = err.Error()
					return
				}
			}
			fn, value := a.prompt.confirm, a.prompt.value
			a.prompt = promptState{}
			if fn != nil {
				fn(value)
			}
		}
	})
	if !a.prompt.open {
		a.prompt = promptState{}
	}
}

func (a *app) workspaceView(c *ui.Context) {
	a.layoutRendered = a.currentWorkspaceLayout()
	page := a.workspaceNavigationPage()
	if page != 1 {
		a.workspaceContent(c, page)
		return
	}
	a.ensurePanes()
	c.Key("workbench-navigation-split")
	components.SidebarSplit(c, &a.sidebarWidth, a.sidebarHidden, func() {
		a.connectionSidebar(c, a.sidebarHidden)
	}, func() {
		a.workspaceContent(c, page)
	})
}

type workspaceLayout struct {
	sidebarHidden, toolsHidden, toolsRight, expanded, detached bool
	terminals                                                  *terminalLayoutNode
}

func (a *app) currentWorkspaceLayout() workspaceLayout {
	return workspaceLayout{a.sidebarHidden, a.toolsHidden, a.toolsRight, a.fileExpanded, a.toolsDetached, a.terminalLayout}
}

func (a *app) contentLayoutReady() bool { return a.layoutRendered == a.currentWorkspaceLayout() }

func (a *app) setToolsHidden(hidden bool) {
	if a.toolsDetached {
		if !hidden {
			a.detachTools()
			return
		}
		a.closeToolsWindow()
		a.transfersOpen = false
		a.moveToolDialogsToMain()
	}
	a.toolsHidden, a.fileExpanded = hidden, false
	a.toolsLayoutOpen = false
	if a.page == 1 {
		a.contentFocus = true
	}
}

func (a *app) setToolsDock(right bool) {
	if a.toolsDetached {
		a.closeToolsWindow()
		a.toolsDetached = false
		a.resetToolPresentation()
		a.moveToolDialogsToMain()
	}
	a.toolsRight, a.toolsHidden, a.fileExpanded = right, false, false
	a.toolsLayoutOpen = false
	a.contentFocus = true
}

func (a *app) toolsToggle(c *ui.Context) {
	c.Key("workspace-tools-toggle")
	label := tr(c, "隐藏工具面板")
	if a.toolsHidden {
		label = tr(c, "显示工具面板")
	}
	icon := components.BottomPanelIcon
	if a.toolsDockRight(c) {
		icon = components.RightPanelIcon
	}
	if a.toolsDetached {
		icon = components.DetachPanelIcon
	}
	if components.SVGButton(c, icon, label).Tooltip(label + " · Cmd/Ctrl+Shift+J").Clicked() {
		a.setToolsHidden(!a.toolsHidden)
	}
}

func (a *app) sidebarToggle(c *ui.Context) {
	c.Key("workspace-sidebar-toggle")
	page := a.workspaceNavigationPage()
	label := a.pageSidebarLabel(page)
	if components.SVGButton(c, components.SidebarIcon, label).Clicked() {
		a.sidebarHidden = !a.sidebarHidden
		if a.page == 1 {
			a.contentFocus = true
		}
	}
}

func (a *app) pageSidebarLabel(page int) string {
	name := tr(nil, "连接侧栏")
	if page == 3 {
		name = tr(nil, "授权分类")
	} else if page == 4 {
		name = tr(nil, "设置分类")
	}
	if a.sidebarHidden {
		return fmt.Sprintf(tr(nil, "展开%s"), name)
	}
	return fmt.Sprintf(tr(nil, "收窄%s"), name)
}

func (a *app) workspaceContent(c *ui.Context, page int) {
	content := ui.Column(c).Key(page).Label(tr(c, "主内容")).FillWidth().Grow(1).MinHeight(0).MinWidth(0)
	content.Children(func() {
		switch page {
		case 3:
			a.approvalView(c)
		case 4:
			a.settingsView(c)
		default:
			a.terminalWorkspace(c, content.Bounds().W, content.Bounds().H)
		}
	})
}

func (a *app) terminalWorkspace(c *ui.Context, width, height float32) {
	a.syncFollowingFilePanes()
	if a.toolsDetached {
		a.sessionStage(c)
		return
	}
	if a.fileExpanded && !a.toolsHidden {
		a.fileArea(c)
		return
	}
	if a.toolsHidden {
		a.sessionStage(c)
		return
	}
	if a.toolsDockRight(c) {
		// Before the first layout, keep a previously fitted share until the
		// workspace width is known instead of imposing a different minimum.
		limits := components.SplitLimits{Min: 1, Max: 80, Default: 65}
		// The native splitter clamps only to 40 px. Keep the dock controls
		// usable when the window or the connection sidebar changes size.
		if width > 1 {
			room := width - 1
			minimum := min(50, 300*100/room)
			maximum := max(minimum, (room-340)*100/room)
			limits.Min, limits.Max = minimum, min(80, maximum)
		}
		components.SplitPercent(c, &a.terminalWidthShare, limits, func() { ui.Column(c).Fill().Children(func() { a.sessionStage(c) }) }, func() { ui.Column(c).Fill().Children(func() { a.fileArea(c) }) }).Fill()
	} else {
		limits := components.SplitLimits{Min: 30, Max: 90, Default: 72}
		if height > 1 {
			room := height - 1
			limits.Min = max(30, min(50, 160*100/room))
			limits.Max = max(limits.Min, min(90, (room-160)*100/room))
		}
		components.SplitVerticalPercent(c, &a.terminalShare, limits, func() { ui.Column(c).Fill().Children(func() { a.sessionStage(c) }) }, func() { a.fileArea(c) }).Fill()
	}
}

// A narrow main window cannot fit a useful terminal and a right-hand tool pane.
// Fall back to bottom docking for this frame without changing the saved choice.
func (a *app) toolsDockRight(c *ui.Context) bool {
	w, _ := c.Size()
	if a.sidebarHidden {
		w -= components.RailWidth
	} else {
		width := a.sidebarWidth
		if width <= 0 {
			width = 190
		}
		w -= width
	}
	return a.toolsRight && w >= 640
}

func (a *app) newConnection() {
	if a.connection.phase == "save" {
		return
	}
	page := a.page
	if page == 0 {
		page = a.connection.returnPage
	}
	a.resetConnectionEditor()
	a.connection.returnPage = page
	a.details = nil
	a.form = vault.Profile{ID: vault.ShortID(), Auth: "password", Port: 22}
	a.port = "22"
	a.ownerPass = ""
	a.page = 0
}
func (a *app) editConnection() {
	if a.connection.phase == "save" {
		return
	}
	page := a.page
	if page == 0 {
		page = a.connection.returnPage
	}
	a.resetConnectionEditor()
	a.connection.returnPage = page
	for _, d := range a.connections {
		if d.ID == a.selected {
			copy := d
			a.details = &copy
			a.form = vault.Profile{ID: d.ID, Name: d.Name, Group: d.Group, Host: d.Host, Port: d.Port, User: d.User, Fingerprint: d.Fingerprint, Auth: d.Auth, AgentFingerprint: d.AgentFingerprint, Policy: d.Policy}
			a.form.Policy.ProtectedPaths = append([]string(nil), d.Policy.ProtectedPaths...)
			a.port = fmt.Sprint(d.Port)
			a.ownerPass = ""
			a.connection.authLabel = authLabel(d.Auth)
			a.connection.trustedTarget = targetID(d.Host, d.Port)
			a.connection.connect = false
			a.page = 0
			return
		}
	}
	a.newConnection()
}
func (a *app) hostName(id string) string {
	for _, d := range a.connections {
		if d.ID == id {
			return d.Name + " · " + d.User + "@" + d.Host
		}
	}
	if id == "" {
		return tr(nil, "选择服务器")
	}
	return id
}

func (a *app) sessionStage(c *ui.Context) {
	stage := ui.Column(c).Fill().Label(tr(c, "终端区域"))
	stage.Children(func() { a.sessionStageContent(c, stage.Bounds().W) })
}
func (a *app) sessionStageContent(c *ui.Context, width float32) {
	a.ensureTerminalGroups()
	a.applyingTerminalFocus = a.contentFocus
	a.renderingTerminalGroups = true
	defer func() {
		a.renderingTerminalGroups = false
		a.renderingTerminalGroup = nil
		a.applyingTerminalFocus = false
		if pending := a.pendingTerminalMutation; pending != nil {
			a.pendingTerminalMutation = nil
			pending()
			c.AnimationFrame()
		}
	}()
	if layout := a.terminalLayout; layout != nil {
		if width > 1 {
			minimum := min(50, 220*100/(width-1))
			layout.share = max(minimum, min(100-minimum, layout.share))
		}
		ui.SplitPercent(c, &layout.share,
			func() { a.terminalGroupView(c, layout.left, true, false) },
			func() { a.terminalGroupView(c, layout.right, false, true) }).Fill()
	} else {
		a.terminalGroupView(c, a.singleTerminalGroup, true, true)
	}
}

func (a *app) sessionTerminal(c *ui.Context, s *session) {
	if s.term != nil {
		if s.closed {
			ui.Row(c).FillWidth().Padding(5, 10).Gap(10).AlignItems(ui.Center).Children(func() {
				ui.Text(c, tr(c, "连接已断开")).FontSize(12).TextColor(c.Theme().TextMuted).Grow(1)
				if components.Button(c, tr(c, "重新连接")).Disabled(a.connecting[s.host] != nil).Clicked() {
					a.reconnectTerminal(s)
				}
			})
		}
		a.nativeTerminal(c, s)
	} else {
		ui.Spacer(c).Fill()
	}
}
func (a *app) closeSession() { a.closeSessionAt(a.active) }
func (a *app) closeSessionAt(index int) {
	if index < 0 || index >= len(a.sessions) {
		return
	}
	s := a.sessions[index]
	if a.deferTerminalMutation(func() {
		a.closeSessionAt(a.sessionIndex(s))
	}) {
		return
	}
	a.detachServerStatus(s)
	s.closed = true
	if s.term != nil {
		s.term.Close()
	}
	if s.management != nil && s.management.containerDetails != nil {
		a.closeDockerDialog(s.management.containerDetails)
	}
	current := a.currentTerminal()
	a.removeTerminalFromGroups(s)
	a.sessions = append(a.sessions[:index], a.sessions[index+1:]...)
	a.restoreTerminalSelection(current)
	a.ensureTerminalGroups()
	a.syncFollowingFilePanes()
	a.contentFocus = true
}

func (a *app) closeEditor() {
	close := func(string) {
		a.editorSequence++
		if a.editorTerminalGroup != nil {
			a.editorTerminalGroup.editorSelected = false
		}
		a.editorTerminalGroup = nil
		a.editPath = ""
		a.editHash = ""
		a.editHost = ""
		a.editShown = false
		a.edit = &editor.State{Buffer: editor.NewBuffer("")}
		a.contentFocus = true
	}
	if a.edit != nil && a.edit.Buffer.Modified() {
		a.ask(tr(nil, "关闭未保存的文件？"), a.editPath, tr(nil, "放弃修改后关闭。需要保留修改时，请取消并先保存。"), "", "", tr(nil, "放弃并关闭"), close)
	} else {
		close("")
	}
}
func (a *app) editorStage(c *ui.Context) {
	ui.Row(c).FillWidth().Padding(5, 8).Gap(6).Children(func() {
		label := a.hostName(a.editHost)
		if a.editLocal {
			label = tr(c, "本机")
		}
		ui.Text(c, label+" · "+a.editPath).SingleLine().Grow(1).FontSize(12)
		components.ButtonGroup(c, func() {
			if components.Button(c, tr(c, "保存"), components.ButtonOptions{Variant: components.Primary, Disabled: a.busy || !a.edit.Buffer.Modified(), Busy: a.editorSaving == a.edit}).Clicked() {
				a.saveEditor()
			}
			if components.Button(c, tr(c, "另存为")).Clicked() {
				a.saveEditorAs()
			}
		}).Label(tr(c, "编辑操作"))
	})
	view := editor.View(c, a.edit).FillWidth().Grow(1).MinHeight(0).Label(tr(c, "文件编辑器"))
	if a.contentFocus && a.contentLayoutReady() && (a.renderingTerminalGroup == nil || a.renderingTerminalGroup == a.activeTerminalGroup) {
		view.Focus()
		a.contentFocus = false
	}
	if view.FocusWithin() && a.renderingTerminalGroup != nil && !a.applyingTerminalFocus && !a.contentFocus {
		a.activeTerminalGroup = a.renderingTerminalGroup
		a.editShown = true
	}
	if view.Shortcut(ui.Cmd, ui.KeyS) {
		a.saveEditor()
	}
}

func (a *app) confirmDraft(s *session) {
	body := s.draft
	a.ask(tr(nil, "执行常用命令"), a.hostName(s.host), body, "", "", tr(nil, "执行"), func(string) {
		for _, current := range a.sessions {
			if current == s && s.term != nil {
				select {
				case <-s.term.Done():
					a.status = tr(nil, "终端已断开，请重新连接")
					return
				default:
				}
				s.term.Send([]byte(strings.ReplaceAll(body, "\r\n", "\n") + "\r"))
				s.draft = ""
				a.status = tr(nil, "命令已发送到 ") + sessionDisplayName(nil, s)
				return
			}
		}
		a.status = tr(nil, "原终端已关闭，命令未发送")
	})
}

func (a *app) commandsView(c *ui.Context) {
	ui.Column(c).FillWidth().Grow(1).MinHeight(0).Padding(8).Gap(7).Children(func() {
		ui.Row(c).FillWidth().Gap(6).Children(func() {
			ui.TextInput(c, &a.snippetSearch).Grow(1).Label(tr(c, "搜索常用命令")).Placeholder(tr(c, "搜索命令"))
			if components.IconButton(c, "＋", tr(c, "新建常用命令")).Clicked() {
				a.snippetForm = vault.Snippet{ID: vault.ShortID(), Group: "日常维护"}
				a.snippetOpen = true
				a.snippetDetached = a.toolsWindowFocused()
				a.invalidateWindows()
			}
		})
		items := make([]vault.Snippet, 0, len(a.snippets))
		query := strings.ToLower(a.snippetSearch)
		for _, v := range a.snippets {
			if strings.Contains(strings.ToLower(v.Name+" "+v.Group+" "+v.Body), query) {
				items = append(items, v)
			}
		}
		a.snippetTable.Selected = &a.snippetSelected
		a.snippetTable.Key = func(i int) any { return items[i].ID }
		a.snippetTable.Label = func(i int) string { return items[i].Name }
		list := components.List(c, &a.snippetTable, len(items), func(i int) {
			v := items[i]
			row := ui.Column(c).FillWidth().Padding(6, 8).Gap(3).Children(func() {
				ui.Text(c, v.Name).FontSize(12).SingleLine()
				ui.Text(c, v.Body).FontSize(11).SingleLine().TextColor(c.Theme().TextMuted)
			})
			row.ContextMenu(func(m *ui.Menu) {
				if m.Item(tr(c, "填入终端")).Chosen() {
					a.fillSnippet(v)
				}
				if m.Item(tr(c, "编辑")).Chosen() {
					a.snippetForm = v
					a.snippetOpen = true
					a.snippetDetached = a.toolsWindowFocused()
					a.invalidateWindows()
				}
				if m.Item(tr(c, "删除")).Chosen() {
					a.ask(tr(c, "删除常用命令？"), v.Name, tr(c, "仅删除命令库中的这条记录。"), "", "", tr(c, "删除"), func(string) { a.persistSnippet(v, true) })
				}
			})
		}).FillWidth().Grow(1).MinHeight(0).Label(tr(c, "常用命令列表"))
		if list.Submitted() && a.snippetSelected >= 0 && a.snippetSelected < len(items) {
			a.fillSnippet(items[a.snippetSelected])
		}
		if len(items) == 0 {
			ui.Text(c, tr(c, "暂无常用命令")).FontSize(11).TextColor(c.Theme().TextMuted)
		}
		if a.active >= 0 && a.active < len(a.sessions) {
			s := a.sessions[a.active]
			if s.draft != "" {
				ui.Text(c, tr(c, "目标: ")+sessionDisplayName(c, s)).FontSize(11).TextColor(c.Theme().TextMuted)
				ui.Row(c).FillWidth().Gap(6).Children(func() {
					ui.TextInput(c, &s.draft).Label(tr(c, "待执行命令")).Grow(1)
					if components.Button(c, tr(c, "执行")).Disabled(s.term == nil).Clicked() {
						a.confirmDraft(s)
					}
				})
			}
		}
	})
}
func (a *app) activeTerminalName() string {
	if a.active >= 0 && a.active < len(a.sessions) {
		return sessionDisplayName(nil, a.sessions[a.active])
	}
	return tr(nil, "请先连接终端")
}
func (a *app) fillSnippet(v vault.Snippet) {
	if a.active < 0 || a.active >= len(a.sessions) {
		a.status = tr(nil, "请先打开终端")
		return
	}
	a.sessions[a.active].draft = v.Body
	a.editShown = false
	a.page = 1
	a.dock = toolCommands
	a.toolsHidden = false
	a.status = tr(nil, "已填入 ") + sessionDisplayName(nil, a.sessions[a.active]) + tr(nil, "，确认后点执行")
}
func (a *app) snippetDialog(c *ui.Context) {
	components.Dialog(c, &a.snippetOpen, components.DialogOptions{Title: tr(c, "保存常用命令"), Width: 550}, func() {
		ui.TextInput(c, &a.snippetForm.Name).Label(tr(c, "命令名称")).Placeholder(tr(c, "命令名称")).FillWidth().AutoFocus()
		ui.TextInput(c, &a.snippetForm.Group).Label(tr(c, "命令分组")).Placeholder(tr(c, "分组")).FillWidth()
		ui.TextArea(c, &a.snippetForm.Body).Label(tr(c, "命令内容")).Height(160).FillWidth()
	}, func() {
		if components.Button(c, tr(c, "取消")).Clicked() {
			a.snippetOpen = false
		}
		if components.Button(c, tr(c, "保存命令"), components.ButtonOptions{Variant: components.Primary, Busy: a.busy}).Clicked() {
			a.persistSnippet(a.snippetForm, false)
		}
	})
	if !a.snippetOpen {
		a.snippetForm = vault.Snippet{}
	}
}

func (a *app) persistSnippet(v vault.Snippet, remove bool) {
	server, gen := a.server, a.generation
	if server == nil {
		return
	}
	a.work(func() (func(), error) {
		e := server.Vault.SaveSnippet(broker.VaultPath(a.dir), v, remove)
		return func() {
			if gen != a.generation {
				return
			}
			a.snippets = server.Vault.Snippets()
			a.snippetOpen = false
			a.status = tr(nil, "常用命令已加密保存")
		}, e
	})
}
