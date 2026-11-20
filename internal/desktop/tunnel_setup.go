package desktop

import (
	"fmt"
	"net"
	"strings"

	"github.com/egoist/mygo/ui"
	"sshclient/internal/connectcfg"
	"sshclient/internal/desktop/components"
	"sshclient/internal/vault"
)

var tunnelKinds = [...]struct{ id, label, listen, target, action string }{
	{"local", "访问远端服务", "127.0.0.1:0", "127.0.0.1:80", "启动转发"},
	{"remote", "让服务器访问本机服务", "127.0.0.1:0", "127.0.0.1:3000", "启动远程转发"},
	{"socks5", "把服务器当代理", "127.0.0.1:1080", "", "启动 SOCKS5"},
}

type tunnelEndpoints struct{ listen, target string }

func (s *tunnelManager) selectKind(kind string) {
	if s.kind == kind {
		return
	}
	if s.drafts == nil {
		s.drafts = map[string]tunnelEndpoints{}
	}
	if s.kind != "" {
		s.drafts[s.kind] = tunnelEndpoints{s.listen, s.target}
	}
	s.kind, s.message = kind, ""
	if s.latest != nil {
		s.latest.openWebsite = false
		s.latest = nil
	}
	if draft, ok := s.drafts[kind]; ok {
		s.listen, s.target = draft.listen, draft.target
		return
	}
	for _, scene := range tunnelKinds {
		if scene.id == kind {
			s.listen, s.target = scene.listen, scene.target
			if kind == "socks5" && s.routeChoice == "local-socks" && s.localProxy == s.listen {
				s.listen = "127.0.0.1:1081"
			}
			return
		}
	}
}

func (s *tunnelManager) purpose() string {
	switch s.kind {
	case "local":
		if s.browse {
			return "website"
		}
		return "service"
	case "remote":
		return "share"
	default:
		return ""
	}
}

func (s *tunnelManager) actionLabel() string {
	if s.purpose() == "website" {
		return "打开网页"
	}
	for _, mode := range tunnelKinds {
		if mode.id == s.kind {
			return mode.action
		}
	}
	return "启动转发"
}

func (a *app) tunnelSetupView(c *ui.Context) {
	s := &a.tunnels
	if len(a.connections) == 0 {
		ui.Text(c, tr(c, "先添加一台服务器")).FillWidth().TextColor(c.Theme().TextMuted)
		if components.Button(c, tr(c, "新建连接")).Clicked() {
			s.open = false
			a.navigate(a.newConnection)
		}
		return
	}
	if s.kind == "" {
		s.selectKind("local")
	}
	ui.Row(c).FillWidth().Gap(6).Children(func() {
		for _, scene := range tunnelKinds {
			c.Key(scene.id)
			theme := c.Theme()
			bg, border := theme.Surface, theme.Border
			if s.kind == scene.id {
				bg, border = theme.Accent.Alpha(.10), theme.Accent
			}
			button := ui.ButtonBase(c).Grow(1).Basis(0).MinWidth(0).Height(66).Radius(8).
				Background(bg).Border(1, border).Label(tr(c, scene.label))
			button.Children(func() {
				ui.Column(c).Fill().Center().Gap(3).Padding(4, 5).Children(func() {
					ui.Row(c).Width(92).Height(18).AlignItems(ui.Center).Gap(3).Children(func() {
						ui.Icon(c, components.MonitorIcon).Size(15, 15)
						tunnelWire(c, scene.id == "remote", 0, nil)
						ui.Icon(c, components.ServerIcon).Size(15, 15)
						if scene.id == "socks5" {
							tunnelWire(c, false, 0, nil)
							ui.Icon(c, components.TunnelTargetIcon).Size(15, 15)
						}
					})
					ui.Text(c, tr(c, scene.label)).FillWidth().FontSize(11).Wrap().TextAlign(ui.Center)
				})
			})
			if button.Clicked() {
				s.selectKind(scene.id)
				c.Invalidate()
			}
		}
	})

	a.tunnelDiagram(c)
	if s.routeChoice == "local-socks" {
		tunnelEndpointInput(c, "已有 SOCKS5 代理", &s.localProxy)
	}
	if s.kind == "local" {
		ui.Checkbox(c, &s.browse, tr(c, "启动后打开网页"))
		if s.browse {
			optionSelect(c, "网页协议", &s.webProtocol, []connectionOption{{"auto", "按端口自动"}, {"http", "HTTP"}, {"https", "HTTPS"}})
		}
	}
}

func (a *app) tunnelDiagram(c *ui.Context) {
	s := &a.tunnels
	remote, socks := s.kind == "remote", s.kind == "socks5"
	ui.Row(c).FillWidth().MinWidth(0).Padding(10, 8).AlignItems(ui.Center).
		Background(c.Theme().Background).Radius(10).Children(func() {
		title := "本机应用"
		if remote {
			title = "本机服务"
		} else if socks {
			title = "本机代理"
		}
		c.Key("tunnel-local-node")
		tunnelDiagramNode(c, components.MonitorIcon, title, remote || socks, func() {
			switch {
			case remote:
				tunnelNodeEndpoint(c, "本机服务端口", &s.target, false)
			case socks:
				tunnelNodeEndpoint(c, "SOCKS5 端口", &s.listen, true)
			default:
				tunnelNodeEndpoint(c, "本机入口端口", &s.listen, true)
			}
		})
		a.tunnelRouteWire(c, remote)
		c.Key("tunnel-server-node")
		tunnelDiagramNode(c, components.ServerIcon, "", !remote && !socks, func() {
			a.tunnelServerPicker(c)
			if s.host != "" {
				endpoint := a.tunnelServerEndpoint(s.host)
				ui.Text(c, endpoint).FillWidth().FontSize(10).SingleLine().TextAlign(ui.Center).TextColor(c.Theme().TextMuted).Tooltip(endpoint)
			}
			if remote {
				tunnelNodeEndpoint(c, "服务器本机入口", &s.listen, true)
				ui.Text(c, tr(c, "在服务器本机使用")).FontSize(10).FillWidth().Wrap().TextAlign(ui.Center).TextColor(c.Theme().TextMuted)
			} else if !socks {
				tunnelNodeEndpoint(c, "服务端口", &s.target, false)
			}
		})
		if socks {
			tunnelWire(c, false, 18, nil)
			tunnelDiagramNode(c, components.TunnelTargetIcon, "应用选的目标", false, nil)
		}
	})
}

func tunnelDiagramNode(c *ui.Context, icon *ui.SVG, title string, active bool, controls func()) {
	theme := c.Theme()
	bg, border := theme.Surface, theme.Border
	if active {
		bg, border = theme.Accent.Alpha(.07), theme.Accent.Alpha(.65)
	}
	ui.Column(c).Grow(1).Basis(0).MinWidth(0).Gap(4).Padding(7, 8).AlignItems(ui.Center).
		Background(bg).Border(1, border).Radius(8).Children(func() {
		ui.Icon(c, icon).Size(20, 20).TextColor(theme.TextMuted)
		if title != "" {
			ui.Text(c, tr(c, title)).FillWidth().FontSize(12).Bold().Wrap().TextAlign(ui.Center)
		}
		if controls != nil {
			controls()
		}
	})
}

func tunnelNodeEndpoint(c *ui.Context, label string, address *string, auto bool) {
	host, port, err := net.SplitHostPort(*address)
	if err != nil {
		host = "127.0.0.1"
	}
	if auto && port == "0" {
		port = ""
	}
	label = tr(c, label)
	ui.Text(c, label).FillWidth().FontSize(10).Wrap().TextAlign(ui.Center).TextColor(c.Theme().TextMuted)
	hostEdited := ui.TextInput(c, &host).FillWidth().Height(26).MinWidth(0).Label(label + tr(c, "主机输入")).Changed()
	placeholder := ""
	if auto {
		placeholder = tr(c, "自动分配")
	}
	portEdited := ui.TextInput(c, &port).FillWidth().Height(28).MinWidth(0).
		Label(label + tr(c, "输入")).Placeholder(placeholder).Changed()
	if hostEdited || portEdited {
		if auto && port == "" {
			port = "0"
		}
		*address = net.JoinHostPort(host, port)
	}
}

func tunnelWire(c *ui.Context, reverse bool, width float32, body func()) {
	row := ui.Row(c).Height(26).Center()
	if width == 0 {
		row.Grow(1).MinWidth(12)
	} else {
		row.Width(width).Shrink(0)
	}
	accent := c.Theme().Accent
	row.DrawOver(func(p *ui.Painter, r ui.Rect) {
		if r.W < 8 {
			return
		}
		left, right, y := r.X, r.X+r.W, r.Y+r.H/2
		if body != nil {
			// Leave the middle clear for the route menu trigger.
			center := (left + right) / 2
			p.Line(left, y, center-14, y, 1.4, accent)
			p.Line(center+14, y, right, y, 1.4, accent)
		} else {
			p.Line(left, y, right, y, 1.4, accent)
		}
		tip, base := right-1, right-5
		if reverse {
			tip, base = left+1, left+5
		}
		var arrow ui.Path
		arrow.MoveTo(base, y-3).LineTo(tip, y).LineTo(base, y+3)
		p.StrokePath(&arrow, 1.4, accent)
	})
	if body != nil {
		row.Children(body)
	}
}

func (a *app) tunnelRouteWire(c *ui.Context, reverse bool) {
	s := &a.tunnels
	options := a.tunnelRouteOptions()
	if s.routeChoice == "direct" || s.routeChoice == "" && !a.tunnelHasDefaultHop() {
		tunnelWire(c, reverse, 68, func() {
			tooltip := "选择线路"
			if s.routeChoice == "direct" {
				tooltip = "本次直连"
			}
			components.SVGButton(c, components.PlusIcon, tr(c, "选择线路"), tr(c, tooltip)).Size(24, 24).
				Menu(func(m *ui.Menu) { a.tunnelRoutesMenu(c, m, options) })
		})
		return
	}
	tunnelWire(c, reverse, 18, nil)
	c.Key("tunnel-route-node")
	tunnelDiagramNode(c, components.TunnelIcon, "SSH 线路", false, func() {
		if components.SVGButton(c, components.CloseIcon, tr(c, "移除中转")).Absolute().Top(4).End(4).Size(22, 22).Clicked() {
			s.routeChoice = "direct"
			c.Invalidate()
		}
		text := tr(c, "线路不可用")
		if route := s.selectedRoute(); route.Mode == "relay" {
			text = a.tunnelServerName(route.ID)
		}
		for _, option := range options {
			if option.value == s.routeChoice {
				text = option.label
				break
			}
		}
		components.Button(c, tr(c, text)).FillWidth().MinWidth(0).Label(tr(c, "选择线路")).
			Menu(func(m *ui.Menu) { a.tunnelRoutesMenu(c, m, options) })
	})
	tunnelWire(c, reverse, 18, nil)
}

func (a *app) tunnelServerPicker(c *ui.Context) {
	s := &a.tunnels
	options := []connectionOption{{"", "选择服务器"}}
	counts := map[string]int{}
	for _, p := range a.connections {
		counts[p.Name]++
	}
	for _, p := range a.connections {
		label := p.Name
		if counts[label] > 1 {
			label += " · " + targetID(p.Host, p.Port)
		}
		options = append(options, connectionOption{p.ID, label})
	}
	if optionSelect(c, "使用服务器", &s.host, options) {
		s.baseRoute = connectcfg.RouteSelection{}
	}
}

func tunnelEndpointInput(c *ui.Context, label string, value *string) {
	host, port, err := net.SplitHostPort(*value)
	if err != nil {
		host, port = *value, ""
	}
	ui.Field(c, tr(c, label), func() {
		ui.Row(c).FillWidth().Gap(8).AlignItems(ui.Center).Children(func() {
			changed := ui.TextInput(c, &host).Grow(1).MinWidth(0).Height(36).Label(tr(c, label) + tr(c, "主机输入")).Placeholder(tr(c, "IP 地址或域名")).Changed()
			ui.Text(c, tr(c, "端口")).Shrink(0).TextColor(c.Theme().TextMuted)
			changed = ui.TextInput(c, &port).Width(76).Height(36).Label(tr(c, label)+tr(c, "端口输入")).Changed() || changed
			if changed {
				*value = net.JoinHostPort(host, port)
			}
		})
	})
}

func (s *tunnelManager) selectedRoute() connectcfg.RouteSelection {
	if s.routeChoice == "direct" {
		return connectcfg.RouteSelection{Mode: "direct"}
	}
	mode, id, _ := strings.Cut(s.routeChoice, ":")
	switch mode {
	case "saved", "relay", "session", "tunnel":
		return connectcfg.RouteSelection{Mode: mode, ID: id}
	}
	return s.baseRoute
}

func (a *app) localTunnelProxy(address string) (vault.SavedRoute, error) {
	if err := connectcfg.Address(address, false); err != nil {
		return vault.SavedRoute{}, err
	}
	host, _, _ := net.SplitHostPort(address)
	ip := net.ParseIP(host)
	if !((ip != nil && ip.IsLoopback()) || host == "localhost") {
		return vault.SavedRoute{}, fmt.Errorf("%s", tr(nil, "本机代理请填写回环地址；其他代理请从已保存线路选择"))
	}
	for _, r := range a.routes {
		if r.Relay == "" && r.Proxy.Mode == "socks5" && r.Proxy.Address == address && r.Proxy.User == "" && !r.HasPassword {
			return vault.SavedRoute{ID: r.ID, Name: r.Name, Proxy: r.Proxy}, nil
		}
	}
	return vault.SavedRoute{ID: vault.ShortID(), Name: tr(nil, "本机 SOCKS5") + " · " + address, Proxy: connectcfg.Proxy{Mode: "socks5", Address: address}}, nil
}

func (a *app) tunnelHasDefaultHop() bool {
	s := &a.tunnels
	if s.baseRoute.Temporary() {
		return s.baseRoute.Mode != "direct"
	}
	for _, p := range a.connections {
		if p.ID == s.host {
			return p.Network.RouteID != "" || p.Network.Relay != "" || p.Network.Proxy.Address != ""
		}
	}
	return false
}

func (a *app) tunnelRouteOptions() []connectionOption {
	s := &a.tunnels
	defaultLabel := "使用默认线路"
	if s.baseRoute.Temporary() {
		defaultLabel = "使用当前会话线路"
	}
	options := []connectionOption{{"", defaultLabel}, {"direct", "本次直连"}, {"local-socks", "本机已有 SOCKS5"}}
	for _, route := range a.liveConnectionRoutes(s.host) {
		options = append(options, connectionOption{route.route.Key(), route.label})
	}
	for _, r := range a.routes {
		options = append(options, connectionOption{"saved:" + r.ID, r.Name})
	}
	return options
}

func (a *app) tunnelRoutesMenu(c *ui.Context, m *ui.Menu, options []connectionOption) {
	s := &a.tunnels
	for i, option := range options {
		if i == 3 {
			m.Separator()
		}
		if m.Item(tr(c, option.label)).ID("route:" + option.value).Chosen() {
			s.routeChoice = option.value
			if s.routeChoice == "local-socks" {
				if s.localProxy == "" {
					s.localProxy = "127.0.0.1:1080"
				}
				if s.kind == "socks5" && s.listen == s.localProxy && s.listen == "127.0.0.1:1080" {
					s.listen = "127.0.0.1:1081"
				}
			}
		}
	}
	if relays := a.relayServerOptions(s.host); len(relays) > 0 {
		m.Separator()
		relayServerMenu(c, m, relays, func(route connectcfg.RouteSelection) {
			s.routeChoice = route.Key()
		})
	}
}
