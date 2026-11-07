package desktop

import (
	"fmt"
	"slices"
	"strings"

	"github.com/egoist/mygo/ui"
	"github.com/egoist/mygo/ui/editor"
)

// A tool has one presentation owner. Its source is captured when promoted;
// changing focus or dragging it must not silently retarget an operation.
type workspaceToolTab struct {
	kind   int
	group  *terminalGroup
	source *session
}

type workspaceTab struct {
	session  *session
	document *editor.State
	tool     *workspaceToolTab
}

func (a *app) workspaceTabs(g *terminalGroup) []workspaceTab {
	if g == nil || g.split != nil {
		return nil
	}
	wanted := make([]workspaceTab, 0, len(g.sessions)+len(g.tools)+1)
	for _, s := range g.sessions {
		wanted = append(wanted, workspaceTab{session: s})
	}
	if a.editorTerminalGroup == g && a.editPath != "" {
		wanted = append(wanted, workspaceTab{document: a.edit})
	}
	for _, tool := range g.tools {
		wanted = append(wanted, workspaceTab{tool: tool})
	}
	// Membership belongs to the session/document/tool owners. This slice stores
	// only their interleaved order, dropping stale references on reconciliation.
	order := g.tabOrder[:0]
	for _, tab := range g.tabOrder {
		if slices.Contains(wanted, tab) && !slices.Contains(order, tab) {
			order = append(order, tab)
		}
	}
	for _, tab := range wanted {
		if !slices.Contains(order, tab) {
			order = append(order, tab)
		}
	}
	g.tabOrder = order
	return order
}

func (a *app) workspaceTabTitle(c *ui.Context, tab workspaceTab) string {
	if tab.session != nil {
		return sessionDisplayName(c, tab.session)
	}
	if tab.tool != nil {
		return workspaceToolTitle(c, tab.tool.kind)
	}
	name := a.editPath
	if i := strings.LastIndexAny(name, "/\\"); i >= 0 {
		name = name[i+1:]
	}
	if a.edit != nil && a.edit.Buffer.Modified() {
		name += " ·"
	}
	return tr(c, "编辑: ") + name
}

func workspaceToolTitle(c *ui.Context, kind int) string {
	switch kind {
	case toolCommands:
		return tr(c, "命令")
	case toolDiagnostics:
		return tr(c, "服务器状态")
	default:
		return tr(c, "文件")
	}
}

func (a *app) workspaceTabDisplayTitle(c *ui.Context, tab workspaceTab) string {
	label := a.workspaceTabTitle(c, tab)
	if tab.tool != nil && tab.tool.kind != toolFiles && tab.tool.source != nil {
		label += " · " + sessionDisplayName(c, tab.tool.source)
	}
	return label
}

func (a *app) selectedWorkspaceTab(g *terminalGroup) workspaceTab {
	if g == nil {
		return workspaceTab{}
	}
	if g.selectedTool != nil {
		return workspaceTab{tool: g.selectedTool}
	}
	if g.editorSelected && a.editorTerminalGroup == g && a.editPath != "" {
		return workspaceTab{document: a.edit}
	}
	return workspaceTab{session: g.active}
}

func (a *app) workspaceTerminalSelected(s *session) bool {
	g := a.activeTerminalGroup
	return g != nil && g.selectedTool == nil && !g.editorSelected && g.active == s && a.currentTerminal() == s
}

func (a *app) activateWorkspaceTab(g *terminalGroup, tab workspaceTab) {
	if a.fileExpanded {
		a.fileExpanded = false
		a.workspaceRevision++
	}
	if tab.session != nil {
		a.activateTerminal(tab.session, true)
		return
	}
	if tab.document != nil {
		a.activateGroupEditor(g)
		return
	}
	if tab.tool == nil || tab.tool.group != g {
		return
	}
	if g.selectedTool != tab.tool {
		a.workspaceRevision++
	}
	g.selectedTool, g.editorSelected = tab.tool, false
	a.activeTerminalGroup, a.editShown, a.contentFocus = g, false, false
	if tab.tool.source == nil {
		tab.tool.source = a.currentTerminal()
	}
	if index := a.sessionIndex(tab.tool.source); index >= 0 {
		a.active, a.lastActiveTerminal = index, tab.tool.source
	}
	a.syncWorkspaceFileFlags()
}

func (a *app) syncWorkspaceFileFlags() {
	file := a.workspaceTools[toolFiles]
	a.filesTabOpen = file != nil
	a.filesTabSelected = file != nil && a.activeTerminalGroup == file.group && file.group.selectedTool == file
}

// The connection and lock adapters still expose these two flags. Import their
// explicit selection changes here; all rendering uses the shared group model.
func (a *app) reconcileWorkspaceTools() {
	if a.workspaceTools == nil {
		a.workspaceTools = make(map[int]*workspaceToolTab)
	}
	file := a.workspaceTools[toolFiles]
	if a.filesTabOpen && file == nil {
		file = &workspaceToolTab{kind: toolFiles, group: a.activeTerminalGroup, source: a.currentTerminal()}
		a.workspaceTools[toolFiles] = file
		file.group.tools = append(file.group.tools, file)
	}
	if !a.filesTabOpen && file != nil {
		file.group.tools = slices.DeleteFunc(file.group.tools, func(t *workspaceToolTab) bool { return t == file })
		if file.group.selectedTool == file {
			file.group.selectedTool = nil
		}
		delete(a.workspaceTools, toolFiles)
		file = nil
	}
	if file != nil && a.filesTabSelected {
		a.activeTerminalGroup, file.group.selectedTool = file.group, file
	} else if file != nil && a.activeTerminalGroup == file.group && file.group.selectedTool == file {
		file.group.selectedTool = nil
	}
}

func (a *app) openWorkspaceTool(kind int) {
	if !a.online || kind == toolTransfers {
		return
	}
	if a.deferTerminalMutation(func() { a.openWorkspaceTool(kind) }) {
		return
	}
	if a.toolsDetached {
		a.setToolsDockSide(a.savedToolsDockSide())
	}
	a.ensureTerminalGroups()
	if a.workspaceTools == nil {
		a.workspaceTools = make(map[int]*workspaceToolTab)
	}
	tool := a.workspaceTools[kind]
	if tool == nil {
		tool = &workspaceToolTab{kind: kind, group: a.activeTerminalGroup, source: a.currentTerminal()}
		a.workspaceTools[kind] = tool
		tool.group.tools = append(tool.group.tools, tool)
	}
	a.ensureFileLayout()
	a.resetToolPresentation()
	a.activateWorkspaceTab(tool.group, workspaceTab{tool: tool})
	a.page = 1
	a.workspaceRevision++
	a.syncWorkspaceFileFlags()
	a.invalidateWindows()
}

func (a *app) closeWorkspaceTab(tab workspaceTab) {
	if a.deferTerminalMutation(func() { a.closeWorkspaceTab(tab) }) {
		return
	}
	if tab.session != nil {
		a.closeSessionAt(a.sessionIndex(tab.session))
		return
	}
	if tab.document != nil {
		a.closeEditor()
		return
	}
	tool := tab.tool
	if tool == nil || a.workspaceTools[tool.kind] != tool {
		return
	}
	tool.group.tools = slices.DeleteFunc(tool.group.tools, func(t *workspaceToolTab) bool { return t == tool })
	if tool.group.selectedTool == tool {
		tool.group.selectedTool = nil
	}
	delete(a.workspaceTools, tool.kind)
	if tool.kind == toolFiles {
		a.filesTabOpen, a.filesTabSelected = false, false
	}
	a.workspaceRevision++
	a.ensureTerminalGroups()
	a.syncWorkspaceFileFlags()
	a.contentFocus = a.activeTerminalGroup.selectedTool == nil
	a.invalidateWindows()
}

func (a *app) dockWorkspaceTool(tool *workspaceToolTab) {
	if tool == nil {
		return
	}
	if a.deferTerminalMutation(func() { a.dockWorkspaceTool(tool) }) {
		return
	}
	kind := tool.kind
	a.closeWorkspaceTab(workspaceTab{tool: tool})
	a.showTool(kind)
}

func (a *app) workspaceTabMenu(m *ui.Menu, tab workspaceTab) {
	if !a.online || !a.validTerminalDrag(a.workspaceTabPayload(tab)) {
		return
	}
	if tab.session != nil {
		a.terminalTabMenu(m, tab.session)
	} else if tab.tool != nil {
		if m.Item(tr(nil, "返回停靠面板")).Chosen() {
			a.dockWorkspaceTool(tab.tool)
		}
		m.Separator()
	}
	if tab.session == nil && m.Item(tr(nil, "关闭此标签")).Chosen() {
		a.closeWorkspaceTab(tab)
	}
	m.Separator()
	g := a.editorTerminalGroup
	if tab.session != nil {
		g = tab.session.group
	}
	if tab.tool != nil {
		g = tab.tool.group
	}
	for _, item := range []struct{ label, edge string }{{"向左拆分", "left"}, {"向右拆分", "right"}, {"向上拆分", "top"}, {"向下拆分", "bottom"}} {
		if m.Item(tr(nil, item.label)).Disabled(g == nil || len(a.workspaceTabs(g)) < 2).Chosen() {
			a.dropWorkspaceTab(a.workspaceTabPayload(tab), g, workspaceTab{}, item.edge)
		}
	}
}

func (a *app) renderWorkspaceTool(c *ui.Context, tool *workspaceToolTab) {
	previous := a.renderingWorkspaceTool
	previousBounds := a.workspaceToolBounds
	a.renderingWorkspaceTool = tool
	defer func() { a.renderingWorkspaceTool, a.workspaceToolBounds = previous, previousBounds }()
	body := ui.Column(c).Fill().MinWidth(0).MinHeight(0)
	body.Children(func() {
		a.workspaceToolBounds = body.Bounds()
		switch tool.kind {
		case toolCommands:
			a.commandsView(c)
		case toolDiagnostics:
			a.diagnosticsView(c)
		default:
			a.ensureFileLayout()
			a.syncFollowingFilePanes()
			a.fileWindows(c, a.fileLayout)
		}
	})
}

func (a *app) workspaceToolSource(tool *workspaceToolTab) *session {
	if tool != nil && a.hasSession(tool.source) {
		return tool.source
	}
	return nil
}

// Native menus execute after the rendering scope has ended. Keep the originating
// tool and generation attached to that callback, even if focus changes meanwhile.
func (a *app) workspaceBoundMenu(build func(*ui.Menu)) func(*ui.Menu) {
	tool, generation := a.renderingWorkspaceTool, a.generation
	if tool == nil {
		return build
	}
	source := tool.source
	return func(menu *ui.Menu) {
		if !a.online || a.generation != generation || a.workspaceTools[tool.kind] != tool || tool.source != source {
			return
		}
		previous := a.renderingWorkspaceTool
		a.renderingWorkspaceTool = tool
		defer func() { a.renderingWorkspaceTool = previous }()
		build(menu)
	}
}

func (tab workspaceTab) key() string {
	if tab.session != nil {
		return fmt.Sprintf("terminal-tab:%p", tab.session)
	}
	if tab.tool != nil {
		return fmt.Sprintf("tool-tab:%d", tab.tool.kind)
	}
	return "editor-tab"
}

func layoutGroups(n *terminalLayoutNode) []*terminalGroup {
	if n == nil {
		return nil
	}
	var out []*terminalGroup
	var visit func(*terminalGroup)
	visit = func(g *terminalGroup) {
		if g == nil {
			return
		}
		if g.split != nil {
			visit(g.split.left)
			visit(g.split.right)
		} else {
			out = append(out, g)
		}
	}
	visit(n.left)
	visit(n.right)
	return out
}

func (a *app) pruneWorkspaceGroup(g *terminalGroup) *terminalGroup {
	if g == nil {
		return nil
	}
	if g.split == nil {
		if len(g.sessions) == 0 && len(g.tools) == 0 && a.editorTerminalGroup != g {
			return nil
		}
		return g
	}
	g.split.left, g.split.right = a.pruneWorkspaceGroup(g.split.left), a.pruneWorkspaceGroup(g.split.right)
	if g.split.left == nil {
		return g.split.right
	}
	if g.split.right == nil {
		return g.split.left
	}
	return g
}

func (a *app) normalizeWorkspaceTree() {
	if a.terminalLayout == nil {
		return
	}
	n := a.terminalLayout
	n.left, n.right = a.pruneWorkspaceGroup(n.left), a.pruneWorkspaceGroup(n.right)
	var remaining *terminalGroup
	if n.left == nil {
		remaining = n.right
	} else if n.right == nil {
		remaining = n.left
	} else {
		return
	}
	if remaining == nil {
		remaining = a.singleTerminalGroup
		if remaining == nil || remaining.split != nil {
			remaining = a.newTerminalGroup()
		}
	}
	if remaining.split != nil {
		a.terminalLayout = remaining.split
	} else {
		a.singleTerminalGroup, a.terminalLayout = remaining, nil
	}
}

// splitWorkspaceGroup inserts a branch around a leaf without mutating the leaf.
func (a *app) splitWorkspaceGroup(target *terminalGroup, edge string) *terminalGroup {
	if !slices.Contains(a.terminalGroups(), target) {
		return nil
	}
	added := a.newTerminalGroup()
	n := &terminalLayoutNode{left: target, right: added, share: 50, vertical: edge == "top" || edge == "bottom"}
	if edge == "left" || edge == "top" {
		n.left, n.right = added, target
	}
	if a.terminalLayout == nil {
		a.terminalLayout = n
	} else {
		var replace func(*terminalLayoutNode) bool
		replace = func(parent *terminalLayoutNode) bool {
			for _, child := range []**terminalGroup{&parent.left, &parent.right} {
				if *child == target {
					*child = &terminalGroup{split: n}
					return true
				}
				if (*child).split != nil && replace((*child).split) {
					return true
				}
			}
			return false
		}
		replace(a.terminalLayout)
	}
	a.workspaceRevision++
	return added
}
