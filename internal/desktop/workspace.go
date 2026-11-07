package desktop

import (
	"context"
	"fmt"
	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"
	"path"
	"sshclient/internal/desktop/components"
	"sshclient/internal/localipc"
)

func (a *app) refreshAuth() {
	if a.auth == nil {
		return
	}
	auth, ctx, dir, gen := a.auth, a.ctx, a.dir, a.generation
	go func() {
		st := auth.Status(ctx, dir)
		a.postUI(func() {
			if a.auth == auth && a.generation == gen {
				a.authStatus = st
			}
		})
	}()
}
func (a *app) connectionTree(c *ui.Context) { a.connectionSidebar(c, false) }
func size(n uint64) string                  { return components.Bytes(n) }

func (a *app) addTransfer(direction, local, remote string) {
	host := a.selected
	a.work(func() (func(), error) {
		j, e := a.transfers.Add(host, direction, local, remote)
		return func() { a.jobs = a.transfers.List(); a.runTransfer(j.ID) }, e
	})
}
func (a *app) runTransfer(id string) {
	owner, err := a.unlockedOwner()
	if err != nil {
		a.status = err.Error()
		return
	}
	a.status = ""
	gen, manager, ctx := a.generation, a.transfers, a.operationContext()
	go func() {
		e := manager.RunWithOwner(ctx, id, tr(nil, "桌面文件传输"), owner)
		a.postUI(func() {
			if gen != a.generation {
				return
			}
			if e != nil {
				a.status = e.Error()
			} else {
				a.status = tr(nil, "传输完成")
			}
			a.jobs = a.transfers.List()
			if e == nil {
				for _, j := range a.jobs {
					if j.ID == id && j.State == "done" {
						a.transferArrived(j)
					}
				}
			}
		})
	}()
}
func (a *app) transferView(c *ui.Context) {
	a.transferViewWithState(c, &a.transferTable)
}

func (a *app) transferViewWithState(c *ui.Context, state *ui.ListState) {
	state.Key = func(i int) any { return a.jobs[i].ID }
	state.Label = func(i int) string { return path.Base(a.jobs[i].Remote) }
	if len(a.jobs) == 0 {
		ui.Column(c).FillWidth().Grow(1).MinHeight(0).Center().Gap(12).Children(func() {
			ui.Icon(c, components.TransfersIcon).Size(28, 28).TextColor(c.Theme().TextMuted)
			ui.Text(c, tr(c, "暂无传输任务")).FontSize(12).TextColor(c.Theme().TextMuted)
		})
		return
	}
	components.List(c, state, len(a.jobs), func(i int) {
		j := a.jobs[i]
		ui.Column(c).FillWidth().Padding(8, 10).Gap(4).Tooltip(j.Local + " ↔ " + j.Remote).Children(func() {
			ui.Row(c).FillWidth().Gap(8).AlignItems(ui.Center).Children(func() {
				ui.Text(c, reviewText(path.Base(j.Remote))).SingleLine().Grow(1).FontSize(13)
				if j.State == "running" || j.State == "waiting" {
					if components.Button(c, tr(c, "暂停")).Clicked() {
						a.transfers.Pause(j.ID)
					}
				} else if j.State != "done" {
					if components.Button(c, tr(c, "继续")).Label(tr(c, "继续 / 重试")).Clicked() {
						a.runTransfer(j.ID)
					}
				}
			})
			direction := tr(c, "上传至 ")
			if j.Direction == "download" {
				direction = tr(c, "下载自 ")
			}
			ui.Text(c, direction+reviewText(a.hostName(j.Host))).SingleLine().FontSize(11).TextColor(c.Theme().TextMuted)
			progress := float64(j.Bytes) / float64(max(j.Size, 1))
			if j.State == "done" {
				progress = 1
			}
			ui.Progress(c, progress).FillWidth()
			ui.Text(c, fmt.Sprintf("%s · %s / %s · %s/s", transferState(j.State), size(uint64(max(j.Bytes, 0))), size(uint64(max(j.Size, 0))), size(uint64(max(j.Speed, 0))))).SingleLine().FontSize(10).TextColor(c.Theme().TextMuted)
			if j.Directory {
				label := fmt.Sprintf(tr(c, "文件夹 · %d / %d 个文件"), j.FilesDone, j.FilesTotal)
				if j.Phase == "scanning" && j.State == "running" {
					label = fmt.Sprintf(tr(c, "正在扫描文件夹…已找到 %d 个文件"), j.FilesTotal)
				}
				ui.Text(c, label).SingleLine().FontSize(11).TextColor(c.Theme().TextMuted)
			}
			if j.Error != "" {
				ui.Text(c, reviewText(j.Error)).SingleLine().FontSize(11).TextColor(c.Theme().Danger).Tooltip(j.Error)
			}
		})
	}).FillWidth().Grow(1).MinHeight(0).Label(tr(c, "传输队列"))
}
func (a *app) securityView(c *ui.Context) {
	a.idleSettings(c)
	name := a.authStatus.Name
	if name == "" {
		name = tr(c, "系统验证")
	}
	ui.Column(c).FillWidth().Padding(16).Radius(8).Gap(14).Background(c.Theme().Surface).Label(tr(c, "安全与解锁设置")).Children(func() {
		ui.Row(c).FillWidth().Gap(12).AlignItems(ui.Center).Children(func() {
			ui.Text(c, name).FontSize(15).Bold().Grow(1)
			state := tr(c, "未启用")
			if a.authStatus.Enabled {
				state = tr(c, "已启用")
			} else if !a.authStatus.Available {
				state = tr(c, "不可用")
			}
			ui.Text(c, state).FontSize(12).TextColor(c.Theme().TextMuted).Shrink(0)
		})
		if (!a.authStatus.Available || !a.authStatus.Enabled) && a.authStatus.Reason != "" {
			ui.Column(c).FillWidth().Label(tr(c, "系统验证可用性")).Children(func() {
				ui.Text(c, a.authStatus.Reason).FillWidth().FontSize(12)
			})
		}
		if a.authStatus.Available || a.authStatus.Enabled {
			ui.Field(c, tr(c, "确认主密码"), func() {
				ui.TextInput(c, &a.ownerPass).Password().FillWidth().Height(36).Label(tr(c, "主密码确认设置")).Placeholder(tr(c, "更改安全设置前验证"))
			})
		}
		components.ActionGroup(c, ui.Start, func() {
			if components.Button(c, tr(c, "重新检测")).Height(36).Disabled(a.busy).Clicked() {
				a.refreshAuth()
			}
			ui.Spacer(c).Grow(1).MinWidth(0)
			if a.authStatus.Enabled {
				if components.Button(c, tr(c, "停用 ")+name).Height(36).Disabled(a.busy || a.ownerPass == "").Clicked() {
					a.configureAuth(false)
				}
			} else if a.authStatus.Available {
				if components.PrimaryButton(c, tr(c, "启用 ")+name).Height(36).Disabled(a.busy || a.ownerPass == "").Clicked() {
					a.configureAuth(true)
				}
			}
		}).Wrap()
		if a.authCancel != nil && components.Button(c, tr(c, "取消验证")).Height(36).Clicked() {
			a.authCancel()
		}
		if a.status != "" {
			ui.Column(c).FillWidth().Label(tr(c, "安全设置反馈")).Children(func() {
				ui.Text(c, reviewText(a.status)).FillWidth().FontSize(12).TextColor(c.Theme().TextMuted)
			})
		}
	})
	if a.server != nil {
		pin := a.server.PublicKeyPin()
		ui.Column(c).FillWidth().Padding(16).Radius(8).Gap(10).Background(c.Theme().Surface).Label(tr(c, "本次代理身份")).Children(func() {
			ui.Text(c, tr(c, "本机代理身份与 CLI 配对")).FontSize(15).Bold()
			ui.Text(c, pin).FillWidth().Selectable().SingleLine().Ellipsis("…").Tooltip(pin).FontSize(12).Label(tr(c, "代理公钥指纹"))
			ui.Text(c, tr(c, "代理身份随保险库保持稳定，首次可信解锁后自动配对；锁定或重启无需再复制指纹。客户端会话仍绑定进程与活连接，断线后重新授权。")).FillWidth().FontSize(12).TextColor(c.Theme().TextMuted)
			if components.Button(c, tr(c, "配对 / 修复本机 CLI")).Disabled(a.busy).Clicked() {
				server, dir := a.server, a.dir
				a.ask(tr(nil, "信任此本机代理？"), pin, tr(nil, "只更新当前用户的代理身份信任，不授予服务器访问权限。已有不同身份的配对会被替换。"), "", "", tr(nil, "确认配对"), func(string) {
					a.work(func() (func(), error) {
						current, err := server.Vault.BrokerPublicKeyPin()
						if err != nil {
							return nil, err
						}
						if err := localipc.PairBroker(dir, current, true); err != nil {
							return nil, err
						}
						return func() { a.status = tr(nil, "本机 CLI 已配对，之后会自动验证代理身份") }, nil
					})
				})
			}
			if components.Button(c, tr(c, "复制代理公钥")).Clicked() {
				c.WriteClipboard(pin)
			}
		})
	}
}

func (a *app) configureAuth(enable bool) {
	if a.auth == nil || a.busy {
		return
	}
	auth, dir := a.auth, a.dir
	ctx, finish := a.authContext()
	pass := []byte(a.ownerPass)
	a.ownerPass = ""
	a.work(func() (func(), error) {
		defer wipe(pass)
		defer finish()
		var e error
		if enable {
			e = auth.Enroll(ctx, dir, pass)
		} else {
			e = auth.Disable(dir, pass)
		}
		return func() { a.refreshAuth(); a.status = tr(nil, "系统验证设置已更新") }, e
	})
}

func chooseUpload(w *mygo.Window) ([]string, error) {
	return mygo.Dialog.Open(mygo.OpenDialogOptions{Parent: w, Title: tr(nil, "选择上传文件")})
}

func (a *app) authContext() (context.Context, func()) {
	if a.authCancel != nil {
		a.authCancel()
	}
	ctx, cancel := context.WithCancel(a.operationContext())
	a.authSequence++
	seq := a.authSequence
	a.authCancel = cancel
	return ctx, func() {
		cancel()
		a.postUI(func() {
			if a.authSequence == seq {
				a.authCancel = nil
			}
		})
	}
}

func (a *app) switchHost(id string) {
	if a.page == 0 {
		a.cancelConnectionEditor()
		if a.page == 0 {
			return
		}
	}
	if id != a.selected {
		a.selected = id
		a.files = nil
		a.remotePath = "/"
		a.listedPath = "/"
		a.fileSelected = -1
		a.editShown = false
	}
}
