package desktop

import (
	"context"
	"fmt"
	"path/filepath"
	"sort"
	"time"

	"github.com/egoist/mygo/ui"
	"sshclient/internal/approval"
	"sshclient/internal/desktop/components"
	"sshclient/internal/identitymodel"
	"sshclient/internal/protocol"
)

func idlePolicyLabel(seconds int) string {
	if seconds == 0 {
		return tr(nil, "闲置不撤销")
	}
	return fmt.Sprintf(tr(nil, "闲置 %d 分钟撤销"), seconds/60)
}

func idlePolicyControl(c *ui.Context, seconds int, disabled bool, choose func(int)) {
	components.Button(c, idlePolicyLabel(seconds)).Disabled(disabled).Label(tr(c, "调整闲置期限")).Menu(func(m *ui.Menu) {
		for _, n := range []int{0, 60, 300, 900, 1800} {
			if m.Item(idlePolicyLabel(n)).Chosen() {
				choose(n)
			}
		}
	})
}

func grantApplicant(r approval.Request) string {
	if r.Peer.Path != "" {
		return reviewText(filepath.Base(r.Peer.Path))
	}
	if r.Peer.Bundle != "" {
		return reviewText(r.Peer.Bundle)
	}
	if r.OwnerIssued {
		return tr(nil, "本机所有者")
	}
	if r.Operation.Client != "" {
		return tr(nil, "自报 · ") + reviewText(r.Operation.Client)
	}
	return tr(nil, "未记录")
}

func (a *app) setGrantPolicy(r approval.Request, change approval.GrantPolicyChange) {
	if a.busy {
		return
	}
	owner, err := a.unlockedOwner()
	if err != nil {
		a.status = err.Error()
		return
	}
	ctx, cancel := context.WithTimeout(a.operationContext(), 5*time.Second)
	cli, gen := a.client, a.generation
	change.ID, change.Nonce = r.ID, r.Nonce
	a.busy = true
	go func() {
		defer cancel()
		err := cli.SetGrantPolicy(ctx, change, owner)
		a.postUI(func() {
			if a.generation != gen {
				return
			}
			a.busy = false
			if err != nil {
				a.status = err.Error()
				return
			}
			a.status = tr(nil, "授权规则已更新")
			a.refreshAuthorizationRequests()
		})
	}()
}

func (a *app) grantMediumReviewSwitch(c *ui.Context, r approval.Request, label string) {
	mediumReviewControl(c, label, r.ConfirmsMediumReview(), a.busy, func(confirm bool) {
		a.setGrantPolicy(r, approval.GrantPolicyChange{MediumReview: &confirm})
	})
}

func (a *app) refreshAuthorizationRequests() {
	if a.busy || !a.online {
		return
	}
	owner, err := a.unlockedOwner()
	if err != nil {
		a.status = err.Error()
		return
	}
	ctx, cancel := context.WithTimeout(a.operationContext(), 5*time.Second)
	gen, cli := a.generation, a.client
	a.busy = true
	go func() {
		defer cancel()
		page, err := cli.CallWithOwner(ctx, protocol.Message{Action: "pending"}, owner)
		a.postUI(func() {
			if a.generation != gen || !a.online {
				return
			}
			a.busy = false
			if err != nil {
				a.status = err.Error()
				return
			}
			a.requests = page.Requests
		})
	}()
}

func (a *app) approvalActiveView(c *ui.Context) {
	var grants, operations []approval.Request
	for _, r := range a.requests {
		if r.Status != "approved" && r.Status != "running" {
			continue
		}
		if r.Operation.Kind == "grant" {
			grants = append(grants, r)
		} else {
			operations = append(operations, r)
		}
	}
	if len(grants) > 0 {
		a.selectedRequest = grants[0].ID
	} else if len(operations) > 0 {
		a.selectedRequest = operations[0].ID
	} else {
		a.selectedRequest = ""
	}
	sort.Slice(grants, func(i, j int) bool { return grants[i].Expires.Before(grants[j].Expires) })
	sort.Slice(operations, func(i, j int) bool { return operations[i].ID < operations[j].ID })
	ui.Row(c).FillWidth().Gap(8).AlignItems(ui.Center).Children(func() {
		ui.Text(c, tr(c, "当前授权")).Grow(1).Bold().FontSize(16)
		if components.Button(c, tr(c, "刷新授权")).Disabled(a.busy || a.identities.busy).Clicked() {
			a.refreshAuthorizationRequests()
			a.refreshIdentities(nil)
		}
	})
	ui.Scroll(c).Key("active-authorizations").Fill().MinWidth(0).Children(func() {
		ui.Column(c).FillWidth().MinWidth(0).Gap(12).Children(func() {
			ui.Text(c, fmt.Sprintf(tr(c, "服务器操作授权 · %d"), len(grants))).FontSize(13).Bold()
			if len(grants) == 0 {
				ui.Text(c, tr(c, "暂无有效服务器授权")).FontSize(12).TextColor(c.Theme().TextMuted)
			}
			for _, r := range grants {
				c.Key(r.ID)
				ui.Column(c).FillWidth().MinWidth(0).Padding(12).Radius(8).Border(1, c.Theme().Border).Gap(6).Label(tr(c, "当前服务器授权：") + r.ID).Children(func() {
					ui.Row(c).FillWidth().MinWidth(0).Gap(8).Wrap().Children(func() {
						ui.Text(c, grantApplicant(r)+" → "+reviewText(a.approvalServerName(r))).Grow(1).MinWidth(0).Bold().FontSize(13)
						ui.Text(c, r.Expires.Local().Format("01-02 15:04")+tr(c, " 到期")).FontSize(12).TextColor(c.Theme().TextMuted)
					})
					reviewRow(c, tr(c, "连接地址"), r.Operation.Endpoint)
					permission := grantAuditPermission(r.Operation.Grant.Permission)
					reviewRow(c, tr(c, "权限"), permission)
					if r.Operation.Grant.Permission == "guarded" {
						a.grantMediumReviewSwitch(c, r, tr(c, "确认中风险文件操作"))
					}
					reviewRow(c, tr(c, "读取范围"), r.Operation.Grant.ReadRoot)
					reviewRow(c, tr(c, "写入范围"), r.Operation.Grant.WriteRoot)
					idle := r.Operation.Grant.IdleSeconds
					if idle == r.Operation.Grant.LifetimeSeconds {
						idle = 0
					}
					if r.Activity != nil {
						idle = r.Activity.IdleSeconds
						reviewRow(c, tr(c, "最近使用"), r.Activity.LastUsed.Local().Format("01-02 15:04:05"))
					}
					ui.Row(c).FillWidth().Gap(8).Wrap().Children(func() {
						idlePolicyControl(c, idle, a.busy, func(n int) { a.setGrantPolicy(r, approval.GrantPolicyChange{IdleSeconds: n}) })
						if components.Button(c, tr(c, "查看审计"), components.ButtonOptions{Variant: components.Quiet}).Clicked() {
							a.showRelatedAudit(r.ID, "")
						}
						if components.Button(c, tr(c, "立即撤销"), components.ButtonOptions{Variant: components.Destructive}).Disabled(a.busy).Label(tr(c, "撤销授权：") + r.ID).Clicked() {
							a.decide(r.ID, false, true)
						}
					})
				})
			}
			ui.Text(c, tr(c, "SSH 密钥授权")).FontSize(13).Bold()
			if a.identities.error != "" {
				ui.Text(c, reviewText(a.identities.error)).FontSize(12).TextColor(c.Theme().Danger)
			}
			if !a.identities.loaded {
				label := "读取密钥授权"
				if a.identities.busy {
					label = "正在读取 SSH 密钥…"
				}
				if components.Button(c, tr(c, label)).Disabled(a.identities.busy).Clicked() {
					a.refreshIdentities(nil)
				}
			} else if len(a.identities.state.Leases) == 0 {
				ui.Text(c, tr(c, "暂无有效密钥限时授权")).FontSize(12).TextColor(c.Theme().TextMuted)
			}
			for _, lease := range a.identities.state.Leases {
				if lease.Expires <= time.Now().UnixMilli() || lease.IdleSeconds > 0 && lease.LastUsed+int64(lease.IdleSeconds)*1000 <= time.Now().UnixMilli() {
					continue
				}
				key := identitymodel.Key{ID: lease.Key, Name: lease.Key}
				for _, k := range a.identities.state.Keys {
					if k.ID == lease.Key {
						key = k
						break
					}
				}
				c.Key(lease.ID)
				ui.Column(c).FillWidth().MinWidth(0).Padding(12).Radius(8).Border(1, c.Theme().Border).Gap(6).Label(tr(c, "当前密钥授权：") + lease.ID).Children(func() {
					ui.Row(c).FillWidth().MinWidth(0).Gap(8).Wrap().Children(func() {
						ui.Text(c, key.Name).Grow(1).Bold().FontSize(13)
						ui.Text(c, time.UnixMilli(lease.Expires).Local().Format("01-02 15:04")+tr(c, " 到期")).FontSize(12).TextColor(c.Theme().TextMuted)
					})
					ui.Text(c, key.Fingerprint).FillWidth().FontSize(11).Selectable()
					ui.Text(c, tr(c, "允许本机应用使用此密钥签名")).FontSize(12).TextColor(c.Theme().TextMuted)
					reviewRow(c, tr(c, "最近使用"), time.UnixMilli(lease.LastUsed).Local().Format("01-02 15:04:05"))
					ui.Row(c).FillWidth().Gap(8).Wrap().Children(func() {
						idlePolicyControl(c, lease.IdleSeconds, a.identities.busy, func(n int) {
							a.refreshIdentities(&identitymodel.Change{Action: "lease-policy", ID: lease.Key, LeaseID: lease.ID, IdleSeconds: n})
						})
						if components.Button(c, tr(c, "查看审计"), components.ButtonOptions{Variant: components.Quiet}).Clicked() {
							a.showRelatedAudit("", key.Fingerprint)
						}
						if components.Button(c, tr(c, "立即撤销"), components.ButtonOptions{Variant: components.Destructive}).Disabled(a.identities.busy).Label(tr(c, "撤销密钥授权：") + lease.ID).Clicked() {
							a.refreshIdentities(&identitymodel.Change{Action: "revoke", ID: lease.Key, LeaseID: lease.ID})
						}
					})
				})
			}
			if len(operations) > 0 {
				ui.Text(c, fmt.Sprintf(tr(c, "单次许可与运行中的操作 · %d"), len(operations))).FontSize(13).Bold()
				for _, r := range operations {
					c.Key(r.ID)
					ui.Row(c).FillWidth().MinWidth(0).Gap(8).Padding(10).Radius(6).Background(c.Theme().Surface).Children(func() {
						ui.Column(c).Grow(1).MinWidth(0).Gap(4).Children(func() {
							ui.Text(c, grantApplicant(r)+" · "+operationTitle(r.Operation.Kind)).FontSize(13)
							ui.Text(c, reviewText(r.Operation.Endpoint)).FontSize(12).TextColor(c.Theme().TextMuted)
						})
						if components.Button(c, tr(c, "撤销并断开")).Disabled(a.busy).Clicked() {
							a.decide(r.ID, false, true)
						}
					})
				}
			}
		})
	})
}
