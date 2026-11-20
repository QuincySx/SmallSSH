package desktop

import (
	"github.com/egoist/mygo/ui"
	"sshclient/internal/connectcfg"
)

func (a *app) sessionRouteLabel(c *ui.Context, route connectcfg.RouteSelection) string {
	if !route.Temporary() {
		return ""
	}
	name := tr(c, "直连")
	if route.Mode == "saved" {
		name = tr(c, "线路不可用")
		for _, saved := range a.routes {
			if saved.ID == route.ID {
				name = saved.Name
				break
			}
		}
	}
	if route.Mode == "relay" {
		name = a.tunnelServerName(route.ID)
	}
	if route.Mode == "session" {
		name = tr(c, "中继会话已结束")
		for _, s := range a.sessions {
			if s.requestID == route.ID {
				name = a.tunnelServerName(s.host)
				break
			}
		}
	}
	if route.Mode == "tunnel" {
		name = "SOCKS5"
		for _, run := range a.tunnels.active {
			if run.requestID == route.ID {
				name += " · " + a.tunnelServerName(run.host)
				break
			}
		}
	}
	return tr(c, "本次：") + name
}

func fileRouteKey(endpoint string, route connectcfg.RouteSelection) string {
	if !route.Temporary() {
		return endpoint
	}
	return endpoint + "\x00" + route.Key()
}

func (p *filePane) snapshot() *filePane {
	return &filePane{endpoint: p.endpoint, route: p.route, states: map[string]*directoryBrowser{fileRouteKey(p.endpoint, p.route): p.browser()}}
}

func (a *app) activeFileRoute() connectcfg.RouteSelection {
	if tool := a.workspaceTools[toolFiles]; tool != nil && tool.source != nil {
		return tool.source.target.Route
	}
	if source := a.currentTerminal(); source != nil {
		return source.target.Route
	}
	return connectcfg.RouteSelection{}
}

func (a *app) fileRoute(endpoint string, explicit ...connectcfg.RouteSelection) connectcfg.RouteSelection {
	if len(explicit) != 0 {
		return explicit[0]
	}
	if p := a.activeFilePane; p != nil && p.endpoint == endpoint {
		return p.route
	}
	if endpoint == a.activeFileEndpoint() {
		return a.activeFileRoute()
	}
	return connectcfg.RouteSelection{}
}

func transferRoute(from string, source, destination connectcfg.RouteSelection) connectcfg.RouteSelection {
	if from == localEndpoint {
		return destination
	}
	return source
}
