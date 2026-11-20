package desktop

import (
	"math"
	"slices"
	"strings"

	"github.com/egoist/mygo/ui"
	"sshclient/internal/desktop/components"
)

// Canvas and tab aggregation share owners. A presentation change only moves
// existing views; it never opens an SSH connection or rebuilds a browser.
func (a *app) setWorkspaceCanvas(canvas bool) {
	if a.deferTerminalMutation(func() { a.setWorkspaceCanvas(canvas) }) {
		return
	}
	a.ensureTerminalGroups()
	a.workspaceCanvas = canvas
	if canvas {
		a.tileWorkspaceTabs()
	} else {
		a.mergeTerminalViews()
	}
	a.fileExpanded = false
	a.workspaceRevision++
	a.contentFocus = a.terminalFocusWanted()
	a.invalidateWindows()
}

func (a *app) tileWorkspaceTabs() {
	focused := a.selectedWorkspaceTab(a.activeTerminalGroup)
	var entries []workspaceTab
	for _, group := range a.terminalGroups() {
		entries = append(entries, a.workspaceTabs(group)...)
	}
	if len(entries) == 0 {
		return
	}
	leaves := make([]*terminalGroup, 0, len(entries))
	for _, entry := range entries {
		group := a.newTerminalGroup()
		group.tabOrder = []workspaceTab{entry}
		switch {
		case entry.session != nil:
			group.sessions, group.active = []*session{entry.session}, entry.session
			entry.session.group = group
		case entry.tool != nil:
			group.tools, group.selectedTool = []*workspaceToolTab{entry.tool}, entry.tool
			entry.tool.group = group
		case entry.document != nil:
			group.editorSelected = true
			a.editorTerminalGroup = group
		}
		if entry == focused {
			a.activeTerminalGroup = group
		}
		leaves = append(leaves, group)
	}
	columns := int(math.Ceil(math.Sqrt(float64(len(leaves)))))
	var rows []*terminalGroup
	for begin := 0; begin < len(leaves); begin += columns {
		rows = append(rows, canvasBranch(leaves[begin:min(begin+columns, len(leaves))], false))
	}
	root := canvasBranch(rows, true)
	if root.split == nil {
		a.singleTerminalGroup, a.terminalLayout = root, nil
	} else {
		a.terminalLayout = root.split
	}
	a.ensureTerminalGroups()
	a.syncWorkspaceFileFlags()
}

func canvasBranch(groups []*terminalGroup, vertical bool) *terminalGroup {
	if len(groups) == 1 {
		return groups[0]
	}
	half := len(groups) / 2
	return &terminalGroup{split: &terminalLayoutNode{
		left: canvasBranch(groups[:half], vertical), right: canvasBranch(groups[half:], vertical),
		vertical: vertical, share: float32(half) * 100 / float32(len(groups)),
	}}
}

func (a *app) placeCanvasTab(tab workspaceTab, group *terminalGroup) {
	if !a.workspaceCanvas || len(a.workspaceTabs(group)) < 2 {
		return
	}
	edge := "right"
	destination := group
	if tab.tool == nil || tab.tool.kind != toolFiles {
		for _, candidate := range a.terminalGroups() {
			if candidate.canvasBounds.W*candidate.canvasBounds.H > destination.canvasBounds.W*destination.canvasBounds.H {
				destination = candidate
			}
		}
		if destination.canvasBounds.H > destination.canvasBounds.W {
			edge = "bottom"
		}
	}
	if tab.tool != nil && tab.tool.kind == toolFiles {
		edge = "bottom"
	}
	a.dropWorkspaceTab(a.workspaceTabPayload(tab), destination, workspaceTab{}, edge)
}

func (a *app) toggleCanvasTool(kind int) {
	if tool := a.workspaceTools[kind]; tool != nil {
		if tool.group.selectedTool == tool {
			a.closeWorkspaceTab(workspaceTab{tool: tool})
		} else {
			a.activateWorkspaceTab(tool.group, workspaceTab{tool: tool})
		}
		a.invalidateWindows()
		return
	}
	a.openWorkspaceTool(kind)
}

func workspaceKindIcon(kind int) *ui.SVG {
	if kind >= toolVNCBase {
		return components.MonitorIcon
	}
	switch kind {
	case -1:
		return components.TerminalIcon
	case toolFiles:
		return components.FolderIcon
	case toolCommands:
		return components.CodeFileIcon
	case toolTransfers:
		return components.TransfersIcon
	default:
		return components.WorkspaceIcon
	}
}

func (a *app) workspaceAppsRail(c *ui.Context) {
	ui.Column(c).Width(32).Shrink(0).FillHeight().Gap(4).AlignItems(ui.Center).Label(tr(c, "画布功能开关")).Children(func() {
		for _, kind := range []int{toolFiles, toolCommands, toolDiagnostics} {
			name := workspaceToolTitle(c, kind)
			button := components.SVGButton(c, workspaceKindIcon(kind), tr(c, "画布功能：")+name).Radius(8).Tooltip(name)
			tool := a.workspaceTools[kind]
			if tool != nil && tool.group.selectedTool == tool {
				button.Background(c.Theme().Accent.Alpha(.12)).TextColor(c.Theme().Accent)
			}
			if button.Clicked() {
				a.toggleCanvasTool(kind)
			}
		}
		ui.Spacer(c).Grow(1)
		components.SVGButton(c, components.MoreIcon, tr(c, "画布布局")).Menu(func(m *ui.Menu) {
			if m.Item(tr(c, "平铺全部面板")).Chosen() {
				a.setWorkspaceCanvas(true)
			}
			if m.Item(tr(c, "聚合为标签")).Chosen() {
				a.setWorkspaceCanvas(false)
			}
		})
	})
}

func (a *app) canvasPaneHeader(c *ui.Context, group *terminalGroup, bounds ui.Rect) {
	tab := a.selectedWorkspaceTab(group)
	if tab == (workspaceTab{}) {
		return
	}
	title := a.workspaceTabDisplayTitle(c, tab)
	kind := -1
	if tab.tool != nil {
		kind = tab.tool.kind
	}
	height := float32(components.PanelHeaderHeight)
	if tab.tool != nil {
		height = components.CanvasToolHeaderHeight
	}
	head := components.HeaderBar(c).Height(height).Padding(0, components.PanelInset, 0, 16).Gap(6).
		Label(tr(c, "画布面板：") + a.workspaceTabTitle(c, tab)).Drag(a.workspaceTabPayload(tab))
	if a.activeTerminalGroup == group {
		head.Background(c.Theme().Accent.Alpha(.035))
	}
	head.Children(func() {
		if tab.session != nil {
			color := c.Theme().Accent
			if tab.session.closed {
				color = c.Theme().TextMuted
			}
			ui.Box(c).Size(6, 6).Radius(3).Background(color).Shrink(0)
		}
		ui.Icon(c, workspaceKindIcon(kind)).Size(14, 14).Shrink(0).TextColor(c.Theme().TextMuted)
		components.HeaderLabel(c, title).Grow(1).MinWidth(0).Tooltip(title)
		if a.workspaceCanvas {
			components.ButtonGroup(c, func() { a.canvasPaneActions(c, group) }).MinWidth(0).Shrink(1)
		}
		label := tr(c, "关闭标签：") + a.workspaceTabTitle(c, tab)
		if tab.tool != nil && tab.tool.kind == toolFiles {
			label = tr(c, "关闭文件标签")
		}
		if components.SVGButton(c, components.CloseIcon, label).Size(24, 24).Clicked() {
			a.closeWorkspaceTab(tab)
		}
	})
	head.ContextMenu(func(m *ui.Menu) { a.workspaceTabMenu(m, tab) })
	// Dropping at the center shares the pane; the global strip switches its views.
	// The narrow top strip forwards a top split to the whole card.
	a.workspaceGroupDrop(c, head, group, workspaceTab{}, false, bounds)
	if head.Clicked() {
		a.activateWorkspaceTab(group, tab)
	}
}

func (a *app) canvasPaneActions(c *ui.Context, group *terminalGroup) {
	tab := a.selectedWorkspaceTab(group)
	if tab.session != nil {
		source := tab.session
		if components.SVGButton(c, components.FolderIcon, tr(c, "在下方打开文件")).Size(24, 24).Disabled(source.closed).Clicked() {
			a.openCanvasFiles(source, group)
		}
		if components.SVGButton(c, components.RightPanelIcon, tr(c, "在右侧新建终端")).Size(24, 24).Disabled(source.closed).Clicked() {
			a.newCanvasTerminal(source, group, "right")
		}
		if components.SVGButton(c, components.BottomPanelIcon, tr(c, "在下方新建终端")).Size(24, 24).Disabled(source.closed).Clicked() {
			a.newCanvasTerminal(source, group, "bottom")
		}
	} else if tab.tool != nil && tab.tool.vnc == nil {
		if components.SVGButton(c, components.BottomPanelIcon, tr(c, "收回工具区")).Size(24, 24).Clicked() {
			a.dockWorkspaceTool(tab.tool)
		}
	}
}

// A terminal's folder action explicitly selects that server. Merely focusing
// another card continues to leave an existing file view bound to its source.
func (a *app) openCanvasFiles(source *session, group *terminalGroup) {
	if a.deferTerminalMutation(func() { a.openCanvasFiles(source, group) }) {
		return
	}
	if !a.hasSession(source) || source.closed || !slices.Contains(a.terminalGroups(), group) {
		return
	}
	a.activateTerminal(source, false)
	a.openWorkspaceTool(toolFiles)
	tool := a.workspaceTools[toolFiles]
	if tool == nil {
		return
	}
	tool.source = source
	a.chooseFileEndpoint(a.serverFilePane(), followTerminalEndpoint)
	a.dropWorkspaceTab(a.workspaceTabPayload(workspaceTab{tool: tool}), group, workspaceTab{}, "bottom")
}

func (a *app) newCanvasTerminal(source *session, group *terminalGroup, edge string) {
	if !a.hasSession(source) || source.closed || !slices.Contains(a.terminalGroups(), group) {
		return
	}
	if a.deferTerminalMutation(func() { a.newCanvasTerminal(source, group, edge) }) {
		return
	}
	if _, err := a.unlockedOwner(); err != nil {
		a.status = err.Error()
		return
	}
	a.activateTerminal(source, false)
	pending := a.reserveTerminal(source.host, group, false)
	pending.target.Route = source.target.Route
	if source.target.Kind == "docker-terminal" {
		name := source.containerName
		if name == "" {
			name = strings.TrimPrefix(source.name, "容器 · ")
		}
		// The placeholder takes the title the connected terminal will keep, so
		// repeated splits of one container stay distinguishable.
		pending.target, pending.containerName = source.target, name
		pending.name = a.containerTerminalName(name, pending)
	}
	// Placement belongs to the click, not a later successful handshake. Queue,
	// failure and cancellation stay in this same card even if focus changes.
	a.dropWorkspaceTab(a.workspaceTabPayload(workspaceTab{session: pending}), group, workspaceTab{}, edge)
	if source.target.Kind == "docker-terminal" {
		a.openContainerTerminal(pending, pending.target.Docker.Target, pending.containerName, pending.target.Docker.Shell, pending)
	} else {
		a.openShellInGroup(source.host, true, pending.group, nil, pending)
	}
}

func (a *app) workspaceActivity(c *ui.Context) {
	a.setTransferWindow(a.renderingToolsWindow)
	state := &a.activityTable
	if tool := a.renderingWorkspaceTool; tool != nil && tool.kind == toolTransfers {
		state = &tool.activityTable
	}
	ui.Column(c).Fill().MinHeight(0).MinWidth(0).Padding(8).Gap(6).Children(func() {
		a.activityContent(c, state)
	})
}
