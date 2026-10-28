package desktop

import (
	"fmt"

	"github.com/egoist/mygo/ui"
	"sshclient/internal/approval"
)

func compactApprovalTitle(r approval.Request) string {
	if r.Operation.Kind == "grant" {
		if r.Renewal {
			return tr(nil, "会话已过期，允许续期？")
		}
		return tr(nil, "允许访问服务器？")
	}
	return approvalHeading(r)
}

func approvalApplicant(r approval.Request) (name, note string) {
	if r.OwnerIssued {
		return tr(nil, "本机所有者"), ""
	}
	if r.Operation.Kind == "grant" {
		return reviewText(r.Operation.Client), tr(nil, "名称由申请端提供，请核对配对码")
	}
	return reviewText(r.Operation.Client), tr(nil, "名称由申请端提供，未经身份验证")
}

func (a *app) compactApprovalReview(c *ui.Context, r approval.Request) {
	op := r.Operation
	t := c.Theme()
	scoped := op.Kind == "grant" && (op.Grant.ReadRoot != "" || op.Grant.WriteRoot != "")
	gap := float32(12)
	if scoped {
		gap = 8
	}
	ui.Column(c).FillWidth().Gap(gap).Children(func() {
		ui.Column(c).FillWidth().Padding(14).Gap(10).Radius(10).
			Background(t.Surface).Border(1, t.Border.Alpha(.7)).Children(func() {
			target := op.Endpoint
			if target == "" {
				target = op.Host
			}
			ui.Column(c).FillWidth().Gap(3).Children(func() {
				name := a.approvalServerName(r)
				if name != "" && name != target {
					ui.Text(c, reviewText(name)).FillWidth().FontSize(13).Bold()
				}
				ui.Text(c, reviewText(target)).FillWidth().Selectable().FontSize(12).TextColor(t.TextMuted)
			})
			ui.Column(c).FillWidth().Padding(10, 0, 0).BorderWidth(1, 0, 0, 0).BorderColor(t.Border.Alpha(.7)).Gap(5).Children(func() {
				permission, actions, caution := approvalPermission(r)
				if permission != compactApprovalTitle(r) {
					ui.Text(c, permission).FontSize(13).Bold()
				}
				if actions != "" && !scoped {
					ui.Text(c, actions).FillWidth().FontSize(12).TextColor(t.TextMuted)
				}
				if caution != "" {
					color := t.TextMuted
					if op.Kind != "grant" || op.Grant.Permission != "read" {
						color = t.Warning
					}
					ui.Text(c, caution).FillWidth().FontSize(11).TextColor(color)
				}
				term := tr(c, "仅本次操作")
				if op.Kind == "grant" {
					term = fmt.Sprintf(tr(c, "最长 %s · 闲置 %s 后失效"), grantDuration(op.Grant.LifetimeSeconds), grantDuration(op.Grant.IdleSeconds))
				} else if r.OwnerSession {
					term = tr(c, "关闭终端、锁定或退出时结束")
				}
				ui.Text(c, term).FillWidth().FontSize(12)
				if op.Kind == "grant" {
					requestGrantRoots(c, op.Grant)
					if !scoped {
						ui.Text(c, tr(c, "锁定、退出或撤销后失效")).FillWidth().FontSize(11).TextColor(t.TextMuted)
					}
				}
			})
		})
		if op.Kind == "grant" {
			ui.Column(c).FillWidth().Gap(4).AlignItems(ui.Center).Children(func() {
				ui.Text(c, tr(c, "配对码")).FontSize(11).TextColor(t.TextMuted)
				ui.Text(c, op.Grant.PairingCode()).FillWidth().TextAlign(ui.Center).Selectable().Font("monospace").FontSize(14)
			})
		}
		requestOperationReview(c, r)
		open := ui.Local(c.Root(), "approval-compact-details", func() bool { return false })
		ui.Collapsible(c, tr(c, "技术详情"), open, func() { requestTechnicalReview(c, r) })
	})
}
