package desktop

import (
	"github.com/egoist/mygo/ui"
	"sshclient/internal/connectcfg"
)

type liveConnectionRoute struct {
	label string
	route connectcfg.RouteSelection
}

func (a *app) liveRelay(id string) *session {
	for _, s := range a.sessions {
		if s.requestID == id && id != "" && !s.closed && s.connectionState == "" && s.target.Kind == "shell" {
			return s
		}
	}
	return nil
}

func (a *app) liveSOCKS(id string) *tunnelRun {
	for _, run := range a.tunnels.active {
		if run.requestID == id && id != "" && run.running && run.ready && run.kind == "socks5" {
			return run
		}
	}
	return nil
}

// The menu lists working sources. Choosing one captures its operation reference;
// the broker resolves it and rejects a source that has since stopped.
func (a *app) liveConnectionRoutes(target string) []liveConnectionRoute {
	var choices []liveConnectionRoute
	seen := map[string]bool{}
	for _, s := range a.sessions {
		if s.host == target || a.liveRelay(s.requestID) != s {
			continue
		}
		key := fileRouteKey(s.host, s.target.Route)
		if seen[key] {
			continue
		}
		seen[key] = true
		label := tr(nil, "通过 ") + a.tunnelServerName(s.host)
		if suffix := a.sessionRouteLabel(nil, s.target.Route); suffix != "" {
			label += " · " + suffix
		}
		choices = append(choices, liveConnectionRoute{label, connectcfg.RouteSelection{Mode: "session", ID: s.requestID}})
	}
	for _, run := range a.tunnels.active {
		if a.liveSOCKS(run.requestID) == run {
			label := "SOCKS5 · " + a.tunnelServerName(run.host) + " · " + run.address
			choices = append(choices, liveConnectionRoute{label, connectcfg.RouteSelection{Mode: "tunnel", ID: run.requestID}})
		}
	}
	return choices
}

func (a *app) relayServerOptions(target string) []connectionOption {
	var options []connectionOption
	names := map[string]int{}
	for _, server := range a.connections {
		if server.ID != target {
			names[server.Name]++
		}
	}
	for _, server := range a.connections {
		if server.ID == target {
			continue
		}
		label := server.Name
		if names[label] > 1 {
			label += " · " + targetID(server.Host, server.Port)
		}
		options = append(options, connectionOption{server.ID, label})
	}
	return options
}

func relayServerMenu(c *ui.Context, menu *ui.Menu, options []connectionOption, chosen func(connectcfg.RouteSelection)) {
	menu.Submenu(tr(c, "新建 SSH 中转"), func(sub *ui.Menu) {
		for _, opt := range options {
			route := connectcfg.RouteSelection{Mode: "relay", ID: opt.value}
			if sub.Item(opt.label).ID(route.Key()).Chosen() {
				chosen(route)
			}
		}
	})
}
