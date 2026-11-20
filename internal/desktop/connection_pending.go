package desktop

import (
	"fmt"

	"github.com/egoist/mygo/ui"
	"sshclient/internal/approval"
	"sshclient/internal/desktop/components"
)

func (a *app) reserveTerminal(host string, group *terminalGroup, activate bool) *session {
	a.ensureTerminalGroups()
	if group == nil {
		group = a.activeTerminalGroup
	}
	name := host
	for _, profile := range a.connections {
		if profile.ID == host {
			name = profile.Name
			break
		}
	}
	base := name
	for n := 2; ; n++ {
		found := false
		for _, existing := range a.sessions {
			if existing.name == name {
				found = true
				break
			}
		}
		if !found {
			break
		}
		name = fmt.Sprintf("%s (%d)", base, n)
	}
	pending := &session{host: host, name: name, group: group, connectionState: "connecting", target: approval.Operation{Host: host, Kind: "shell"}}
	a.sessions = append(a.sessions, pending)
	group.sessions = append(group.sessions, pending)
	if group.active == nil {
		group.active = pending
	}
	if activate {
		a.page, a.dock = 1, toolCommands
		a.placeCanvasTab(workspaceTab{session: pending}, group)
		a.activateTerminal(pending, true)
	}
	return pending
}

func (a *app) failPendingTerminals(host, message string) {
	for _, request := range a.pendingShellOpens[host] {
		if request.pending != nil {
			request.pending.closed = true
			request.pending.connectionState, request.pending.connectionError = "error", message
		}
	}
	delete(a.pendingShellOpens, host)
}

func (a *app) retryPendingTerminal(s *session) {
	if !a.hasSession(s) {
		return
	}
	// Both open paths return before installing a cancel while the vault is
	// locked or offline. Refuse here, so the card keeps a reason and its retry
	// button instead of a spinner that nothing will finish.
	if _, err := a.unlockedOwner(); err != nil {
		s.connectionError, s.connectionMismatch, a.status = err.Error(), false, err.Error()
		return
	}
	s.closed, s.connectionError, s.connectionMismatch = false, "", false
	s.connectionState = "connecting"
	if s.target.Kind == "docker-terminal" {
		a.openContainerTerminal(s, s.target.Docker.Target, s.containerName, s.target.Docker.Shell, s)
	} else {
		a.openShellInGroup(s.host, true, s.group, nil, s)
	}
}

func (a *app) pendingTerminalView(c *ui.Context, s *session) {
	ui.Column(c).Fill().Padding(24).Gap(12).Center().Label(tr(c, "服务器连接状态")).Children(func() {
		if s.connectionState != "error" {
			ui.Spinner(c).Size(26, 26)
			text := tr(c, "正在连接服务器…")
			if s.connectionState == "queued" {
				text = tr(c, "正在等待前一连接…")
			}
			ui.Text(c, text).FontSize(16).Bold()
		} else {
			ui.Text(c, tr(c, "连接未完成")).FontSize(16).Bold()
			ui.Text(c, reviewText(s.connectionError)).FillWidth().TextAlign(ui.Center).TextColor(c.Theme().Danger)
		}
		ui.Text(c, a.hostName(s.host)).FontSize(12).FillWidth().TextAlign(ui.Center).TextColor(c.Theme().TextMuted)
		if route := a.sessionRouteLabel(c, s.target.Route); route != "" {
			ui.Text(c, route).FontSize(12).FillWidth().TextAlign(ui.Center).TextColor(c.Theme().TextMuted)
		}
		ui.Row(c).FillWidth().Gap(8).Wrap().Justify(ui.Center).Children(func() {
			if s.connectionState == "error" {
				label := tr(c, "重试连接")
				if s.connectionMismatch {
					label = tr(c, "重新核对服务器身份")
				}
				if components.Button(c, label).Clicked() {
					if s.connectionMismatch {
						a.offerHostKeyUpdate(s.host, s.target.Route, func() { a.retryPendingTerminal(s) })
					} else {
						a.retryPendingTerminal(s)
					}
				}
			}
			// While connecting, closing is cancelling; say so on the button.
			closeLabel := tr(c, "取消连接")
			if s.connectionState == "error" {
				closeLabel = tr(c, "关闭此标签")
			}
			if components.Button(c, closeLabel).Clicked() {
				a.closeSessionAt(a.sessionIndex(s))
			}
		})
	})
}
