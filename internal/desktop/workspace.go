package desktop

import (
	"context"
	"errors"
	"fmt"
	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"
	"path"
	"slices"
	"sshclient/internal/desktop/components"
	"sshclient/internal/localipc"
	"sshclient/internal/sshdata"
	"sshclient/internal/transfer"
)

func (a *app) refreshAuth() {
	if a.auth == nil {
		return
	}
	a.migrateLegacyDeviceUnlock()
	auth, ctx, dir, gen, store, preferred := a.auth, a.ctx, a.dir, a.generation, a.quickStore(), a.quickUnlockPreferred()
	go func() {
		st := auth.Status(ctx, dir)
		// Enabled now means the local service holds an envelope this
		// executable deposited, not that a file exists.
		if st.Available && preferred {
			if state, err := store.Status(ctx); err == nil {
				st.Enabled = state.Available
			}
		}
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
	a.runFileTransfer(id, nil)
}

func (a *app) runFileTransfer(id string, confirmed *uploadConflict) {
	owner, err := a.unlockedOwner()
	if err != nil {
		a.status = err.Error()
		return
	}
	a.status = ""
	gen, manager, ctx := a.generation, a.transfers, a.operationContext()
	cli, dialog := a.client, a.currentDialogHost()
	go func() {
		var e error
		if confirmed != nil {
			e = manager.ReplaceUploadWithOwner(ctx, confirmed.job, confirmed.originalHash, tr(nil, "桌面文件传输"), owner)
		} else {
			e = manager.RunWithOwner(ctx, id, tr(nil, "桌面文件传输"), owner)
		}
		var conflict uploadConflict
		if errors.Is(e, sshdata.ErrUploadTargetExists) {
			for _, job := range manager.List() {
				if job.ID == id && job.State == "error" {
					conflict, e = inspectUploadConflict(ctx, cli, owner, job, dialog)
					break
				}
			}
		}
		a.postUI(func() {
			if gen != a.generation {
				return
			}
			a.jobs = a.transfers.List()
			// A pause or cancel the user asked for is not an error to report.
			stopped := a.transferCancelling[id] || slices.ContainsFunc(a.jobs, func(j transfer.Job) bool { return j.ID == id && j.State == "paused" })
			if conflict.originalHash != "" && !stopped {
				a.uploadConflicts = append(a.uploadConflicts, conflict)
				a.showNextUploadConflict()
			} else if e != nil && !stopped {
				a.status = reviewText(tr(nil, e.Error()))
			} else if e == nil && !stopped {
				a.status = tr(nil, "传输完成")
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
	a.activityContent(c, &a.transferTable)
}

func (a *app) transferViewWithState(c *ui.Context, state *ui.ListState) {
	history := a.activityHistory[0] == 1
	jobs := a.visibleTransferJobs(history)
	state.Key = func(i int) any { return jobs[i].ID }
	state.Label = func(i int) string { return path.Base(jobs[i].Remote) }
	if len(jobs) == 0 {
		ui.Column(c).FillWidth().Grow(1).MinHeight(0).Center().Gap(12).Children(func() {
			ui.Icon(c, components.TransfersIcon).Size(28, 28).TextColor(c.Theme().TextMuted)
			text := tr(c, "暂无传输任务")
			if history {
				text = tr(c, "暂无已完成记录")
			}
			ui.Text(c, text).FontSize(12).TextColor(c.Theme().TextMuted)
		})
		return
	}
	components.List(c, state, len(jobs), func(i int) {
		j := jobs[i]
		cancelling := a.transferCancelling[j.ID]
		detail := j.Local + " ↔ " + j.Remote
		if j.Backup != "" {
			detail += "\n" + tr(c, "旧副本") + " · " + j.Backup
		}
		ui.Column(c).FillWidth().Padding(8, 10).Gap(4).Tooltip(reviewText(detail)).Children(func() {
			ui.Row(c).FillWidth().Gap(6).AlignItems(ui.Center).Children(func() {
				ui.Text(c, reviewText(path.Base(j.Remote))).SingleLine().Grow(1).MinWidth(0).FontSize(13)
				status, color := transferState(c, j.State), c.Theme().TextMuted
				switch {
				case cancelling:
					status = tr(c, "正在取消…")
				case j.State == "error":
					color = c.Theme().Danger
				case j.State == "running" && j.Size > 0:
					status = fmt.Sprintf("%d%%", min(100, j.Bytes*100/j.Size))
					color = c.Theme().Accent
				}
				if !history {
					ui.Text(c, status).SingleLine().Shrink(0).FontSize(11).TextColor(color)
				}
				a.transferRowActions(c, j, history, cancelling)
			})
			direction := tr(c, "上传至 ")
			if j.Direction == "download" {
				direction = tr(c, "下载自 ")
			}
			ui.Text(c, direction+reviewText(a.hostName(j.Host))).SingleLine().FontSize(11).TextColor(c.Theme().TextMuted)
			if history {
				ui.Text(c, transferState(c, j.State)+" · "+size(uint64(max(j.Size, 0)))).FontSize(11).TextColor(c.Theme().TextMuted)
				return
			}
			// A failed job that never moved has nothing to show in a bar; its
			// error line below says what happened.
			if j.State != "error" || j.Bytes > 0 {
				progress := float64(j.Bytes) / float64(max(j.Size, 1))
				ui.Progress(c, progress).FillWidth()
				detail := fmt.Sprintf("%s / %s", size(uint64(max(j.Bytes, 0))), size(uint64(max(j.Size, 0))))
				if j.State == "running" {
					detail += fmt.Sprintf(" · %s/s", size(uint64(max(j.Speed, 0))))
				}
				ui.Text(c, detail).SingleLine().FontSize(10).TextColor(c.Theme().TextMuted)
			}
			if j.Directory {
				label := fmt.Sprintf(tr(c, "文件夹 · %d / %d 个文件"), j.FilesDone, j.FilesTotal)
				if j.Phase == "scanning" && j.State == "running" {
					label = fmt.Sprintf(tr(c, "正在扫描文件夹…已找到 %d 个文件"), j.FilesTotal)
				}
				ui.Text(c, label).SingleLine().FontSize(11).TextColor(c.Theme().TextMuted)
			}
			if j.Error != "" {
				// Paused jobs keep an informational note; only failures are red.
				color := c.Theme().TextMuted
				if j.State == "error" {
					color = c.Theme().Danger
				}
				message := reviewText(tr(c, j.Error))
				ui.Text(c, message).SingleLine().FontSize(11).TextColor(color).Tooltip(message)
			}
		})
	}).FillWidth().Grow(1).MinHeight(0).Label(tr(c, "传输队列"))
}

func (a *app) visibleTransferJobs(history bool) []transfer.Job {
	jobs := make([]transfer.Job, 0, len(a.jobs))
	for i := len(a.jobs) - 1; i >= 0; i-- {
		if j := a.jobs[i]; (j.State == "done") == history {
			jobs = append(jobs, j)
		}
	}
	return jobs
}
func (a *app) securityView(c *ui.Context) {
	a.idleSettings(c)
	name := a.systemAuthenticationMethod(c).Label
	ui.Column(c).FillWidth().Padding(16).Radius(8).Gap(14).Background(c.Theme().Surface).Label(tr(c, "安全与解锁设置")).Children(func() {
		preferred := a.quickUnlockPreferred()
		ui.Row(c).FillWidth().Gap(12).AlignItems(ui.Center).Children(func() {
			ui.Text(c, fmt.Sprintf(tr(c, "用 %s 快速解锁"), name)).FontSize(15).Bold().Grow(1).MinWidth(0)
			state := tr(c, "下次用主密码解锁后生效")
			switch {
			case !a.authStatus.Available:
				state = tr(c, "不可用")
			case !preferred:
				state = tr(c, "已关闭")
			case a.authStatus.Enabled:
				state = tr(c, "已就绪")
			}
			ui.Text(c, state).FontSize(12).TextColor(c.Theme().TextMuted).Shrink(0)
		})
		ui.Text(c, fmt.Sprintf(tr(c, "锁定后可用 %s 解锁；开机、更新或 48 小时后需主密码。凭据只在本机后台内存中，不写入磁盘。"), name)).FillWidth().FontSize(12).TextColor(c.Theme().TextMuted)
		if !a.authStatus.Available && a.authStatus.Reason != "" {
			ui.Column(c).FillWidth().Label(tr(c, "系统验证可用性")).Children(func() {
				ui.Text(c, a.authStatus.Reason).FillWidth().FontSize(12)
			})
		}
		arm := a.authStatus.Available && (!preferred || !a.authStatus.Enabled)
		if arm {
			ui.Field(c, tr(c, "确认主密码"), func() {
				ui.TextInput(c, &a.ownerPass).Password().FillWidth().Height(36).Label(tr(c, "主密码确认设置")).Placeholder(tr(c, "开启快速解锁前验证"))
			})
		}
		components.ActionGroup(c, ui.Start, func() {
			if components.Button(c, tr(c, "重新检测")).Height(36).Disabled(a.busy).Clicked() {
				a.refreshAuth()
			}
			ui.Spacer(c).Grow(1).MinWidth(0)
			if preferred && a.authStatus.Available {
				if components.Button(c, tr(c, "关闭快速解锁")).Height(36).Disabled(a.busy).Clicked() {
					a.setQuickUnlock(false)
				}
			}
			if arm {
				label := tr(c, "开启快速解锁")
				if preferred {
					label = tr(c, "立即启用")
				}
				if components.PrimaryButton(c, label).Height(36).Disabled(a.busy || a.ownerPass == "").Clicked() {
					a.setQuickUnlock(true)
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
			if components.Button(c, tr(c, "配对 / 修复本机 CLI")).Disabled(a.busy).Clicked() {
				server, dir := a.server, a.dir
				a.ask(tr(nil, "信任此本机代理？"), pin, tr(nil, "只更新当前用户的代理身份信任，不授予服务器访问权限。已有不同身份的配对会被替换。"), "", "", tr(nil, "确认配对"), func(string) {
					a.work(func() (func(), error) {
						current, err := server.BrokerPublicKeyPin()
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

func chooseUpload(w *mygo.Window) ([]string, error) {
	return mygo.Dialog.Open(mygo.OpenDialogOptions{Parent: w, Title: tr(nil, "选择上传文件"), Multiple: true})
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
