package desktop

import "testing"

// Server status is fetched over SSH, so it must follow the screen exactly.
func TestMonitorVisibilityFollowsTheStatusPanel(t *testing.T) {
	active, other := &session{}, &session{}
	a := &app{online: true, page: 1, dock: toolDiagnostics, sessions: []*session{active}, active: 0}
	if !a.monitorWanted(active) {
		t.Fatal("the status panel was on screen but nothing was fetched")
	}
	if a.monitorWanted(other) {
		t.Fatal("an inactive terminal's status was fetched although the panel shows another")
	}
	for _, hidden := range []struct {
		name  string
		apply func(*app)
	}{
		{"collapsed panel", func(a *app) { a.toolsHidden = true }},
		{"another tool tab", func(a *app) { a.dock = toolFiles }},
		{"another page", func(a *app) { a.page = 3 }},
		{"signed out", func(a *app) { a.online = false }},
	} {
		b := &app{online: true, page: 1, dock: toolDiagnostics, sessions: []*session{active}, active: 0}
		hidden.apply(b)
		if b.monitorWanted(active) {
			t.Fatalf("%s: kept fetching server status the user cannot see", hidden.name)
		}
	}
	if (&app{page: 1, dock: toolDiagnostics}).monitorWanted(nil) {
		t.Fatal("no session reported as visible")
	}
}

func TestPromotedStatusMonitorsItsVisibleSourceIndependentlyOfTheDock(t *testing.T) {
	a := workspaceModelFixture(2)
	source, other := a.sessions[0], a.sessions[1]
	a.openWorkspaceTool(toolDiagnostics)
	tool := a.workspaceTools[toolDiagnostics]
	a.dropWorkspaceTab(a.workspaceTabPayload(workspaceTab{tool: tool}), source.group, workspaceTab{}, "right")
	a.activateTerminal(other, true)
	a.toolsHidden = true
	if !a.monitorWanted(source) || a.monitorWanted(other) {
		t.Fatal("visible promoted status did not keep its original server subscription")
	}
	a.dropWorkspaceTab(a.workspaceTabPayload(workspaceTab{session: other}), tool.group, workspaceTab{}, "")
	if a.monitorWanted(source) {
		t.Fatal("a covered status tab continued polling")
	}
	a.activateWorkspaceTab(tool.group, workspaceTab{tool: tool})
	a.fileExpanded = true
	if a.monitorWanted(source) {
		t.Fatal("a maximized dock did not pause a covered status pane")
	}
}
