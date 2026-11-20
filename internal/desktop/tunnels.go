package desktop

import (
	"context"
	"errors"
	"net"
	"net/url"
	"slices"

	"github.com/egoist/mygo/ui"
	"sshclient/internal/approval"
	"sshclient/internal/client"
	"sshclient/internal/connectcfg"
	"sshclient/internal/desktop/components"
	"sshclient/internal/vault"
)

type tunnelRun struct {
	host, kind, address, status    string
	requestID                      string
	spec                           connectcfg.Tunnel
	route                          connectcfg.RouteSelection
	cancel                         context.CancelFunc
	running, failed, ready         bool
	purpose, webProtocol, urlError string
	openWebsite                    bool
}
type tunnelManager struct {
	open, showingServices, browse       bool
	host, kind, listen, target, message string
	routeChoice, localProxy             string
	webProtocol                         string
	drafts                              map[string]tunnelEndpoints
	baseRoute                           connectcfg.RouteSelection
	active                              []*tunnelRun
	latest                              *tunnelRun
}

func (a *app) closeTunnels() {
	for _, run := range a.tunnels.active {
		if run.cancel != nil {
			run.cancel()
		}
	}
	a.tunnels = tunnelManager{}
}
func (a *app) openTunnels(host string) {
	if !a.online {
		a.status = tr(nil, "请先选择已保存的连接")
		return
	}
	s := &a.tunnels
	for _, run := range s.active {
		run.openWebsite = false
	}
	s.open = true
	s.showingServices = false
	s.latest = nil
	s.browse = false
	s.message, s.kind, s.webProtocol = "", "", "auto"
	s.drafts = nil
	s.selectKind("local")
	s.routeChoice, s.localProxy = "", ""
	s.baseRoute = connectcfg.RouteSelection{}
	s.host = host
	if s.host == "" {
		for _, p := range a.connections {
			if p.ID == a.selected {
				s.host = p.ID
				break
			}
		}
	}
	if s.host == "" {
		if source := a.currentTerminal(); source != nil {
			for _, p := range a.connections {
				if p.ID == source.host {
					s.host = p.ID
					break
				}
			}
		}
	}
	if source := a.currentTerminal(); source != nil && source.host == s.host && a.liveRelay(source.requestID) == source {
		s.baseRoute = source.target.Route
	}
}
func (a *app) tunnelsView(c *ui.Context) {
	s := &a.tunnels
	if !s.open || !a.online {
		return
	}
	_, height := c.Size()
	title := "新建端口转发"
	if s.showingServices {
		title = "端口转发"
	}
	components.Dialog(c, &s.open, components.DialogOptions{Title: tr(c, title), Width: 720, Close: a.closeTunnelPanel}, func() {
		ui.Scroll(c).FillWidth().MaxHeight(max(160, height-160)).MinHeight(0).Children(func() {
			ui.Column(c).FillWidth().Gap(12).Children(func() {
				if s.message != "" {
					ui.Text(c, reviewText(s.message)).FillWidth().TextColor(c.Theme().Danger)
				}
				if s.showingServices {
					if components.Button(c, tr(c, "新建服务…"), components.ButtonOptions{Variant: components.Primary}).Clicked() {
						a.openTunnels(a.selected)
					}
				}
				for _, run := range slices.Clone(s.active) {
					if !s.showingServices && run != s.latest {
						continue
					}
					c.Key(run)
					ui.Row(c).FillWidth().MinWidth(0).Gap(8).AlignItems(ui.Center).Children(func() {
						ui.Column(c).Grow(1).MinWidth(0).Gap(4).Children(func() {
							ui.Text(c, a.tunnelServerName(run.host)+" · "+tr(c, run.label())).FillWidth().FontSize(12).Bold()
							if run.address != "" {
								label := "本机访问地址"
								if run.kind == "remote" {
									label = "服务器上的访问地址"
								}
								ui.Text(c, tr(c, label)).FontSize(11).TextColor(c.Theme().TextMuted)
								address := run.address
								if website := run.websiteURL(); website != "" {
									address = website
								}
								ui.Text(c, address).FillWidth().FontSize(13).Selectable()
							}
							status := ui.Text(c, reviewText(run.status)).FontSize(11).FillWidth()
							if run.failed {
								status.TextColor(c.Theme().Danger)
							}
						})
						if run.ready && run.running {
							a.tunnelWebsiteAction(c, run)
							if components.Button(c, tr(c, "复制入口")).Clicked() {
								address := run.address
								if website := run.websiteURL(); website != "" {
									address = website
								}
								c.WriteClipboard(address)
								a.status = tr(nil, "已复制入口地址")
							}
							if components.Button(c, tr(c, "停止")).Clicked() {
								stopTunnel(run)
							}
						} else if run.running {
							if components.Button(c, tr(c, "取消"), components.ButtonOptions{Variant: components.Quiet}).Clicked() {
								stopTunnel(run)
							}
						} else if components.SVGButton(c, components.CloseIcon, tr(c, "移除隧道记录 ")+run.address, tr(c, "移除记录")).Clicked() {
							if i := slices.Index(s.active, run); i >= 0 {
								s.active = slices.Delete(s.active, i, i+1)
							}
						}
					})
				}
				if s.showingServices {
					if len(s.active) == 0 {
						ui.Text(c, tr(c, "暂无运行中的服务")).FillWidth().TextColor(c.Theme().TextMuted)
					}
				} else {
					a.tunnelSetupView(c)
				}
			})
		})
	}, func() {
		if components.Button(c, tr(c, "关闭面板")).Clicked() {
			a.closeTunnelPanel()
		}
		if !s.showingServices && s.kind != "" {
			existing := a.reusableSimpleService()
			label := s.actionLabel()
			if existing != nil && !existing.ready {
				label = "正在准备…"
			}
			button := components.Button(c, tr(c, label), components.ButtonOptions{Variant: components.Primary}).
				Disabled(s.host == "" || existing != nil && !existing.ready)
			if button.Clicked() {
				if existing == nil {
					a.startTunnel()
				} else {
					s.showingServices = true
				}
			}
		}
	})
}

func (a *app) startTunnel() {
	s := &a.tunnels
	spec := connectcfg.Tunnel{Kind: s.kind, Listen: s.listen, Target: s.target}
	if spec.Kind == "socks5" {
		spec.Target = ""
	}
	if err := spec.Validate(); err != nil {
		s.message = err.Error()
		return
	}
	owner, err := a.unlockedOwner()
	if err != nil {
		s.message = err.Error()
		return
	}
	route := s.selectedRoute()
	var local *vault.SavedRoute
	if s.routeChoice == "local-socks" {
		r, err := a.localTunnelProxy(s.localProxy)
		if err != nil {
			s.message = err.Error()
			return
		}
		local = &r
		route = connectcfg.RouteSelection{Mode: "saved", ID: r.ID}
	}
	kept := s.active[:0]
	for _, old := range s.active {
		if old.running {
			kept = append(kept, old)
		}
	}
	if len(kept) >= 32 {
		s.message = tr(nil, "最多同时管理 32 条隧道，请先停止旧隧道")
		return
	}
	ctx, cancel := context.WithCancel(a.operationContext())
	purpose := s.purpose()
	row := &tunnelRun{purpose: purpose, webProtocol: s.webProtocol, openWebsite: purpose == "website", host: s.host, kind: spec.Kind, address: spec.Listen, spec: spec, route: route, status: tr(nil, "正在核对连接…"), cancel: cancel, running: true}
	s.active = append(kept, row)
	s.latest = row
	s.message = ""
	gen, cli := a.generation, a.client
	fail := func(err error) {
		cancel()
		if gen != a.generation {
			return
		}
		row.running = false
		row.failed = !errors.Is(err, context.Canceled)
		row.status = tr(nil, "已停止")
		if row.failed {
			row.status = connectionError(err)
			s.message = row.status
		}
	}
	connect := func() {
		a.ensureConnectionTrusted(ctx, row.host, row.route, func() { go a.runTunnel(ctx, row, cli, owner, gen) }, fail)
	}
	if local == nil {
		connect()
		return
	}
	// Saving this reusable local route is offline; only the subsequent explicit
	// tunnel start is permitted to contact its proxy or SSH destination.
	exists := false
	for _, r := range a.routes {
		exists = exists || r.ID == local.ID
	}
	if exists {
		connect()
		return
	}
	go func() {
		err := cli.SaveRoute(ctx, *local, false, false, owner)
		a.postUI(func() {
			if gen != a.generation || !a.online {
				return
			}
			if err != nil {
				fail(err)
				return
			}
			a.routes = append(a.routes, vault.RouteSummary{ID: local.ID, Name: local.Name, Proxy: local.Proxy.Public()})
			if ctx.Err() != nil {
				fail(ctx.Err())
				return
			}
			connect()
		})
	}()
}

func (a *app) runTunnel(ctx context.Context, row *tunnelRun, cli client.Client, owner client.OwnerProof, gen int) {
	defer row.cancel()
	ctx = client.WithRoute(ctx, row.host, row.route)
	stream, err := cli.StartWithOwner(ctx, approval.Operation{Host: row.host, Kind: "tunnel", Tunnel: row.spec}, owner)
	if err == nil {
		defer stream.Close()
		a.postUI(func() {
			if gen == a.generation && ctx.Err() == nil {
				row.address = stream.ListenAddress
				row.requestID = stream.RequestID
				row.status = tr(nil, "运行中")
				row.ready = true
				if a.tunnels.open && a.tunnels.latest == row {
					a.tunnels.showingServices = true
				} else {
					row.openWebsite = false
				}
			}
		})
		for {
			message, e := stream.Receive()
			if e != nil {
				if ctx.Err() == nil {
					err = e
				}
				break
			}
			if message.Error != "" {
				err = errors.New(message.Error)
				break
			}
			if message.Action == "done" {
				break
			}
		}
	}
	wasCanceled := ctx.Err() != nil
	a.postUI(func() {
		if gen != a.generation {
			return
		}
		row.running = false
		row.status = tr(nil, "已停止")
		if err != nil && !wasCanceled {
			row.status = connectionError(err)
			row.failed = true
		}
	})
}

func (run *tunnelRun) label() string {
	switch run.purpose {
	case "website":
		return "访问服务器网页"
	case "share":
		return "让服务器访问本机服务"
	case "service":
		return "访问远端服务"
	default:
		return tunnelKindLabel(run.kind)
	}
}
func (run *tunnelRun) websiteURL() string {
	if run.purpose != "website" || !run.ready || !run.running {
		return ""
	}
	host, _, err := net.SplitHostPort(run.address)
	if err != nil {
		return ""
	}
	ip := net.ParseIP(host)
	if ip == nil || !ip.IsLoopback() {
		return ""
	}
	scheme := "http"
	_, targetPort, _ := net.SplitHostPort(run.spec.Target)
	if run.webProtocol == "https" || (run.webProtocol == "auto" || run.webProtocol == "") && targetPort == "443" {
		scheme = "https"
	}
	return (&url.URL{Scheme: scheme, Host: run.address}).String()
}
func (a *app) tunnelWebsiteAction(c *ui.Context, run *tunnelRun) {
	address := run.websiteURL()
	if address == "" {
		return
	}
	open := func() {
		c.OpenURLThen(address, func(err error) {
			if err != nil {
				run.urlError = tr(nil, "浏览器未打开，可复制入口地址重试")
			} else {
				run.urlError = ""
			}
		})
	}
	if run.openWebsite {
		run.openWebsite = false
		open()
	}
	if a.tunnels.showingServices && components.Button(c, tr(c, "打开网页")).Clicked() {
		open()
	}
	if run.urlError != "" {
		ui.Text(c, run.urlError).FillWidth().FontSize(11).TextColor(c.Theme().Danger)
	}
}

func (a *app) closeTunnelPanel() {
	a.tunnels.open = false
	for _, run := range a.tunnels.active {
		run.openWebsite = false
	}
}

func (a *app) reusableSimpleService() *tunnelRun {
	s := &a.tunnels
	r := s.latest
	if r == nil || !r.running || s.routeChoice == "local-socks" {
		return nil
	}
	if r.host == s.host && r.purpose == s.purpose() && r.spec.Kind == s.kind && r.spec.Listen == s.listen && r.spec.Target == s.target && r.route == s.selectedRoute() && r.webProtocol == s.webProtocol {
		return r
	}
	return nil
}
