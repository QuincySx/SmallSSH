package desktop

import (
	"context"
	"fmt"
	"time"

	"github.com/egoist/mygo/ui"
	"sshclient/internal/auditlog"
	"sshclient/internal/desktop/components"
)

const (
	approvalHistoryCooldown        = 3 * time.Second
	approvalHistoryRefreshInterval = 10 * time.Second
)

type approvalHistoryState struct {
	events          []auditlog.Event
	next, before    string
	loading, loaded bool
	err             string
	lastAttempt     time.Time
	sequence        uint64
	cancel          context.CancelFunc
	table           ui.ListState
}

func (a *app) clearApprovalHistory() {
	state := &a.approvalHistory
	if state.cancel != nil {
		state.cancel()
	}
	a.approvalHistory = approvalHistoryState{sequence: state.sequence + 1}
}

// Leave a usable interval between automatic reads so manual refresh and older
// navigation are not continuously disabled by the shared request cooldown.
func (a *app) refreshApprovalHistoryIfDue() {
	state := &a.approvalHistory
	if state.before == "" && time.Since(state.lastAttempt) >= approvalHistoryRefreshInterval {
		a.loadApprovalHistory(false)
	}
}

// Only UI events and the existing visible-page poll schedule reads. Rendering
// never starts a repeating timer or waits for disk/network work.
func (a *app) loadApprovalHistory(older bool) {
	state := &a.approvalHistory
	now := time.Now()
	if state.loading || now.Sub(state.lastAttempt) < approvalHistoryCooldown {
		return
	}
	if !a.online {
		return
	}
	owner, err := a.unlockedOwner()
	if err != nil {
		state.loaded, state.err, state.lastAttempt = true, err.Error(), now
		return
	}
	before := ""
	if older {
		before = state.next
		if before == "" {
			return
		}
	}
	ctx, cancel := context.WithTimeout(a.operationContext(), 5*time.Second)
	state.cancel = cancel
	state.loading, state.lastAttempt = true, now
	state.sequence++
	seq, gen, cli := state.sequence, a.generation, a.client
	go func() {
		defer cancel()
		page, err := cli.History(ctx, auditlog.Query{Limit: 50, Before: before}, owner)
		a.postUI(func() {
			state := &a.approvalHistory
			if a.generation != gen || state.sequence != seq || !a.online {
				return
			}
			resetScroll := !state.loaded || state.before != before
			state.loading, state.loaded, state.cancel = false, true, nil
			if err != nil {
				state.err = tr(nil, "无法读取历史，请重试")
				if err.Error() == auditlog.ErrCursor.Error() {
					state.err, state.next = tr(nil, "历史分页已过期，请刷新历史"), ""
				}
				return
			}
			state.events, state.next, state.before, state.err = page.Events, page.Next, before, ""
			if resetScroll {
				state.table.ScrollTo(0, ui.Start)
			}
		})
	}()
}

func historyValue(value string) string {
	if value == "" {
		return tr(nil, "未记录")
	}
	return reviewText(value)
}

func historyApplicant(event auditlog.Event) string {
	if event.OwnerIssued {
		return tr(nil, "本机所有者")
	}
	if event.Client == "" {
		return tr(nil, "未记录")
	}
	return tr(nil, "自报 · ") + reviewText(event.Client)
}

func historyServer(event auditlog.Event) string {
	if event.HostName != "" {
		return historyValue(event.HostName)
	}
	return historyValue(event.Host)
}

func historyOperation(event auditlog.Event) string {
	switch event.Kind {
	case "policy":
		return tr(nil, "AI 访问")
	case "audit-history":
		return tr(nil, "访问历史")
	case "profilePut", "profileChange":
		return tr(nil, "保存连接")
	case "profileDetails":
		return tr(nil, "编辑连接")
	case "profileDelete":
		return tr(nil, "删除连接")
	case "renewalChallenge", "renewSession":
		return tr(nil, "申请续期")
	case "delegate", "ownerRequest", "decide", "revoke", "withdraw":
		return tr(nil, "访问授权")
	case "run":
		return tr(nil, "执行")
	}
	if event.Kind == "" {
		return tr(nil, "未记录")
	}
	return operationTitle(event.Kind)
}

func historyResult(event auditlog.Event) string {
	result := map[string]string{
		"unlocked": tr(nil, "已解锁"), "locked": tr(nil, "已锁定"),
		"approved": tr(nil, "已批准"), "denied": tr(nil, "已拒绝"),
		"revoked": tr(nil, "已撤销"), "withdrawn": tr(nil, "已撤回"),
		"delegated": tr(nil, "已授权执行"), "renewalRequested": tr(nil, "申请续期"),
		"started": tr(nil, "已开始"), "ended": tr(nil, "已结束"),
		"requested": tr(nil, "请求授权"), "expired": tr(nil, "已到期"), "policy_changed": tr(nil, "策略已修改"),
	}[event.Event]
	if result == "" {
		result = historyValue(event.Event)
	}
	if event.Reason != "" {
		reason := map[string]string{
			"automation_disabled": tr(nil, "AI 访问已关闭"), "server_disabled": tr(nil, "AI 访问已关闭"),
			"protected_path":         tr(nil, "关键路径受保护"),
			"uncontrolled_operation": tr(nil, "保护模式不开放此操作"),
			"scope_denied":           tr(nil, "超出授权范围"), "invalid_authority": tr(nil, "授权无效或已过期"),
			"invalid_policy":    tr(nil, "服务器策略无效"),
			"invalid_operation": tr(nil, "操作无效"), "owner_required": tr(nil, "需要本机所有者操作"),
		}[event.Reason]
		if reason == "" {
			reason = tr(nil, "操作未获允许")
		}
		result += " · " + reason
	}
	return result
}

func (a *app) approvalHistoryView(c *ui.Context) {
	state := &a.approvalHistory
	if !state.loaded && !state.loading && state.lastAttempt.IsZero() {
		a.loadApprovalHistory(false)
	}
	cooling := time.Since(state.lastAttempt) < approvalHistoryCooldown
	ui.Row(c).FillWidth().MinWidth(0).Gap(8).AlignItems(ui.Center).Children(func() {
		ui.Text(c, tr(c, "访问历史")).Grow(1).MinWidth(0).Bold().FontSize(16)
		if components.Button(c, tr(c, "刷新历史")).Disabled(state.loading || cooling).Clicked() {
			a.loadApprovalHistory(false)
		}
	})
	if state.err != "" {
		ui.Text(c, reviewText(state.err)).FillWidth().FontSize(12).TextColor(c.Theme().Danger)
	}
	if state.loading {
		ui.Text(c, tr(c, "正在读取历史…")).FillWidth().FontSize(12).TextColor(c.Theme().TextMuted)
	}
	if len(state.events) == 0 && state.next == "" {
		ui.Column(c).Fill().Center().Children(func() {
			if state.loaded && state.err == "" && !state.loading {
				ui.Text(c, tr(c, "暂无访问历史")).TextColor(c.Theme().TextMuted)
			}
		})
		return
	}
	state.table.Key = func(i int) any {
		e := state.events[i]
		return fmt.Sprintf("%s:%s:%s:%d", e.RequestID, e.Event, e.Generation, e.Time.UnixNano())
	}
	state.table.Label = func(i int) string {
		e := state.events[i]
		return historyValue(e.HostName) + " · " + historyResult(e)
	}
	width, _ := c.Size()
	navigationWidth := approvalCategoryWidth
	if a.sidebarHidden {
		navigationWidth = components.RailWidth
	}
	if width-navigationWidth-2*approvalContentPadding < 820 {
		c.Key("approval-history-summary-list")
		components.List(c, &state.table, len(state.events), func(row int) {
			e := state.events[row]
			ui.Column(c).FillWidth().MinWidth(0).Padding(10, 12).Gap(5).BorderWidth(0, 0, 1, 0).BorderColor(c.Theme().Border).Children(func() {
				ui.Row(c).FillWidth().MinWidth(0).Gap(10).AlignItems(ui.Start).Children(func() {
					ui.Text(c, historyServer(e)).Grow(1).MinWidth(0).Bold().FontSize(13)
					ui.Text(c, e.Time.Local().Format("01-02 15:04:05")).Shrink(0).FontSize(11).TextColor(c.Theme().TextMuted)
				})
				ui.Text(c, historyApplicant(e)).FillWidth().MinWidth(0).FontSize(12).TextColor(c.Theme().TextMuted)
				ui.Text(c, historyOperation(e)).FillWidth().MinWidth(0).FontSize(12)
				ui.Text(c, historyResult(e)).FillWidth().MinWidth(0).FontSize(12).TextColor(c.Theme().TextMuted)
			})
		}).FillWidth().Grow(1).MinWidth(0).Label(tr(c, "访问历史表格"))
	} else {
		c.Key("approval-history-table")
		components.Table(c, &state.table, []ui.TableColumn{
			{ID: "time", Title: tr(c, "时间"), Width: 132},
			{ID: "server", Title: tr(c, "服务器"), MinWidth: 110},
			{ID: "client", Title: tr(c, "申请方"), Width: 130},
			{ID: "operation", Title: tr(c, "操作"), Width: 140},
			{ID: "result", Title: tr(c, "结果"), MinWidth: 250},
		}, len(state.events), func(row, col int) {
			e := state.events[row]
			value := e.Time.Local().Format("01-02 15:04:05")
			switch col {
			case 1:
				value = historyServer(e)
			case 2:
				value = historyApplicant(e)
			case 3:
				value = historyOperation(e)
			case 4:
				value = historyResult(e)
			}
			components.CellText(c, value)
		}).FillWidth().Grow(1).MinWidth(0).Label(tr(c, "访问历史表格"))
	}
	ui.Row(c).FillWidth().MinWidth(0).Gap(8).AlignItems(ui.Center).Children(func() {
		ui.Text(c, fmt.Sprintf(tr(c, "本页 %d 条"), len(state.events))).Grow(1).FontSize(11).TextColor(c.Theme().TextMuted)
		if components.Button(c, tr(c, "更早记录")).Disabled(state.loading || cooling || state.next == "").Clicked() {
			a.loadApprovalHistory(true)
		}
	})
}
