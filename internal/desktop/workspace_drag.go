package desktop

import (
	"slices"

	"github.com/egoist/mygo/ui"
)

func (a *app) workspaceTabPayload(tab workspaceTab) terminalTabDrag {
	return terminalTabDrag{owner: a, generation: a.generation, session: tab.session, document: tab.document, tool: tab.tool}
}

func (a *app) workspaceGroupDrop(c *ui.Context, panel ui.Element, g *terminalGroup, before workspaceTab, edges bool, groupBounds ...ui.Rect) {
	x, y, _ := panel.PointerPosition()
	edge := ""
	if edges {
		edge = workspaceDropEdge(x, y, panel.Bounds().W, panel.Bounds().H)
	}
	// A pane's tab strip sits on its top edge. Its child drop target must
	// forward that narrow edge gesture instead of consuming it as a reorder.
	if !edges && len(groupBounds) > 0 && y+panel.Bounds().Y < groupBounds[0].Y+8 {
		edge = "top"
	}
	if d, ok := ui.DragOver[terminalTabDrag](panel); ok && a.validTerminalDrag(d) {
		if edge == "" {
			panel.Border(1, c.Theme().Accent)
		} else {
			highlight := ui.Box(c).Absolute().Background(c.Theme().Accent.Alpha(.14))
			switch edge {
			case "left":
				highlight.Left(0).Top(0).Bottom(0).Width(panel.Bounds().W / 2)
			case "right":
				highlight.Right(0).Top(0).Bottom(0).Width(panel.Bounds().W / 2)
			case "top":
				highlight.Top(0).Left(0).Right(0).Height(panel.Bounds().H / 2)
			case "bottom":
				highlight.Bottom(0).Left(0).Right(0).Height(panel.Bounds().H / 2)
			}
			highlight.Label(tr(c, "分组放置区"))
		}
	}
	if d, ok := ui.Drop[terminalTabDrag](panel); ok {
		a.dropWorkspaceTab(d, g, before, edge)
	}
}

func workspaceDropEdge(x, y, width, height float32) string {
	if width <= 0 || height <= 0 {
		return ""
	}
	edge, distance := "", float32(.24)
	for _, candidate := range []struct {
		name string
		d    float32
	}{{"left", x / width}, {"right", (width - x) / width}, {"top", y / height}, {"bottom", (height - y) / height}} {
		if candidate.d < distance {
			edge, distance = candidate.name, candidate.d
		}
	}
	return edge
}

func (a *app) dropWorkspaceTab(d terminalTabDrag, destination *terminalGroup, before workspaceTab, edge string) {
	if a.deferTerminalMutation(func() { a.dropWorkspaceTab(d, destination, before, edge) }) {
		return
	}
	if !a.validTerminalDrag(d) || !slices.Contains(a.terminalGroups(), destination) {
		return
	}
	if d.fromDock {
		if edge != "" {
			a.setToolsDockSide(edge)
			a.showTool(d.dockKind)
			return
		}
		a.openWorkspaceTool(d.dockKind)
		d.tool = a.workspaceTools[d.dockKind]
		if d.tool == nil {
			return
		}
		d.tool.source = d.toolSource
	}
	tab := workspaceTab{session: d.session, document: d.document, tool: d.tool}
	if edge != "" {
		if len(a.workspaceTabs(destination)) == 1 && a.workspaceTabs(destination)[0] == tab {
			return
		}
		destination = a.splitWorkspaceGroup(destination, edge)
		if destination == nil {
			return
		}
		before = workspaceTab{}
	}
	if tab.session != nil {
		a.moveTerminalTab(tab.session, destination, before.session)
	} else if tab.document != nil {
		if a.editorTerminalGroup != nil {
			a.editorTerminalGroup.editorSelected = false
		}
		a.editorTerminalGroup = destination
		a.activateGroupEditor(destination)
	} else {
		old := tab.tool.group
		old.tools = slices.DeleteFunc(old.tools, func(t *workspaceToolTab) bool { return t == tab.tool })
		if old.selectedTool == tab.tool {
			old.selectedTool = nil
		}
		tab.tool.group = destination
		destination.tools = append(destination.tools, tab.tool)
		a.activateWorkspaceTab(destination, tab)
	}
	order := slices.Clone(a.workspaceTabs(destination))
	order = slices.DeleteFunc(order, func(candidate workspaceTab) bool { return candidate == tab })
	index := slices.Index(order, before)
	if index < 0 {
		index = len(order)
	}
	destination.tabOrder = slices.Insert(order, index, tab)
	a.workspaceRevision++
	a.ensureTerminalGroups()
	a.syncWorkspaceFileFlags()
	a.invalidateWindows()
}
