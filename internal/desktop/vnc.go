package desktop

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/netip"
	"slices"
	"sshclient/internal/client"
	"strconv"
	"strings"
	"time"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/transfer"
	"github.com/egoist/mygo/ui"
	"sshclient/internal/approval"
	"sshclient/internal/connectcfg"
	"sshclient/internal/desktop/components"
	"sshclient/internal/vault"
	"sshclient/internal/vncview"
)

type vncRun struct {
	name          string
	view          vncEmbeddedView
	tab           *workspaceToolTab
	cancel        context.CancelFunc
	window        toolsWindow
	fullscreenOff func()
	wasFullscreen bool
}

type vncManager struct {
	fullscreen                         *vncRun
	loading                            bool
	loadCancel                         context.CancelFunc
	loadSequence                       int
	presentation                       string
	sequence                           int
	open                               bool
	host, address, port, mode, message string
	viewOnly                           bool
	pending                            *vncRun
	runs                               []*vncRun
}

func (a *app) openVNC(host string) {
	if !a.online || host == "" {
		return
	}
	state := &a.vnc
	fresh := state.host != host || state.address == ""
	if state.host != host {
		a.closeVNCDialog()
	}
	if state.host != host || state.address == "" {
		state.address, state.port, state.mode = "", "5900", "direct"
		for _, connection := range a.connections {
			if connection.ID == host {
				state.address = connection.Host
				break
			}
		}
	}
	state.host, state.open, state.message = host, true, ""
	if state.presentation == "" {
		state.presentation = "tab"
	}
	if fresh {
		a.loadVNCSettings(host)
	}
}

func (a *app) loadVNCSettings(host string) {
	owner, err := a.unlockedOwner()
	if err != nil {
		return
	}
	ctx, cancel := context.WithTimeout(a.operationContext(), 3*time.Second)
	a.vnc.loadCancel, a.vnc.loading = cancel, true
	a.vnc.loadSequence++
	sequence, gen, cli := a.vnc.loadSequence, a.generation, a.client
	go func() {
		defer cancel()
		settings, err := cli.VNCSettings(ctx, host, owner)
		a.postUI(func() {
			if gen != a.generation || sequence != a.vnc.loadSequence || host != a.vnc.host || !a.vnc.open {
				return
			}
			a.vnc.loading, a.vnc.loadCancel = false, nil
			if err == nil && settings.Address != "" {
				if address, port, e := net.SplitHostPort(settings.Address); e == nil {
					a.vnc.address, a.vnc.port, a.vnc.mode = address, port, settings.Mode
				}
			}
		})
	}()
}

func (a *app) closeVNCDialog() {
	state := &a.vnc
	state.open = false
	state.loadSequence++
	if state.loadCancel != nil {
		state.loadCancel()
		state.loadCancel = nil
	}
	state.loading = false
	if state.pending != nil {
		state.pending.cancel()
		state.pending = nil
	}
}

func (a *app) vncDialog(c *ui.Context) {
	state := &a.vnc
	if !a.online || !state.open {
		return
	}
	components.Dialog(c, &state.open, components.DialogOptions{Title: tr(c, "VNC 远程桌面"), Width: 520, Close: a.closeVNCDialog}, func() {
		ui.Column(c).FillWidth().Gap(12).Children(func() {
			ui.Form(c, func() {
				optionSelect(c, "连接方式", &state.mode, []connectionOption{{"direct", "直接连接 VNC"}, {"ssh", "通过 SSH 通道"}})
				ui.Field(c, tr(c, "服务器地址"), func() {
					ui.Row(c).FillWidth().Gap(components.FormGap).AlignItems(ui.Center).Children(func() {
						ui.TextInput(c, &state.address).Grow(1).MinWidth(72).Height(components.FormHeight).Label(tr(c, "VNC 主机地址")).Placeholder(tr(c, "IP 地址或域名"))
						ui.Text(c, tr(c, "端口")).SingleLine().Shrink(0).TextColor(c.Theme().TextMuted)
						ui.TextInput(c, &state.port).Width(64).Height(components.FormHeight).Label(tr(c, "VNC 端口"))
					})
				})
			}).FillWidth().Disabled(state.pending != nil || state.loading)
			ui.Checkbox(c, &state.viewOnly, tr(c, "只读查看")).Disabled(state.pending != nil || state.loading)
			if components.Button(c, tr(c, "清除保存的 VNC 登录信息")).Disabled(state.pending != nil || state.loading).Clicked() {
				a.clearVNCCredentials()
			}
			if state.pending != nil || state.loading {
				text := tr(c, "正在打开 VNC 远程桌面…")
				if state.mode == "ssh" {
					text = tr(c, "正在建立 SSH 通道…")
				}
				ui.Text(c, text).FontSize(12)
			}
			if state.message != "" {
				ui.Text(c, reviewText(state.message)).FillWidth().Label(tr(c, "VNC 连接错误")).TextColor(c.Theme().Danger)
			}
		})
	}, func() {
		if components.Button(c, tr(c, "取消")).Clicked() {
			a.closeVNCDialog()
		}
		if components.Button(c, tr(c, "连接远程桌面"), components.ButtonOptions{Variant: components.Primary}).Disabled(state.pending != nil || state.loading).Clicked() {
			a.startVNC()
		}
	})
}

func (a *app) startVNC() {
	state := &a.vnc
	if state.pending != nil || state.loading || !a.online {
		return
	}
	if len(state.runs) >= 8 {
		state.message = tr(nil, "最多打开 8 个远程桌面，请先关闭已有标签或窗口")
		return
	}
	if a.newVNCWindow == nil && a.newVNCView == nil && a.win != nil && !mygo.WebViewAvailable() {
		state.message = tr(nil, "系统没有可用的 WebView，暂时无法打开 VNC 远程桌面")
		return
	}
	target, err := vncTarget(state.address, state.port)
	if err != nil {
		state.message = err.Error()
		return
	}
	spec := connectcfg.Tunnel{Kind: "local", Listen: "127.0.0.1:0", Target: target}
	if err := spec.Validate(); err != nil {
		state.message = err.Error()
		return
	}
	if state.mode != "direct" && state.mode != "ssh" {
		state.message = tr(nil, "请选择 VNC 连接方式")
		return
	}
	var owner client.OwnerProof
	if state.mode == "ssh" {
		owner, err = a.unlockedOwner()
		if err != nil {
			state.message = err.Error()
			return
		}
	}
	ctx, cancel := context.WithCancel(client.WithRoute(a.operationContext(), state.host, connectcfg.RouteSelection{}))
	run := &vncRun{cancel: cancel}
	state.pending, state.message = run, ""
	gen, cli, host, viewOnly, mode := a.generation, a.client, state.host, state.viewOnly, state.mode
	presentation := state.presentation
	credentialOwner, credentialErr := a.unlockedOwner()
	name := a.hostName(host) + " · VNC"
	run.name = name
	go func() {
		defer cancel()
		var stream *client.Stream
		var viewer *vncview.Session
		var err error
		if mode == "ssh" {
			stream, err = cli.StartWithOwner(ctx, approval.Operation{Host: host, Kind: "tunnel", Tunnel: spec}, owner)
			if err == nil {
				defer stream.Close()
				viewer, err = vncview.Start(ctx, stream.ListenAddress, name, viewOnly)
			}
		} else {
			viewer, err = vncview.StartDirect(ctx, target, name, viewOnly)
		}
		if err != nil {
			a.finishVNC(run, gen, ctx, err)
			return
		}
		defer viewer.Close()
		if credentialErr == nil {
			lookupCtx, lookupCancel := context.WithTimeout(ctx, 2*time.Second)
			settings, _ := cli.VNCSettings(lookupCtx, host, credentialOwner)
			lookupCancel()
			value := vncview.Credentials{Remember: true}
			if settings.Address == target && settings.Mode == mode {
				value.Username, value.Password = settings.Username, settings.Password
			}
			viewer.ConfigureCredentials(value, func(saveCtx context.Context, value vncview.Credentials) error {
				if ctx.Err() != nil {
					return ctx.Err()
				}
				settings := vault.VNCSettings{Address: target, Mode: mode}
				if value.Remember {
					settings.Username, settings.Password = value.Username, value.Password
				}
				return cli.SaveVNCSettings(saveCtx, host, settings, credentialOwner)
			})
		}
		viewer.ConfigureFullscreen(func(requestCtx context.Context) error {
			_, err := a.vncControl(requestCtx, ctx, run, gen, func() (string, error) {
				a.toggleVNCFullscreen(run)
				return "", nil
			})
			return err
		})
		if a.win != nil {
			viewer.ConfigureClipboard(func(requestCtx context.Context, write bool, text string) (string, error) {
				return a.vncControl(requestCtx, ctx, run, gen, func() (string, error) {
					if write {
						return "", mygo.Clipboard.Write(transfer.TextData(text))
					}
					return mygo.Clipboard.ReadText(), nil
				})
			})
		}
		a.postUI(func() {
			if gen != a.generation || ctx.Err() != nil || !a.online || a.vnc.pending != run {
				cancel()
				return
			}
			factory := a.newVNCWindow
			if factory == nil && a.win != nil {
				factory = newNativeVNCWindow
			}
			if presentation != "window" {
				if err := a.attachVNCTab(run, viewer.URL()); err != nil {
					a.vnc.pending = nil
					a.vnc.message = err.Error()
					cancel()
					return
				}
			} else if factory != nil {
				run.window = factory(mygo.WindowOptions{Title: name, Parent: a.win, URL: viewer.URL(), Width: 1100, Height: 760, MinWidth: 640, MinHeight: 360, Hidden: true}, func() { a.stopVNC(run) })
			}
			a.vnc.pending = nil
			if run.window == nil && run.view == nil {
				a.vnc.message = tr(nil, "无法打开远程桌面窗口")
				cancel()
				return
			}
			a.vnc.open = false
			a.vnc.runs = append(a.vnc.runs, run)
			if run.window != nil {
				a.showPresenceWindow(run.window)
			}
		})
		if stream == nil {
			<-ctx.Done()
			return
		}
		for {
			message, receiveErr := stream.Receive()
			if receiveErr != nil {
				err = receiveErr
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
		if err == nil {
			err = errors.New(tr(nil, "SSH 通道已结束，请重新连接"))
		}
		a.finishVNC(run, gen, ctx, err)
	}()
}

func vncTarget(address, port string) (string, error) {
	address, port = strings.TrimSpace(address), strings.TrimSpace(port)
	if strings.HasPrefix(address, "[") && strings.HasSuffix(address, "]") {
		address = address[1 : len(address)-1]
	}
	if strings.Contains(address, ":") {
		if _, err := netip.ParseAddr(address); err != nil {
			return "", errors.New(tr(nil, "VNC 主机请填写 IP 或域名，端口在右侧单独填写"))
		}
	}
	number, err := strconv.Atoi(port)
	if err != nil || number < 1 || number > 65535 {
		return "", errors.New(tr(nil, "VNC 端口需为 1–65535 之间的数字"))
	}
	target := net.JoinHostPort(address, strconv.Itoa(number))
	if err := connectcfg.Address(target, false); err != nil {
		return "", errors.New(tr(nil, "请填写有效的 VNC 主机地址"))
	}
	return target, nil
}

func (a *app) finishVNC(run *vncRun, generation int, ctx context.Context, err error) {
	wasCanceled := ctx.Err() != nil
	a.postUI(func() {
		if generation != a.generation {
			return
		}
		if a.vnc.pending == run {
			a.vnc.pending = nil
			if !wasCanceled {
				a.vnc.message = err.Error()
			}
			return
		}
		if !slices.Contains(a.vnc.runs, run) {
			return
		}
		// A disconnected viewer has no live transport to reconnect. Release its
		// window slot; the existing connection entry retains the chosen target.
		a.stopVNC(run)
		if !wasCanceled && a.online {
			a.status = fmt.Sprintf(tr(nil, "远程桌面 SSH 通道已断开：%s"), err)
		}
	})
}

func newNativeVNCWindow(options mygo.WindowOptions, closed func()) toolsWindow {
	// No extra trusted origin is granted. Release builds keep this page
	// outside Go RPC; MyGo development builds trust loopback origins.
	// Only our bundled page may navigate this viewer window.
	w := mygo.NewWindow(options)
	w.OnClosed(closed)
	restrictVNCPage(w.Page(), options.URL)
	return w
}

func (a *app) stopVNC(run *vncRun) {
	a.leaveVNCFullscreen(run)
	run.cancel()
	if run.view != nil {
		run.view.Destroy()
		run.view = nil
	}
	if run.tab != nil {
		tab := run.tab
		run.tab = nil
		a.closeWorkspaceTab(workspaceTab{tool: tab})
	}
	a.vnc.runs = slices.DeleteFunc(a.vnc.runs, func(other *vncRun) bool { return other == run })
	window := run.window
	run.window = nil
	if window != nil {
		window.Hide()
		window.Update(window.Close)
	}
}

func (a *app) closeVNCs() {
	a.closeVNCDialog()
	for _, run := range slices.Clone(a.vnc.runs) {
		a.stopVNC(run)
	}
}

func (a *app) clearVNCCredentials() {
	owner, err := a.unlockedOwner()
	if err != nil {
		a.vnc.message = err.Error()
		return
	}
	host, gen, cli, ctx := a.vnc.host, a.generation, a.client, a.operationContext()
	go func() {
		err := cli.SaveVNCSettings(ctx, host, vault.VNCSettings{}, owner)
		a.postUI(func() {
			if gen != a.generation || a.vnc.host != host || !a.vnc.open {
				return
			}
			if err != nil {
				a.vnc.message = err.Error()
			} else {
				a.vnc.message = tr(nil, "已清除保存的 VNC 登录信息")
			}
		})
	}()
}
