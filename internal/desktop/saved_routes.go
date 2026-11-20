package desktop

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/egoist/mygo/ui"
	"sshclient/internal/connectcfg"
	"sshclient/internal/desktop/components"
	"sshclient/internal/proxyconfig"
	"sshclient/internal/vault"
)

type savedRouteEditor struct {
	value                                                       vault.SavedRoute
	mode                                                        string
	existing, hasPassword, replacePassword, selectAfter, saving bool
	sequence                                                    uint64
	error                                                       string
	importText, selectedNode                                    string
	nodes                                                       []proxyconfig.Node
	reimport                                                    bool
}

func (a *app) connectionRoutePicker(c *ui.Context) {
	options := []connectionOption{{"", "直连 / 连接专用配置"}}
	found := a.form.Network.RouteID == ""
	for _, r := range a.routes {
		options = append(options, connectionOption{r.ID, r.Name})
		found = found || r.ID == a.form.Network.RouteID
	}
	if !found {
		options = append(options, connectionOption{a.form.Network.RouteID, "线路不可用，请重新选择"})
	}
	if optionSelect(c, "默认线路", &a.form.Network.RouteID, options) && a.form.Network.RouteID != "" {
		a.form.Network.Proxy, a.form.Network.Relay = connectcfg.Proxy{}, ""
		a.connection.replaceProxy = false
	}
	ui.Row(c).FillWidth().Gap(8).Wrap().Children(func() {
		if components.Button(c, tr(c, "新建并选用线路")).Clicked() {
			a.editSavedRoute(nil, true)
		}
		for _, r := range a.routes {
			if r.ID == a.form.Network.RouteID {
				if components.Button(c, tr(c, "编辑所选线路")).Clicked() {
					a.editSavedRoute(&r, false)
				}
			}
		}
	})
	for _, r := range a.routes {
		if r.ID == a.form.Network.RouteID {
			ui.Text(c, a.savedRouteDescription(c, r)).FontSize(12).FillWidth().TextColor(c.Theme().TextMuted)
		}
	}
}

func (a *app) savedRouteDescription(c *ui.Context, r vault.RouteSummary) string {
	if r.Relay != "" {
		for _, p := range a.connections {
			if p.ID == r.Relay {
				return tr(c, "SSH 跳板机") + " · " + p.Name
			}
		}
		return tr(c, "SSH 跳板机不可用，请编辑线路")
	}
	return proxyconfig.Label(r.Proxy.Mode) + " · " + r.Proxy.Address
}

func (a *app) routesSettings(c *ui.Context) {
	if a.routeError != "" {
		ui.Text(c, reviewText(a.routeError)).FillWidth().FontSize(12).TextColor(c.Theme().Danger)
	}
	ui.Row(c).FillWidth().Children(func() {
		if components.Button(c, tr(c, "新建线路"), components.ButtonOptions{Variant: components.Primary}).Clicked() {
			a.editSavedRoute(nil, false)
		}
	})
	if len(a.routes) == 0 {
		ui.Text(c, tr(c, "暂无已保存线路")).FillWidth().FontSize(13).TextColor(c.Theme().TextMuted)
	}
	for i, r := range a.routes {
		c.Key(r.ID)
		ui.Column(c).FillWidth().Padding(12, 0).Gap(6).Children(func() {
			ui.Text(c, r.Name).FontSize(13).Bold().FillWidth()
			ui.Text(c, a.savedRouteDescription(c, r)).FontSize(12).FillWidth().TextColor(c.Theme().TextMuted)
			if r.References > 0 {
				ui.Text(c, fmt.Sprintf(tr(c, "%d 个连接使用"), r.References)).FontSize(11).FillWidth().TextColor(c.Theme().TextMuted).Tooltip(tr(c, "删除前需解除这些连接的引用。"))
			}
			ui.Row(c).FillWidth().Gap(8).Wrap().Children(func() {
				if components.Button(c, tr(c, "编辑")).Label(tr(c, "编辑线路：") + r.Name).Clicked() {
					a.editSavedRoute(&r, false)
				}
				if components.Button(c, tr(c, "删除"), components.ButtonOptions{Disabled: r.References > 0, Variant: components.Quiet}).TextColor(c.Theme().Danger).Label(tr(c, "删除线路：") + r.Name).Clicked() {
					a.removeSavedRoute(r)
				}
			})
		})
		if i < len(a.routes)-1 {
			ui.Box(c).FillWidth().Height(1).Background(c.Theme().Border)
		}
	}
}

func (a *app) editSavedRoute(summary *vault.RouteSummary, selectAfter bool) {
	a.routeError = ""
	s := &savedRouteEditor{value: vault.SavedRoute{ID: vault.ShortID()}, mode: "import", selectAfter: selectAfter, sequence: a.connection.sequence}
	if summary != nil {
		s.value = vault.SavedRoute{ID: summary.ID, Name: summary.Name, Proxy: summary.Proxy.Public(), Relay: summary.Relay}
		s.existing, s.hasPassword = true, summary.HasPassword
		s.mode = summary.Proxy.Mode
		if proxyconfig.Supported(s.mode) {
			s.mode = "import"
		}
		if summary.Relay != "" {
			s.mode = "relay"
		}
	}
	a.cancelFileProperties()
	a.cancelDockerDialog()
	a.prompt = promptState{open: true, host: a.currentDialogHost(), route: s, generation: a.generation}
	a.invalidateWindows()
}

func (a *app) savedRouteDialog(c *ui.Context) {
	s := a.prompt.route
	title := tr(c, "新建线路")
	if s.existing {
		title = tr(c, "编辑线路")
	}
	_, windowHeight := c.Size()
	open := true
	components.Dialog(c, &open, components.DialogOptions{Title: title, Width: 560, Close: func() {
		if !s.saving {
			a.prompt = promptState{}
		}
	}, CloseDisabled: s.saving}, func() {
		ui.Scroll(c).FillWidth().MaxHeight(max(120, windowHeight-144)).Children(func() {
			ui.Column(c).FillWidth().Gap(components.FormGap).Disabled(s.saving).Children(func() {
				ui.Field(c, tr(c, "线路名称"), func() {
					ui.TextInput(c, &s.value.Name).FillWidth().Height(components.FormHeight).Label(tr(c, "线路名称")).AutoFocus()
				})
				if optionSelect(c, "线路类型", &s.mode, []connectionOption{{"import", "导入代理（链接 / JSON）"}, {"socks5", "SOCKS5"}, {"http", "HTTP CONNECT"}, {"https", "HTTPS CONNECT"}, {"relay", "SSH 跳板机"}}) {
					s.error = ""
					s.nodes, s.importText, s.selectedNode = nil, "", ""
					s.value.Proxy, s.value.Relay = connectcfg.Proxy{}, ""
					s.hasPassword, s.replacePassword, s.reimport = false, true, false
					if s.mode != "relay" && s.mode != "import" {
						s.value.Proxy.Mode = s.mode
					}
				}
				if s.mode == "import" {
					a.savedRouteImport(c, s)
				} else if s.mode == "relay" {
					options := []connectionOption{{"", "请选择跳板机"}}
					for _, p := range a.connections {
						options = append(options, connectionOption{p.ID, p.Name + " · " + p.ID})
					}
					optionSelectHint(c, "跳板机", &s.value.Relay, options, "只使用已保存并核对指纹的服务器；每跳独立验证，最多 4 跳。")
				} else {
					ui.Field(c, tr(c, "代理主机:端口"), func() {
						ui.TextInput(c, &s.value.Proxy.Address).FillWidth().Height(components.FormHeight).Label(tr(c, "线路代理地址"))
					})
					ui.Field(c, tr(c, "代理用户名"), func() {
						ui.TextInput(c, &s.value.Proxy.User).FillWidth().Height(components.FormHeight).Label(tr(c, "线路代理用户名"))
					})
					if s.hasPassword {
						action := "keep"
						if s.replacePassword {
							action = "replace"
						}
						if optionSelect(c, "已存代理密码", &action, []connectionOption{{"keep", "保留已存密码"}, {"replace", "更换或清空密码"}}) {
							s.replacePassword = action == "replace"
							if !s.replacePassword {
								s.value.Proxy.Password = ""
							}
						}
					}
					if !s.hasPassword || s.replacePassword {
						ui.Field(c, tr(c, "代理密码"), func() {
							ui.TextInput(c, &s.value.Proxy.Password).Password().FillWidth().Height(components.FormHeight).Label(tr(c, "线路代理密码"))
						})
					}
				}
				if s.error != "" {
					ui.Text(c, s.error).FillWidth().FontSize(12).TextColor(c.Theme().Danger)
				}
			})
		})
	}, func() {
		if components.Button(c, tr(c, "取消"), components.ButtonOptions{Disabled: s.saving}).Clicked() {
			a.prompt = promptState{}
		}
		label := tr(c, "保存线路")
		if s.saving {
			label = tr(c, "正在保存")
		}
		pendingImport := s.mode == "import" && (!proxyconfig.Supported(s.value.Proxy.Mode) || s.reimport || len(s.nodes) > 0 || s.importText != "")
		if components.Button(c, label, components.ButtonOptions{Variant: components.Primary, Disabled: s.saving || pendingImport}).Tooltip(tr(c, "线路统一保存并复用；修改后下次连接使用最新配置，当前会话不受影响。")).Clicked() {
			a.saveRouteEditor(s)
		}
	})
	if !open && !s.saving {
		a.prompt = promptState{}
	}
}

func (a *app) saveRouteEditor(s *savedRouteEditor) {
	if !a.online || a.prompt.generation != a.generation || s.saving {
		return
	}
	value := s.value
	value.Name = strings.TrimSpace(value.Name)
	if s.mode == "import" && (!proxyconfig.Supported(value.Proxy.Mode) || s.reimport || s.importText != "" || len(s.nodes) > 0) {
		s.error = tr(nil, "请先解析并选用一个代理节点")
		return
	}
	keep := s.hasPassword && !s.replacePassword && s.mode != "relay"
	if err := value.Validate(); err != nil {
		s.error = err.Error()
		return
	}
	owner, err := a.unlockedOwner()
	if err != nil {
		s.error = err.Error()
		return
	}
	s.saving = true
	cli, epoch := a.client, a.epoch.Load()
	go func() {
		ctx, cancel := context.WithTimeout(a.ctx, 10*time.Second)
		defer cancel()
		err := cli.SaveRoute(ctx, value, keep, false, owner)
		var routes []vault.RouteSummary
		if err == nil {
			routes, err = cli.ListRoutes(ctx, owner)
		}
		a.postUI(func() {
			if !a.online || a.epoch.Load() != epoch || a.prompt.route != s {
				return
			}
			s.saving = false
			if err != nil {
				s.error = err.Error()
				return
			}
			a.routes = routes
			a.connection.routeMeasurements = nil
			a.connection.measuredNetwork = ""
			if s.selectAfter && a.connection.sequence == s.sequence {
				a.form.Network.RouteID = value.ID
				a.form.Network.Proxy, a.form.Network.Relay = connectcfg.Proxy{}, ""
				a.connection.replaceProxy = false
			}
			a.prompt = promptState{}
		})
	}()
}

func (a *app) removeSavedRoute(r vault.RouteSummary) {
	a.routeError = ""
	a.ask(tr(nil, "删除线路"), r.Name, tr(nil, "删除后无法恢复。正在被连接引用的线路不能删除。"), "", "", tr(nil, "删除"), func(string) {
		owner, err := a.unlockedOwner()
		if err != nil {
			a.routeError = err.Error()
			return
		}
		cli, epoch := a.client, a.epoch.Load()
		go func() {
			ctx, cancel := context.WithTimeout(a.ctx, 10*time.Second)
			defer cancel()
			err := cli.SaveRoute(ctx, vault.SavedRoute{ID: r.ID}, false, true, owner)
			a.postUI(func() {
				if !a.online || a.epoch.Load() != epoch {
					return
				}
				if err != nil {
					a.routeError = err.Error()
					return
				}
				for i, v := range a.routes {
					if v.ID == r.ID {
						a.routes = append(a.routes[:i], a.routes[i+1:]...)
						break
					}
				}
			})
		}()
	})
	a.prompt.danger = true
}
