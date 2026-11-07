package desktop

import (
	"cmp"
	"fmt"
	"net"
	"slices"
	"strconv"
	"strings"

	"github.com/egoist/mygo/ui"
	"sshclient/internal/desktop/components"
	"sshclient/internal/monitor"
)

const portScopeListeners = "非回环监听 / UDP 绑定"
const portScopeLocal = "本机监听"
const portScopeConnections = "当前外部连接"

func (a *app) portsView(c *ui.Context, snapshot *monitor.PortSnapshot) {
	if snapshot == nil {
		ui.Text(c, tr(c, "尚未采样端口信息")).FontSize(11).TextColor(c.Theme().TextMuted).Tooltip(tr(c, "端口状态由 Linux 监控采样提供；旧版采样器可能不提供此信息。"))
		return
	}
	ui.Row(c).FillWidth().Wrap().Shrink(0).AlignItems(ui.Center).Gap(8).Children(func() {
		ui.Text(c, portSnapshotStatus(c, snapshot)).FontSize(11).TextColor(c.Theme().TextMuted)
		if !snapshot.Time.IsZero() {
			ui.Text(c, fmt.Sprintf(tr(c, "采样于 %s"), snapshot.Time.Local().Format("15:04:05"))).FontSize(10).TextColor(c.Theme().TextMuted)
		}
	})
	if snapshot.Status != "ready" && snapshot.Status != "partial" {
		return
	}
	state := a.managementState()
	if state.portScope == "" {
		state.portScope = portScopeListeners
	}
	if state.portSort.Column == "" {
		state.portSort = ui.SortOrder{Column: "local"}
	}
	ui.Row(c).FillWidth().Shrink(0).AlignItems(ui.Center).Gap(7).Children(func() {
		sourceSelect(c, &state.portScope, []string{portScopeListeners, portScopeLocal, portScopeConnections}).Width(190).MinWidth(0).Label(tr(c, "端口范围")).Tooltip(tr(c, "非回环包含内网与通配地址，不代表公网可达。外部连接按对端非回环地址归类，不推断连接发起方向。UDP 显示绑定或当前对端。"))
		ui.TextInput(c, &state.portSearch).Grow(1).MinWidth(0).Placeholder(tr(c, "搜索地址、端口、进程或 PID")).Label(tr(c, "搜索端口"))
	})
	ports := visiblePorts(snapshot.Ports, state.portScope, state.portSearch)
	sortPorts(ports, state.portSort)
	state.portTable.Sort = &state.portSort
	state.portTable.Label = func(n int) string { return reviewText(portEndpoint(ports[n].LocalAddress, ports[n].LocalPort)) }
	components.Table(c, &state.portTable, []ui.TableColumn{
		{ID: "protocol", Title: tr(c, "协议"), Width: 62, Sortable: true},
		{ID: "state", Title: tr(c, "状态"), Width: 94, Sortable: true},
		{ID: "local", Title: tr(c, "本地地址"), MinWidth: 150, Sortable: true},
		{ID: "peer", Title: tr(c, "对端地址"), MinWidth: 150, Sortable: true},
		{ID: "process", Title: tr(c, "进程"), MinWidth: 100, Sortable: true},
		{ID: "pid", Title: "PID", Width: 76, Align: ui.End, Sortable: true},
	}, len(ports), func(row, col int) {
		p := ports[row]
		process, pid := portOwnerText(p)
		text := p.Protocol
		switch col {
		case 1:
			text = p.State
		case 2:
			text = portEndpoint(p.LocalAddress, p.LocalPort)
		case 3:
			text = portEndpoint(p.PeerAddress, p.PeerPort)
		case 4:
			text = process
		case 5:
			text = pid
		}
		text = reviewText(text)
		components.CellText(c, text).Tooltip(text)
	}).FillWidth().Grow(1).MinHeight(0).Label(tr(c, "端口列表"))
	count := fmt.Sprintf(tr(c, "%d 项端口记录"), len(ports))
	if len(ports) == 0 {
		count = tr(c, "当前范围没有匹配记录")
	}
	ui.Text(c, count).FontSize(10).TextColor(c.Theme().TextMuted)
}

func portSnapshotStatus(c *ui.Context, snapshot *monitor.PortSnapshot) string {
	if snapshot.Status != "ready" && snapshot.Status != "partial" {
		switch snapshot.Note {
		case "missing-tools":
			return tr(c, "端口信息不可用：未找到 ss 或 netstat")
		case "probe-failed":
			return tr(c, "端口信息不可用：采样工具执行失败")
		default:
			return tr(c, "端口信息不可用：采样未完成")
		}
	}
	parts := []string{tr(c, "端口采样完成")}
	if snapshot.Status == "partial" {
		parts[0] = tr(c, "端口信息不完整")
	}
	if snapshot.ProcessLimited {
		parts = append(parts, tr(c, "部分进程信息不可见"))
	}
	if snapshot.Truncated {
		parts = append(parts, tr(c, "记录已截断"))
	}
	if snapshot.Note == "unrecognized-output" {
		parts = append(parts, tr(c, "部分记录无法解析"))
	}
	return strings.Join(parts, " · ")
}

func visiblePorts(ports []monitor.Port, scope, query string) []monitor.Port {
	result := make([]monitor.Port, 0, len(ports))
	query = strings.ToLower(strings.TrimSpace(query))
	for _, port := range ports {
		matches := false
		switch scope {
		case portScopeListeners:
			matches = port.Listening() && !monitor.PortLoopback(port.LocalAddress)
		case portScopeLocal:
			matches = port.Listening() && monitor.PortLoopback(port.LocalAddress)
		case portScopeConnections:
			matches = port.ExternalConnection()
		}
		process, pid := portOwnerText(port)
		search := port.Protocol + " " + port.State + " " + portEndpoint(port.LocalAddress, port.LocalPort) + " " + portEndpoint(port.PeerAddress, port.PeerPort) + " " + process + " " + pid
		if matches && strings.Contains(strings.ToLower(search), query) {
			result = append(result, port)
		}
	}
	return result
}

func sortPorts(ports []monitor.Port, order ui.SortOrder) {
	slices.SortStableFunc(ports, func(a, b monitor.Port) int {
		var result int
		switch order.Column {
		case "protocol":
			result = cmp.Compare(a.Protocol, b.Protocol)
		case "state":
			result = cmp.Compare(a.State, b.State)
		case "local":
			result = comparePortEndpoint(a.LocalAddress, a.LocalPort, b.LocalAddress, b.LocalPort)
		case "peer":
			result = comparePortEndpoint(a.PeerAddress, a.PeerPort, b.PeerAddress, b.PeerPort)
		case "process":
			ap, _ := portOwnerText(a)
			bp, _ := portOwnerText(b)
			result = cmp.Compare(ap, bp)
		case "pid":
			result = cmp.Compare(a.PID, b.PID)
		}
		if result == 0 {
			result = cmp.Compare(portEndpoint(a.LocalAddress, a.LocalPort)+a.Protocol+a.State+a.PeerAddress+a.PeerPort, portEndpoint(b.LocalAddress, b.LocalPort)+b.Protocol+b.State+b.PeerAddress+b.PeerPort)
		}
		if order.Descending {
			return -result
		}
		return result
	})
}

func comparePortEndpoint(aAddress, aPort, bAddress, bPort string) int {
	if result := cmp.Compare(aAddress, bAddress); result != 0 {
		return result
	}
	a, ae := strconv.Atoi(aPort)
	b, be := strconv.Atoi(bPort)
	if ae == nil && be == nil {
		return cmp.Compare(a, b)
	}
	return cmp.Compare(aPort, bPort)
}

func portEndpoint(address, port string) string {
	if address == "" {
		return "—"
	}
	return net.JoinHostPort(address, port)
}

func portOwnerText(port monitor.Port) (string, string) {
	owners := port.Owners
	if len(owners) == 0 && (port.Process != "" || port.PID > 0) {
		owners = []monitor.PortOwner{{Process: port.Process, PID: port.PID}}
	}
	var processes, pids []string
	for _, owner := range owners {
		if owner.Process != "" && !slices.Contains(processes, owner.Process) {
			processes = append(processes, owner.Process)
		}
		if owner.PID > 0 {
			pid := strconv.Itoa(owner.PID)
			if !slices.Contains(pids, pid) {
				pids = append(pids, pid)
			}
		}
	}
	return nonEmpty(strings.Join(processes, ", ")), nonEmpty(strings.Join(pids, ", "))
}
