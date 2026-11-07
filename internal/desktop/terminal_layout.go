package desktop

import (
	"slices"
	"strings"

	"github.com/egoist/mygo/ui"
	"github.com/egoist/mygo/ui/editor"
	"sshclient/internal/desktop/components"
)

// Leaves own tabs. A branch owns only a split, so moving a view never replaces
// its SSH session, document or file browser.
type terminalGroup struct {
	id             uint64
	sessions       []*session
	active         *session
	editorSelected bool
	headerWidth    float32
	revealedTab    *session
	revealedEditor bool
	split          *terminalLayoutNode
	tools          []*workspaceToolTab
	selectedTool   *workspaceToolTab
	tabOrder       []workspaceTab
	revealedTool   *workspaceToolTab
}

type terminalLayoutNode struct {
	left, right *terminalGroup
	share       float32
	vertical    bool
}

func (n *terminalLayoutNode) sessions() []*session {
	if n == nil {
		return nil
	}
	var out []*session
	for _, g := range layoutGroups(n) {
		out = append(out, g.sessions...)
	}
	return out
}

func (a *app) currentTerminal() *session {
	if a.active >= 0 && a.active < len(a.sessions) {
		return a.sessions[a.active]
	}
	return nil
}

func (a *app) terminalFocusWanted() bool {
	return a.activeTerminalGroup == nil || a.activeTerminalGroup.selectedTool == nil && !a.activeTerminalGroup.editorSelected
}

func (a *app) sessionIndex(s *session) int { return slices.Index(a.sessions, s) }

func (a *app) newTerminalGroup() *terminalGroup {
	a.terminalGroupSequence++
	return &terminalGroup{id: a.terminalGroupSequence, headerWidth: -1}
}

func (a *app) terminalGroups() []*terminalGroup {
	if a.terminalLayout != nil {
		return layoutGroups(a.terminalLayout)
	}
	if a.singleTerminalGroup != nil {
		return []*terminalGroup{a.singleTerminalGroup}
	}
	return nil
}

func (a *app) ensureTerminalGroups() {
	if a.singleTerminalGroup == nil {
		a.singleTerminalGroup = a.newTerminalGroup()
	}
	groups := a.terminalGroups()
	if !slices.Contains(groups, a.activeTerminalGroup) {
		a.activeTerminalGroup = groups[0]
	}
	seen := make(map[*session]bool, len(a.sessions))
	for _, g := range groups {
		kept := g.sessions[:0]
		for _, s := range g.sessions {
			if a.hasSession(s) && !seen[s] {
				kept = append(kept, s)
				seen[s], s.group = true, g
			}
		}
		g.sessions = kept
	}
	for _, s := range a.sessions {
		if !seen[s] {
			g := s.group
			if !slices.Contains(groups, g) {
				g = a.activeTerminalGroup
			}
			g.sessions = append(g.sessions, s)
			s.group, seen[s] = g, true
		}
	}
	for _, g := range groups {
		if !slices.Contains(g.sessions, g.active) {
			g.active = nil
			if len(g.sessions) > 0 {
				g.active = g.sessions[0]
			}
		}
	}
	if a.editPath == "" || a.editorWin != nil || a.editorWindowPending {
		a.editorTerminalGroup = nil
		for _, g := range groups {
			g.editorSelected = false
		}
	} else if !slices.Contains(groups, a.editorTerminalGroup) {
		a.editorTerminalGroup = a.activeTerminalGroup
		a.editorTerminalGroup.editorSelected = a.editShown
	}
	// Connection completion and sidebar reuse can activate a tab directly.
	if current := a.currentTerminal(); current != nil && current != a.lastActiveTerminal {
		current.group.active = current
		if !a.editShown {
			current.group.editorSelected = false
			a.activeTerminalGroup = current.group
		}
	}
	a.reconcileWorkspaceTools()
	a.normalizeWorkspaceTree()
	if groups = a.terminalGroups(); !slices.Contains(groups, a.activeTerminalGroup) {
		a.activeTerminalGroup = groups[0]
	}
	a.lastActiveTerminal = a.currentTerminal()
}

// Commit UI mutations after both groups have rendered so a moving PTY cannot
// be built under both its old and new parent in the same render pass.
func (a *app) deferTerminalMutation(run func()) bool {
	if a.renderingTerminalGroups {
		a.pendingTerminalMutation = run
		return true
	}
	return false
}

func (a *app) activateTerminal(s *session, focus bool) {
	if index := a.sessionIndex(s); index >= 0 {
		a.ensureTerminalGroups()
		a.active = index
		if focus && a.fileExpanded {
			a.fileExpanded = false
			a.workspaceRevision++
		}
		a.activeTerminalGroup, s.group.active = s.group, s
		if s.group.selectedTool != nil {
			a.workspaceRevision++
		}
		s.group.selectedTool = nil
		a.filesTabSelected = false
		s.group.editorSelected, a.editShown = false, false
		a.lastActiveTerminal = s
		a.syncFollowingFilePanes()
		if focus {
			a.contentFocus = true
		}
	}
}

func (a *app) rightTerminalGroup() *terminalGroup {
	if a.terminalLayout == nil {
		a.terminalLayout = &terminalLayoutNode{left: a.singleTerminalGroup, right: a.newTerminalGroup(), share: 50}
	}
	groups := a.terminalGroups()
	return groups[len(groups)-1]
}

func (a *app) removeTerminalFromGroups(s *session) {
	if g := s.group; g != nil {
		if index := slices.Index(g.sessions, s); index >= 0 {
			g.sessions = append(g.sessions[:index], g.sessions[index+1:]...)
			if g.active == s {
				g.active = nil
				if len(g.sessions) > 0 {
					g.active = g.sessions[min(index, len(g.sessions)-1)]
				}
			}
		}
	}
}

func (a *app) restoreTerminalSelection(previous *session) {
	selected := previous
	if !a.hasSession(selected) {
		selected = nil
		if a.activeTerminalGroup != nil {
			selected = a.activeTerminalGroup.active
		}
		if !a.hasSession(selected) && len(a.sessions) > 0 {
			selected = a.sessions[0]
		}
	}
	a.active = max(0, a.sessionIndex(selected))
}

func (a *app) moveTerminalTab(s *session, destination *terminalGroup, before *session) {
	if a.deferTerminalMutation(func() { a.moveTerminalTab(s, destination, before) }) {
		return
	}
	// Do not collapse a newly created empty destination before the move.
	if !a.hasSession(s) || !slices.Contains(a.terminalGroups(), destination) {
		return
	}
	if s == before && s.group == destination {
		return
	}
	a.removeTerminalFromGroups(s)
	index := slices.Index(destination.sessions, before)
	if index < 0 {
		index = len(destination.sessions)
	}
	destination.sessions = slices.Insert(destination.sessions, index, s)
	tab := workspaceTab{session: s}
	order := slices.Clone(a.workspaceTabs(destination))
	order = slices.DeleteFunc(order, func(candidate workspaceTab) bool { return candidate == tab })
	position := slices.Index(order, workspaceTab{session: before})
	if position < 0 {
		position = len(order)
	}
	destination.tabOrder = slices.Insert(order, position, tab)
	s.group, destination.active = destination, s
	a.activateTerminal(s, true)
	a.ensureTerminalGroups()
}

func (a *app) splitExistingTerminal(s *session) {
	if a.deferTerminalMutation(func() { a.splitExistingTerminal(s) }) {
		return
	}
	a.ensureTerminalGroups()
	if !a.hasSession(s) || len(a.sessions) < 2 {
		return
	}
	destination := a.rightTerminalGroup()
	if s.group == destination {
		destination = a.terminalGroups()[0]
	}
	a.moveTerminalTab(s, destination, nil)
}

func (a *app) newSplitTerminal(source *session) {
	if a.deferTerminalMutation(func() { a.newSplitTerminal(source) }) {
		return
	}
	if source == nil || source.closed || !a.hasSession(source) {
		return
	}
	a.openShellForHostAfter(source.host, true, func(added *session) {
		if a.workspaceTerminalSelected(source) && a.page == 1 {
			a.ensureTerminalGroups()
			destination := a.rightTerminalGroup()
			if source.group == destination {
				destination = a.terminalGroups()[0]
			}
			a.moveTerminalTab(added, destination, nil)
		}
	})
}

func (a *app) mergeTerminalViews() {
	if a.deferTerminalMutation(a.mergeTerminalViews) {
		return
	}
	a.ensureTerminalGroups()
	if a.terminalLayout == nil {
		return
	}
	groups := a.terminalGroups()
	left := groups[0]
	selectedTool := a.activeTerminalGroup.selectedTool
	var order []workspaceTab
	for _, g := range groups {
		order = append(order, a.workspaceTabs(g)...)
		if g != left {
			left.sessions = append(left.sessions, g.sessions...)
			left.tools = append(left.tools, g.tools...)
		}
	}
	left.tabOrder, left.selectedTool = order, selectedTool
	for _, s := range left.sessions {
		s.group = left
	}
	for _, tool := range left.tools {
		tool.group = left
	}
	if a.editorTerminalGroup != nil {
		a.editorTerminalGroup = left
	}
	left.editorSelected, left.active = a.editShown, a.currentTerminal()
	a.singleTerminalGroup, a.activeTerminalGroup, a.terminalLayout = left, left, nil
	a.workspaceRevision++
	a.syncWorkspaceFileFlags()
	a.contentFocus = selectedTool == nil
}

func (a *app) terminalLayoutMenu(c *ui.Context) {
	s, g, generation := a.currentTerminal(), a.activeTerminalGroup, a.generation
	tab := a.selectedWorkspaceTab(g)
	components.SVGButton(c, components.SplitTerminalIcon, tr(c, "终端分栏")).Tooltip(tr(c, "拖动标签到四边拆分，或合并分组")).Menu(func(m *ui.Menu) {
		if !a.online || a.generation != generation {
			return
		}
		if m.Item(tr(c, "新建终端到另一组")).Disabled(s == nil || s.closed || a.connecting[s.host] != nil).Chosen() {
			a.newSplitTerminal(s)
		}
		count := 0
		for _, group := range a.terminalGroups() {
			count += len(a.workspaceTabs(group))
		}
		if m.Item(tr(c, "移动当前标签到另一组")).Disabled(count < 2).Chosen() {
			if len(a.terminalGroups()) == 1 {
				a.dropWorkspaceTab(a.workspaceTabPayload(tab), g, workspaceTab{}, "right")
			} else {
				groups := a.terminalGroups()
				target := groups[len(groups)-1]
				if target == g {
					target = groups[0]
				}
				a.dropWorkspaceTab(a.workspaceTabPayload(tab), target, workspaceTab{}, "")
			}
		}
		m.Separator()
		if m.Item(tr(c, "合并全部分组")).Disabled(a.terminalLayout == nil).Chosen() {
			a.mergeTerminalViews()
		}
	})
}

func (a *app) terminalTabMenu(m *ui.Menu, s *session) {
	if m.Item(tr(nil, "新建终端标签")).Disabled(s.closed).Chosen() {
		a.runMenu(func() { a.newTerminalTab(s) })
	}
	if m.Item(tr(nil, "关闭此标签")).Chosen() {
		a.closeSessionAt(a.sessionIndex(s))
	}
	m.Separator()
	label := tr(nil, "移到右侧组")
	if groups := a.terminalGroups(); len(groups) > 1 && s.group == groups[len(groups)-1] {
		label = tr(nil, "移到左侧组")
	}
	if m.Item(label).Disabled(len(a.sessions) < 2).Chosen() {
		a.splitExistingTerminal(s)
	}
}

// A tab's menu always targets the clicked session, even when the sidebar or
// another terminal group currently selects a different server.
func (a *app) newTerminalTab(source *session) {
	if !a.online || source == nil || source.closed || !a.hasSession(source) {
		return
	}
	a.activateTerminal(source, false)
	if source.target.Kind == "docker-terminal" {
		name := source.containerName
		if name == "" {
			name = strings.TrimPrefix(source.name, "容器 · ")
		}
		a.openContainerTerminal(source, source.target.Docker.Target, name, source.target.Docker.Shell, nil)
		return
	}
	a.openShellForHostAfter(source.host, true, func(added *session) {
		if a.workspaceTerminalSelected(source) && a.page == 1 {
			a.activateTerminal(added, true)
		}
	})
}

func (a *app) replaceClosedTerminal(old, added *session) {
	if old == nil || added == nil || old == added || !a.hasSession(old) || !a.hasSession(added) {
		return
	}
	a.ensureTerminalGroups()
	current, group := a.currentTerminal(), old.group
	a.removeTerminalFromGroups(added)
	for i, s := range group.sessions {
		if s == old {
			group.sessions[i] = added
		}
	}
	for i, tab := range group.tabOrder {
		if tab.session == old {
			group.tabOrder[i] = workspaceTab{session: added}
		}
	}
	if group.active == old {
		group.active = added
	}
	added.group, added.name = group, old.name
	for _, tool := range a.workspaceTools {
		if tool.source == old {
			tool.source = added
		}
	}
	a.detachServerStatus(old)
	old.closed = true
	if old.term != nil {
		_ = old.term.Close()
	}
	var kept []*session
	for _, s := range a.sessions {
		if s == old {
			kept = append(kept, added)
		} else if s != added {
			kept = append(kept, s)
		}
	}
	if current == old {
		current = added
	}
	a.sessions = kept
	a.active = max(0, a.sessionIndex(current))
	a.lastActiveTerminal = current
	a.syncFollowingFilePanes()
}

func (a *app) reconnectTerminal(source *session) {
	if source == nil || !source.closed || !a.hasSession(source) {
		return
	}
	if source.term == nil && source.connectionState != "" {
		a.retryPendingTerminal(source)
		return
	}
	if source.target.Kind == "docker-terminal" {
		name := source.containerName
		if name == "" {
			name = strings.TrimPrefix(source.name, "容器 · ")
		}
		a.openContainerTerminal(source, source.target.Docker.Target, name, source.target.Docker.Shell, source)
		return
	}
	a.activateTerminal(source, false)
	a.openShellForHostAfter(source.host, true, func(added *session) {
		focused := a.workspaceTerminalSelected(source) && a.page == 1
		a.replaceClosedTerminal(source, added)
		if focused {
			a.activateTerminal(added, true)
		}
	})
}

type terminalTabDrag struct {
	owner      *app
	generation int
	session    *session
	document   *editor.State
	tool       *workspaceToolTab
	fromDock   bool
	dockKind   int
	toolSource *session
}

func (a *app) validTerminalDrag(d terminalTabDrag) bool {
	if d.fromDock {
		kind := d.dockKind == toolFiles || d.dockKind == toolCommands || d.dockKind == toolDiagnostics
		return d.owner == a && d.generation == a.generation && kind && a.workspaceTools[d.dockKind] == nil &&
			d.session == nil && d.document == nil && d.tool == nil && (d.toolSource == nil || a.hasSession(d.toolSource))
	}
	count := 0
	for _, present := range []bool{d.session != nil, d.document != nil, d.tool != nil} {
		if present {
			count++
		}
	}
	if count != 1 {
		return false
	}
	return d.owner == a && d.generation == a.generation && (d.session != nil && a.hasSession(d.session) || d.document != nil && d.document == a.edit && a.editPath != "" || d.tool != nil && a.workspaceTools[d.tool.kind] == d.tool)
}

func (a *app) dropTerminalTab(d terminalTabDrag, destination *terminalGroup, before *session, createRight bool) {
	if a.deferTerminalMutation(func() { a.dropTerminalTab(d, destination, before, createRight) }) {
		return
	}
	if !a.validTerminalDrag(d) {
		return
	}
	if createRight {
		if len(a.sessions) < 2 && (d.document == nil || a.editPath == "") {
			return
		}
		destination = a.rightTerminalGroup()
	}
	if !slices.Contains(a.terminalGroups(), destination) {
		return
	}
	if d.session != nil {
		a.moveTerminalTab(d.session, destination, before)
	} else {
		if a.editorTerminalGroup != nil {
			a.editorTerminalGroup.editorSelected = false
		}
		a.editorTerminalGroup = destination
		a.activateGroupEditor(destination)
		a.ensureTerminalGroups()
	}
}

func (a *app) activateGroupEditor(g *terminalGroup) {
	g.selectedTool = nil
	a.filesTabSelected = false
	a.activeTerminalGroup, g.editorSelected, a.editShown = g, true, true
	a.contentFocus = true
}
