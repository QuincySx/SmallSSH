package desktop

import (
	"context"
	"errors"
	"fmt"

	"github.com/egoist/mygo/ui"
	"sshclient/internal/approval"
	"sshclient/internal/connectcfg"
	"sshclient/internal/desktop/components"
)

type tunnelRun struct {
	host, kind, address, status string
	cancel                      context.CancelFunc
	running                     bool
}
type tunnelManager struct {
	open                                bool
	host, kind, listen, target, message string
	active                              []*tunnelRun
}

func (a *app) openTunnels(host string) {
	if !a.online || host == "" {
		a.status = tr(nil, "请先选择已保存的连接")
		return
	}
	a.tunnels.open = true
	a.tunnels.host = host
	if a.tunnels.kind == "" {
		a.tunnels.kind = "local"
		a.tunnels.listen = "127.0.0.1:0"
		a.tunnels.target = "127.0.0.1:80"
	}
}
func (a *app) tunnelsView(c *ui.Context) {
	state := &a.tunnels
	if !state.open || !a.online {
		return
	}
	components.Dialog(c, &state.open, components.DialogOptions{Title: tr(c, "SSH 隧道"), Target: a.hostName(state.host), Width: 680, Height: 620, Close: func() { state.open = false }}, func() {
		ui.Column(c).FillWidth().Gap(10).Children(func() {
			optionSelect(c, "隧道类型", &state.kind, []connectionOption{{"local", "本地 → SSH 服务器侧目标"}, {"remote", "SSH 服务器监听 → 本机侧目标"}, {"socks5", "本地 SOCKS5 → SSH 服务器侧任意 TCP 目标"}})
			ui.Field(c, tr(c, "监听地址"), func() { ui.TextInput(c, &state.listen).FillWidth().Label(tr(c, "隧道监听地址输入")) }).Description(tr(c, "仅限明确的回环 IP；端口 0 自动分配，例如 127.0.0.1:0"))
			if state.kind != "socks5" {
				ui.Field(c, tr(c, "目标主机:端口"), func() { ui.TextInput(c, &state.target).FillWidth().Label(tr(c, "隧道目标地址输入")) })
			}
			ui.Text(c, tr(c, "回环监听可被同一机器上的其他进程使用。SOCKS5 可访问任意 TCP 目标；不支持 UDP / BIND。隧道后台运行，锁定、退出、撤销或停止会关闭监听和已接入连接。")).FontSize(12).FillWidth().TextColor(c.Theme().TextMuted)
			if state.message != "" {
				ui.Text(c, reviewText(state.message)).FillWidth().TextColor(c.Theme().Danger)
			}
			if components.Button(c, tr(c, "启动隧道"), components.ButtonOptions{Variant: components.Primary}).Clicked() {
				a.startTunnel()
			}
			for _, run := range state.active {
				ui.Row(c).FillWidth().Gap(10).AlignItems(ui.Center).Children(func() {
					ui.Column(c).Grow(1).Children(func() {
						ui.Text(c, a.hostName(run.host)+" · "+run.kind+" · "+run.address).Selectable().FillWidth()
						ui.Text(c, reviewText(run.status)).FontSize(11).FillWidth()
					})
					if components.Button(c, tr(c, "停止")).Disabled(!run.running).Clicked() {
						run.cancel()
						run.status = tr(nil, "正在停止…")
					}
				})
			}
		})
	}, func() {
		if components.Button(c, tr(c, "关闭面板")).Clicked() {
			state.open = false
		}
	})
}
func (a *app) startTunnel() {
	state := &a.tunnels
	spec := connectcfg.Tunnel{Kind: state.kind, Listen: state.listen, Target: state.target}
	if spec.Kind == "socks5" {
		spec.Target = ""
	}
	if e := spec.Validate(); e != nil {
		state.message = e.Error()
		return
	}
	owner, e := a.unlockedOwner()
	if e != nil {
		state.message = e.Error()
		return
	}
	ctx, cancel := context.WithCancel(a.operationContext())
	gen, cli := a.generation, a.client
	row := &tunnelRun{host: state.host, kind: spec.Kind, address: spec.Listen, status: tr(nil, "正在启动…"), cancel: cancel, running: true}
	// Completed rows are bounded; active authority is never evicted.
	kept := state.active[:0]
	for _, old := range state.active {
		if old.running {
			kept = append(kept, old)
		}
	}
	if len(kept) >= 32 {
		cancel()
		state.message = tr(nil, "最多同时管理 32 条隧道，请先停止旧隧道")
		return
	}
	state.active = append(kept, row)
	state.message = ""
	go func() {
		defer cancel()
		stream, err := cli.StartWithOwner(ctx, approval.Operation{Host: row.host, Kind: "tunnel", Tunnel: spec}, owner)
		if err == nil {
			defer stream.Close()
			a.postUI(func() {
				if gen == a.generation {
					row.address = stream.ListenAddress
					row.status = tr(nil, "运行中")
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
				row.status = fmt.Sprint(err)
			}
		})
	}()
}
