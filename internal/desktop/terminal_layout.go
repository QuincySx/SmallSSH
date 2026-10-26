package desktop

import (
	"slices"
	"strings"

	"github.com/egoist/mygo/ui"
	"github.com/egoist/mygo/ui/editor"
	"sshclient/internal/desktop/components"
)

// Each session belongs to one editor-style group. The two groups retain their
// own selected tab; only the focused group drives shared tools and keyboard.
type terminalGroup struct {
	id             uint64
	sessions       []*session
	active         *session
	editorSelected bool
	headerWidth    float32
	revealedTab    *session
	revealedEditor bool
}

type terminalLayoutNode struct {
	left, right *terminalGroup
	share       float32
}

func (n *terminalLayoutNode) sessions() []*session {
	if n == nil {
		return nil
	}
	return append(append([]*session(nil), n.left.sessions...), n.right.sessions...)
}

func (a *app) currentTerminal() *session {
	if a.active >= 0 && a.active < len(a.sessions) {
		return a.sessions[a.active]
	}
	return nil
}

func (a *app) sessionIndex(s *session) int { return slices.Index(a.sessions, s) }

func (a *app) newTerminalGroup() *terminalGroup {
	a.terminalGroupSequence++
	return &terminalGroup{id: a.terminalGroupSequence, headerWidth: -1}
}

func (a *app) terminalGroups() []*terminalGroup {
	if a.terminalLayout != nil {
		return []*terminalGroup{a.terminalLayout.left, a.terminalLayout.right}
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
	if a.editPath == "" {
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
	if a.terminalLayout != nil {
		for _, g := range groups {
			if len(g.sessions) == 0 && a.editorTerminalGroup != g {
				other := groups[0]
				if other == g {
					other = groups[1]
				}
				a.singleTerminalGroup, a.terminalLayout = other, nil
				if a.activeTerminalGroup == g {
					a.activeTerminalGroup = other
				}
				break
			}
		}
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
		a.activeTerminalGroup, s.group.active = s.group, s
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
	return a.terminalLayout.right
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
		destination = a.terminalLayout.left
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
		if a.currentTerminal() == source && a.page == 1 && !a.editShown {
			a.ensureTerminalGroups()
			destination := a.rightTerminalGroup()
			if source.group == destination {
				destination = a.terminalLayout.left
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
	left, right := a.terminalLayout.left, a.terminalLayout.right
	left.sessions = append(left.sessions, right.sessions...)
	for _, s := range left.sessions {
		s.group = left
	}
	if a.editorTerminalGroup == right {
		a.editorTerminalGroup = left
	}
	left.editorSelected, left.active = a.editShown, a.currentTerminal()
	a.singleTerminalGroup, a.activeTerminalGroup, a.terminalLayout = left, left, nil
	a.contentFocus = true
}

func (a *app) terminalLayoutMenu(c *ui.Context) {
	components.SVGButton(c, components.SplitTerminalIcon, tr(c, "终端分栏")).Tooltip(tr(c, "左右分组与合并")).Menu(func(m *ui.Menu) {
		s := a.currentTerminal()
		if m.Item(tr(c, "新建终端到另一组")).Disabled(s == nil || s.closed || a.connecting[s.host] != nil).Chosen() {
			a.newSplitTerminal(s)
		}
		if m.Item(tr(c, "移动当前标签到另一组")).Disabled(s == nil || len(a.sessions) < 2).Chosen() {
			a.splitExistingTerminal(s)
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
	if a.terminalLayout != nil && s.group == a.terminalLayout.right {
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
		if a.currentTerminal() == source && a.page == 1 && !a.editShown {
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
	if group.active == old {
		group.active = added
	}
	added.group, added.name = group, old.name
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
		focused := a.currentTerminal() == source && a.page == 1 && !a.editShown
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
}

func (a *app) validTerminalDrag(d terminalTabDrag) bool {
	return d.owner == a && d.generation == a.generation && (d.session != nil && a.hasSession(d.session) || d.document != nil && d.document == a.edit && a.editPath != "")
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
	a.activeTerminalGroup, g.editorSelected, a.editShown = g, true, true
	a.contentFocus = true
}
