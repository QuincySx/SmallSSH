package desktop

import (
	"fmt"

	"github.com/egoist/mygo/ui"
	"sshclient/internal/approval"
	"sshclient/internal/desktop/components"
)

const approvalCategoryWidth float32 = 152
const approvalContentPadding float32 = 14

// Category navigation remains available in its compact rail. Request navigation
// belongs to the content area, so collapsing categories never hides a target.
var approvalCategories = []struct {
	key, title string
	icon       *ui.SVG
}{
	{"pending", "待处理", components.PendingIcon},
	{"active", "活动", components.ActiveIcon},
	{"history", "历史", components.HistoryIcon},
}

func (a *app) selectApprovalCategory(index int) {
	if index < 0 || index >= len(approvalCategories) {
		index = 0
	}
	if a.approvalTab != index {
		a.clearInlineApproval()
		a.selectedRequest = ""
		if index == 2 {
			a.loadApprovalHistory(false)
		}
	}
	a.approvalTab = index
}

func (a *app) selectApprovalRequest(r approval.Request) {
	if a.selectedRequest != r.ID {
		a.clearInlineApproval()
	}
	a.selectedRequest = r.ID
}

func approvalContentIsCompact(width float32, collapsed bool) bool {
	navigationWidth := approvalCategoryWidth
	if collapsed {
		navigationWidth = components.RailWidth
	}
	return width-navigationWidth-2*approvalContentPadding < 760
}

func (a *app) approvalCategoryNavigation(c *ui.Context, collapsed bool) {
	label := tr(c, "授权分类")
	if collapsed {
		label += tr(c, "窄栏")
	}
	components.NavigationSidebar(c, "approval-category-navigation", label, approvalCategoryWidth, collapsed, func() {
		for index, category := range approvalCategories {
			if components.NavigationIcon(c, "approval-category:"+category.key, tr(c, category.title), category.icon, a.approvalTab == index, collapsed).
				Label(tr(c, "授权分类：") + tr(c, category.title)).Clicked() {
				a.selectApprovalCategory(index)
			}
		}
	})
}

func (a *app) approvalView(c *ui.Context) {
	a.selectApprovalCategory(a.approvalTab)
	collapsed := a.sidebarHidden
	width, _ := c.Size()
	compact := approvalContentIsCompact(width, collapsed)
	ui.Row(c).Key("approval-center").Fill().MinWidth(0).Gap(0).Label(tr(c, "授权页面")).Children(func() {
		a.approvalCategoryNavigation(c, collapsed)
		ui.Column(c).Key("approval-content").Fill().MinWidth(0).Padding(approvalContentPadding).Gap(12).Children(func() {
			if a.approvalTab == 2 {
				a.approvalHistoryView(c)
				return
			}
			items := []approval.Request{}
			for _, r := range a.requests {
				include := r.Status == "pending"
				if a.approvalTab == 1 {
					include = r.Status == "approved" || r.Status == "running"
				}
				if include {
					items = append(items, r)
				}
			}
			if len(items) == 0 {
				a.clearInlineApproval()
				a.selectedRequest = ""
				ui.Column(c).Fill().Center().Children(func() { ui.Text(c, tr(c, "暂无请求")).TextColor(c.Theme().TextMuted) })
				return
			}
			chosen := items[0]
			for _, r := range items {
				if r.ID == a.selectedRequest {
					chosen = r
					break
				}
			}
			a.selectApprovalRequest(chosen)
			if compact && len(items) > 1 {
				labels := make([]string, len(items))
				selected := ""
				for i, r := range items {
					labels[i] = fmt.Sprintf("%d. %s · %s", i+1, reviewText(r.Operation.Client), operationTitle(r.Operation.Kind))
					if r.ID == chosen.ID {
						selected = labels[i]
					}
				}
				c.Key("approval-request-selector")
				if ui.Select(c, &selected, labels).FillWidth().MinWidth(0).Label(tr(c, "选择授权请求")).Changed() {
					for index, label := range labels {
						if selected == label {
							chosen = items[index]
							a.selectApprovalRequest(chosen)
							break
						}
					}
				}
			}
			ui.Row(c).Key("approval-body").Fill().MinWidth(0).Gap(20).Children(func() {
				if !compact {
					c.Key("approval-request-list")
					ui.Scroll(c).Width(200).Shrink(0).FillHeight().Children(func() {
						ui.Column(c).FillWidth().Gap(6).Role(ui.RoleList).Label(tr(c, "授权请求列表")).Children(func() {
							for _, r := range items {
								caller := reviewText(r.Operation.Client)
								if r.OwnerIssued {
									caller = tr(c, "本机所有者")
								}
								server := a.approvalServerName(r)
								if server == "" {
									server = r.Operation.Host
								}
								detail := operationTitle(r.Operation.Kind) + " · " + reviewText(server)
								tip := caller + " · " + detail
								if r.Operation.Endpoint != "" {
									tip += " · " + reviewText(r.Operation.Endpoint)
								}
								if components.ListItem(c, r.ID, caller+" · "+detail, r.ID == chosen.ID).Tooltip(tip).Children(func() {
									ui.Column(c).FillWidth().MinWidth(0).Gap(4).Children(func() {
										ui.Text(c, caller).FillWidth().MinWidth(0).SingleLine().FontSize(13).Bold()
										ui.Text(c, detail).FillWidth().MinWidth(0).SingleLine().FontSize(11).TextColor(c.Theme().TextMuted)
									})
								}).Clicked() {
									chosen = r
									a.selectApprovalRequest(r)
								}
							}
						})
					})
				}
				ui.Column(c).Key("approval-details").Label(tr(c, "授权详情区域")).Fill().MinWidth(0).Gap(14).Children(func() {
					c.Key("approval-review")
					ui.Scroll(c).FillWidth().Grow(1).Children(func() { requestReview(c, chosen, a.approvalServerName(chosen)) })
					if chosen.Status == "pending" {
						key := approvalRequestKey(chosen)
						if key != a.approvalReviewKey || a.inlineApproval == nil {
							a.clearInlineApproval()
							a.approvalReviewKey = key
							state := &approvalPromptState{host: a.win}
							a.inlineApproval = state
							reviewed := chosen
							a.bindApprovalFlow(reviewed, state, a.postUI, func() bool {
								return a.inlineApproval == state && a.approvalTab == 0 && a.selectedRequest == reviewed.ID && a.pendingApproval(reviewed) && (a.win == nil || a.online && a.page == 3 && a.approvalWin == nil)
							}, func() {
								a.status = tr(nil, "授权决定已处理")
								a.clearInlineApproval()
							})
						}
						state := a.inlineApproval
						a.approvalActions(c, chosen, state, func(allow, system bool) {
							state.start(chosen, allow, system)
						}, func() { a.clearInlineApproval(); a.page = 1 })
						a.ownerPass, a.approvalPasswordMode = state.password, state.passwordMode
					} else if chosen.Status == "running" || chosen.Status == "approved" {
						if components.Button(c, tr(c, "撤销并断开")).Height(36).Disabled(a.busy).Clicked() {
							a.decide(chosen.ID, false, true)
						}
					} else {
						ui.Text(c, requestState(chosen.Status)).TextColor(c.Theme().TextMuted)
					}
				})
			})
		})
	})
}
