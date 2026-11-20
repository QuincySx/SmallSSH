package desktop

import (
	"fmt"

	"github.com/egoist/mygo/ui"
	"sshclient/internal/approval"
	"sshclient/internal/desktop/components"
)

func compactApprovalTitle(r approval.Request) string {
	if r.Operation.Kind == "grant" {
		if r.Operation.Grant.Permission == approval.MaintenancePermission {
			return approvalHeading(r)
		}
		if r.Renewal {
			if r.Renewals > 1 {
				return fmt.Sprintf(tr(nil, "会话已过期，允许续期？（连续第 %d 次）"), r.Renewals)
			}
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

func (a *app) compactApprovalReview(c *ui.Context, r approval.Request, lifetimes ...int) {
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
			details := ui.Local(c.Root(), "approval-card-details", func() bool { return false })
			ui.Row(c).FillWidth().Gap(8).AlignItems(ui.Start).Children(func() {
				ui.Column(c).Grow(1).MinWidth(0).Gap(3).Children(func() {
					name := a.approvalServerName(r)
					if name != "" && name != target {
						ui.Text(c, reviewText(name)).FillWidth().FontSize(13).Bold()
					}
					ui.Text(c, reviewText(target)).FillWidth().Selectable().FontSize(12).TextColor(t.TextMuted)
				})
				// One disclosure on the card, as password managers do: the
				// chevron opens the technical detail in place.
				chevron := components.SVGButton(c, components.ChevronDownIcon, tr(c, "技术详情")).Size(26, 26)
				if *details {
					chevron.Rotate(180)
				}
				if chevron.Clicked() {
					*details = !*details
					c.Invalidate()
				}
			})
			if *details {
				ui.Column(c).FillWidth().Gap(5).Children(func() { requestTechnicalReview(c, r) })
			}
			ui.Column(c).FillWidth().Padding(10, 0, 0).BorderWidth(1, 0, 0, 0).BorderColor(t.Border.Alpha(.7)).Gap(5).Children(func() {
				permission, actions, caution := approvalPermission(r)
				if permission != compactApprovalTitle(r) {
					ui.Text(c, permission).FontSize(13).Bold()
				}
				if actions != "" && (!scoped || op.Grant.Permission == approval.MaintenancePermission) {
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
					lifetime := op.Grant.LifetimeSeconds
					if len(lifetimes) > 0 && lifetimes[0] > 0 && r.Status == "pending" {
						lifetime = lifetimes[0]
					}
					idle := min(lifetime, op.Grant.IdleSeconds)
					if r.Status == "pending" {
						state := &approvalPromptState{}
						a.initializeApprovalGrant(r, state)
						if len(lifetimes) == 0 {
							lifetime = state.chosenLifetime(r)
						}
						idle = state.chosenIdle(r)
					}
					term = grantTermsText(c, lifetime, idle)
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
		requestOperationReview(c, r)
	})
}
