package desktop

import (
	"context"
	"fmt"
	"time"

	"github.com/egoist/mygo/ui"
	"sshclient/docs/design"
	"sshclient/internal/approval"
	"sshclient/internal/auditlog"
	"sshclient/internal/desktop/components"
)

const (
	approvalHistoryCooldown        = 3 * time.Second
	approvalHistoryRefreshInterval = 10 * time.Second
)

type approvalHistoryState struct {
	events                                        []auditlog.Event
	next, before                                  string
	loading, loaded                               bool
	err                                           string
	lastAttempt                                   time.Time
	sequence                                      uint64
	cancel                                        context.CancelFunc
	table                                         ui.ListState
	query                                         auditlog.Query
	summary                                       *auditlog.Summary
	search, category, application, result, period string
	exporting                                     bool
	// detail is the chosen row, -1 for none: the row the reader wants explained.
	detail        int
	viewportWidth float32
}

func (a *app) clearApprovalHistory() {
	state := &a.approvalHistory
	if state.cancel != nil {
		state.cancel()
	}
	a.approvalHistory = approvalHistoryState{sequence: state.sequence + 1, detail: -1}
}

// Leave a usable interval between automatic reads so manual refresh and older
// navigation are not continuously disabled by the shared request cooldown.
func (a *app) refreshApprovalHistoryIfDue() {
	state := &a.approvalHistory
	if state.detail < 0 && state.before == "" && time.Since(state.lastAttempt) >= approvalHistoryRefreshInterval {
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
	query := state.query
	query.Limit = 50
	query.Before = before
	query.Summary = before == ""
	go func() {
		defer cancel()
		page, err := cli.History(ctx, query, owner)
		a.postUI(func() {
			state := &a.approvalHistory
			if a.generation != gen || state.sequence != seq || !a.online {
				return
			}
			resetScroll := !state.loaded || state.before != before
			state.loading, state.loaded, state.cancel = false, true, nil
			if err != nil {
				state.err = tr(nil, "无法读取历史，请重试")
				if err.Error() == auditlog.ErrTampered.Error() {
					state.err = tr(nil, "审计历史无法验证：记录已被修改或删除")
				}
				if err.Error() == auditlog.ErrCursor.Error() {
					state.err, state.next = tr(nil, "历史分页已过期，请刷新历史"), ""
				}
				return
			}
			state.events, state.next, state.before, state.err = page.Events, page.Next, before, ""
			if page.Summary != nil {
				state.summary = page.Summary
			}
			state.detail = -1
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
	if event.Executable != "" || event.Bundle != "" || event.ExecutableID != "" {
		return reviewText(auditlog.ApplicationName(event))
	}
	if event.OwnerIssued {
		return tr(nil, "本机所有者")
	}
	if event.Client == "" {
		return tr(nil, "未记录")
	}
	return tr(nil, "自报 · ") + reviewText(event.Client)
}

func historyServer(event auditlog.Event) string {
	if event.KeyName != "" {
		return reviewText(event.KeyName)
	}
	if event.KeyFingerprint != "" {
		return historyValue(event.KeyFingerprint)
	}
	if event.HostName != "" {
		return historyValue(event.HostName)
	}
	return historyValue(event.Host)
}

func historyOperation(event auditlog.Event) string {
	switch event.Kind {
	case "host-trust":
		return tr(nil, "信任服务器身份")
	case "ssh-key-create":
		return tr(nil, "生成 SSH 密钥")
	case "ssh-key-delete":
		return tr(nil, "删除 SSH 密钥")
	case "ssh-key-persist":
		return tr(nil, "密钥限时授权")
	case "ssh-key-revoke", "ssh-key-clear":
		return tr(nil, "撤销密钥授权")
	case "ssh-key-lease-policy":
		return tr(nil, "调整密钥闲置规则")
	case "ssh-key-pending":
		return tr(nil, "查看签名请求")
	case "ssh-key-notifications":
		return tr(nil, "设置签名通知")
	case "system-probe":
		return tr(nil, "系统状态采样")
	case "ssh-key-sign":
		return tr(nil, "SSH 签名")
	case "ssh-key-authorization":
		return tr(nil, "密钥使用授权")
	case "grant-policy-change":
		return tr(nil, "调整授权规则")
	case "connections-reorder":
		return tr(nil, "调整连接顺序")
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
		return tr(nil, "服务器操作授权")
	case "run":
		return tr(nil, "执行")
	}
	if event.Kind == "" {
		return tr(nil, "未记录")
	}
	return operationTitle(event.Kind)
}

// historyDetail is the one-line answer to "what did it do": the command that
// ran, or the path it touched. A record that kept only its time and result is
// not something anyone can review.
func historyDetail(event auditlog.Event) string {
	switch {
	case event.Command != "":
		return reviewText(event.Command)
	case event.Path != "" && event.Destination != "":
		return reviewText(event.Path) + " → " + reviewText(event.Destination)
	case event.Path != "":
		return reviewText(event.Path)
	case event.Destination != "":
		return reviewText(event.Destination)
	case event.Object != "":
		return reviewText(event.Object)
	}
	return ""
}

func historyResult(event auditlog.Event) string {
	if event.Event == "ssh_signed" {
		switch event.Outcome {
		case "success":
			return tr(nil, "签名成功")
		case "cancelled":
			return tr(nil, "签名已取消")
		default:
			return tr(nil, "签名失败")
		}
	}
	if (event.Event == "ended" || event.Event == "system_ended") && event.Outcome != "" {
		result := map[string]string{"success": tr(nil, "执行成功"), "failed": tr(nil, "执行失败"), "cancelled": tr(nil, "已取消"), "unknown": tr(nil, "结果未确认")}[event.Outcome]
		if result == "" {
			result = tr(nil, "结果未确认")
		}
		if event.ExitCode != nil {
			result += fmt.Sprintf(tr(nil, " · 退出码 %d"), *event.ExitCode)
		}
		return result
	}
	result := map[string]string{
		"ssh_key_created": tr(nil, "密钥已生成"), "ssh_key_deleted": tr(nil, "密钥已删除"),
		"system_started": tr(nil, "系统采样已开始"),
		"ssh_requested":  tr(nil, "请求密钥签名"), "ssh_authorized": tr(nil, "密钥使用已批准"), "ssh_denied": tr(nil, "密钥使用已拒绝"), "ssh_signed": tr(nil, "签名完成"), "ssh_revoked": tr(nil, "密钥授权已撤销"), "ssh_expired": tr(nil, "密钥授权已到期"), "ssh_policy_changed": tr(nil, "密钥授权规则已修改"), "grant_policy_changed": tr(nil, "授权规则已修改"),
		"workspace_write": tr(nil, "提交变更"),
		"unlocked":        tr(nil, "已解锁"), "locked": tr(nil, "已锁定"),
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
			"idle_timeout": tr(nil, "闲置超时"), "duration_expired": tr(nil, "期限已到"), "cleared": tr(nil, "所有者撤销"),
			"automation_disabled": tr(nil, "AI 访问已关闭"), "server_disabled": tr(nil, "AI 访问已关闭"),
			"protected_path":         tr(nil, "关键路径受保护"),
			"uncontrolled_operation": tr(nil, "保护模式不开放此操作"),
			"high_risk_operation":    tr(nil, "高风险操作已拒绝"),
			"destructive_command":    tr(nil, "破坏性命令被护栏拦截"),
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

var auditDetailWidth = design.Length("size-audit-detail")

func (a *app) approvalHistoryView(c *ui.Context) {
	state := &a.approvalHistory
	if !state.loaded && !state.loading && state.lastAttempt.IsZero() {
		a.loadApprovalHistory(false)
	}
	cooling := time.Since(state.lastAttempt) < approvalHistoryCooldown
	ui.Row(c).FillWidth().MinWidth(0).Gap(8).AlignItems(ui.Center).Children(func() {
		ui.Text(c, tr(c, "审计记录")).Grow(1).MinWidth(0).Bold().FontSize(16)
		components.Button(c, tr(c, "导出")).Disabled(state.loading || state.exporting || !a.online).Menu(func(m *ui.Menu) {
			for _, format := range []string{"JSONL", "CSV"} {
				if m.Item(format).Chosen() {
					a.exportAuditHistory(format)
				}
			}
		})
		if components.Button(c, tr(c, "刷新历史")).Disabled(state.loading || cooling).Clicked() {
			a.loadApprovalHistory(false)
		}
	})
	a.approvalHistoryFilters(c)
	if state.summary != nil {
		ui.Text(c, fmt.Sprintf(tr(c, "批准 %d 次 · 完成操作 %d 次 · SSH 签名 %d 次 · 共 %d 条记录"), state.summary.Authorizations, state.summary.Operations, state.summary.Signatures, state.summary.Records)).FontSize(12).TextColor(c.Theme().TextMuted)
	}
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
	if state.detail >= len(state.events) {
		state.detail = -1
	}
	// A click chooses a row, and the detail below explains it: which command ran,
	// on which path. A chosen row is the whole point of the review surface.
	state.table.Selected = &state.detail
	chosen := -1
	if state.detail >= 0 && state.detail < len(state.events) {
		chosen = state.detail
	}

	width := state.viewportWidth
	if width == 0 {
		width, _ = c.Size()
		width -= approvalCategoryWidth + 2*approvalContentPadding
	}
	inspector := chosen >= 0 && width >= 780
	ui.Row(c).Key("audit-records-body").Fill().MinWidth(0).MinHeight(0).Gap(0).Label(tr(c, "审计记录区域")).Draw(func(_ *ui.Painter, r ui.Rect) {
		if state.viewportWidth != r.W {
			state.viewportWidth = r.W
			c.AnimationFrame()
		}
	}).Children(func() {
		listWidth := width
		if inspector {
			listWidth -= auditDetailWidth
		}
		a.approvalHistoryRecords(c, listWidth)
		if inspector {
			ui.Column(c).Width(auditDetailWidth).Shrink(0).FillHeight().MinHeight(0).BorderWidth(0, 0, 0, 1).BorderColor(c.Theme().Border).Label(tr(c, "审计详情侧栏")).Children(func() {
				a.auditDetailHeader(c)
				ui.Scroll(c).Key("audit-detail-scroll").Fill().MinHeight(0).Children(func() { a.approvalHistoryDetail(c, state.events[chosen]) })
			})
		}
	})
	if chosen >= 0 && !inspector {
		ui.Column(c).FillWidth().Height(200).Shrink(0).MinHeight(0).BorderWidth(1, 0, 0, 0).BorderColor(c.Theme().Border).Children(func() {
			a.auditDetailHeader(c)
			ui.Scroll(c).Key("audit-detail-scroll").Fill().MinHeight(0).Children(func() { a.approvalHistoryDetail(c, state.events[chosen]) })
		})
	}

	ui.Row(c).FillWidth().MinWidth(0).Shrink(0).Gap(8).AlignItems(ui.Center).Children(func() {
		ui.Text(c, fmt.Sprintf(tr(c, "本页 %d 条"), len(state.events))).Grow(1).MinWidth(0).FontSize(11).TextColor(c.Theme().TextMuted)
		if components.Button(c, tr(c, "更早记录")).Disabled(state.loading || cooling || state.next == "").Clicked() {
			a.loadApprovalHistory(true)
		}
	})
}

func (a *app) auditDetailHeader(c *ui.Context) {
	ui.Row(c).FillWidth().Padding(8, 12).Gap(8).AlignItems(ui.Center).Children(func() {
		ui.Text(c, tr(c, "详情")).Grow(1).Bold().FontSize(13)
		if components.SVGButton(c, components.CloseIcon, tr(c, "关闭详情"), "").Clicked() {
			a.approvalHistory.detail = -1
		}
	})
}

func (a *app) approvalHistoryRecords(c *ui.Context, width float32) {
	state := &a.approvalHistory
	restore := components.ReadableSelection(c)
	defer restore()
	if width < 780 {
		components.List(c, &state.table, len(state.events), func(row int) {
			e := state.events[row]
			ui.Column(c).FillWidth().MinWidth(0).Padding(10, 12).Gap(5).BorderWidth(0, 0, 1, 0).BorderColor(c.Theme().Border).Children(func() {
				ui.Row(c).FillWidth().MinWidth(0).Gap(8).Children(func() {
					ui.Text(c, historyOperation(e)).Grow(1).MinWidth(0).SingleLine().Bold().FontSize(13)
					ui.Text(c, e.Time.Local().Format("01-02 15:04:05")).Shrink(0).FontSize(11).TextColor(c.Theme().TextMuted)
				})
				ui.Row(c).FillWidth().MinWidth(0).Gap(10).Children(func() {
					ui.Text(c, historyServer(e)).Grow(1).MinWidth(0).SingleLine().FontSize(12)
					ui.Text(c, historyApplicant(e)).Grow(1).MinWidth(0).SingleLine().FontSize(12).TextColor(c.Theme().TextMuted)
				})
				ui.Row(c).FillWidth().MinWidth(0).Gap(10).Children(func() {
					ui.Text(c, historyDetail(e)).Grow(1).MinWidth(0).SingleLine().FontSize(11).TextColor(c.Theme().TextMuted).Tooltip(historyDetail(e))
					ui.Text(c, historyResult(e)).Width(design.Length("size-audit-compact-result")).Shrink(0).SingleLine().FontSize(11).TextColor(c.Theme().TextMuted).Tooltip(historyResult(e))
				})
			})
		}).Fill().MinWidth(0).Label(tr(c, "访问历史表格"))
		return
	}
	components.Table(c, &state.table, []ui.TableColumn{
		{ID: "time", Title: tr(c, "时间"), Width: 108},
		{ID: "server", Title: tr(c, "服务器"), MinWidth: 100},
		{ID: "client", Title: tr(c, "申请方"), MinWidth: 100},
		{ID: "operation", Title: tr(c, "操作"), Width: 110},
		{ID: "detail", Title: tr(c, "操作内容"), MinWidth: 160},
		{ID: "result", Title: tr(c, "结果"), MinWidth: 150},
	}, len(state.events), func(row, col int) {
		e := state.events[row]
		values := []string{e.Time.Local().Format("01-02 15:04:05"), historyServer(e), historyApplicant(e), historyOperation(e), historyDetail(e), historyResult(e)}
		components.CellText(c, values[col])
	}).Fill().MinWidth(0).Label(tr(c, "访问历史表格"))
}

// approvalHistoryDetail explains the chosen record: the command that ran at
// full length, the paths it touched, and the request it belongs to. The table
// shows a one-line excerpt; this is what the row cannot fit.
func (a *app) approvalHistoryDetail(c *ui.Context, e auditlog.Event) {
	ui.Column(c).Key("approval-history-detail").FillWidth().MinWidth(0).Padding(4, 12, 12, 12).
		Gap(design.Length("spacing-audit-detail-gap")).Children(func() {
		ui.Text(c, historyOperation(e)).FillWidth().FontSize(14).Bold()
		ui.Text(c, historyResult(e)).FillWidth().FontSize(12).TextColor(c.Theme().TextMuted)
		ui.Text(c, e.Time.Local().Format("2006-01-02 15:04:05 -0700")).FillWidth().FontSize(11).TextColor(c.Theme().TextMuted)
		reviewRow(c, tr(c, "服务器"), historyServer(e))
		reviewRow(c, tr(c, "申请方"), historyApplicant(e))
		if e.MediumReview != nil || e.Permission == "guarded" {
			confirm := !e.SkipMediumReview
			if e.MediumReview != nil {
				confirm = *e.MediumReview
			}
			label := tr(c, "记录时确认中风险文件操作")
			if e.ParentID != "" {
				label = tr(c, "申请时确认中风险文件操作")
			}
			ui.Checkbox(c, &confirm, label).Disabled(true)
		}
		if e.PreviousMediumReview != nil {
			previous := *e.PreviousMediumReview
			ui.Checkbox(c, &previous, tr(c, "变更前")).Disabled(true)
		}
		if grant, ok := a.historyLiveGuardedGrant(e); ok {
			a.grantMediumReviewSwitch(c, grant, tr(c, "当前授权 · 确认中风险文件操作"))
		}
		if e.Command != "" {
			ui.Text(c, tr(c, "命令")).FontSize(12).TextColor(c.Theme().TextMuted)
			ui.Scroll(c).FillWidth().MaxHeight(120).Children(func() {
				ui.Text(c, reviewText(e.Command)).FillWidth().Padding(10).Radius(6).
					Background(c.Theme().Background).Font("monospace").FontSize(12).Selectable()
			})
		}
		reviewRow(c, tr(c, "路径"), e.Path)
		reviewRow(c, tr(c, "目标路径"), e.Destination)
		reviewRow(c, tr(c, "容器 / 项目"), e.Object)
		reviewRow(c, tr(c, "操作发起进程"), e.ActorExecutable)
		if e.ActorPID > 0 {
			reviewRow(c, tr(c, "操作发起实例"), fmt.Sprintf("%d · %s", e.ActorPID, e.ActorInstance))
			reviewRow(c, tr(c, "操作发起映像"), e.ActorCodeID)
		}
		reviewRow(c, tr(c, "调用应用"), e.Executable)
		reviewRow(c, tr(c, "应用标识"), e.Bundle)
		reviewRow(c, tr(c, "映像标识"), e.ExecutableID)
		if e.PeerPID > 0 {
			reviewRow(c, tr(c, "进程"), fmt.Sprintf("%d · %s", e.PeerPID, e.ProcessInstance))
		}
		if e.Client != "" {
			reviewRow(c, tr(c, "自报名称"), e.Client)
		}
		reviewRow(c, tr(c, "连接地址"), e.Endpoint)
		reviewRow(c, tr(c, "服务器指纹"), e.HostFingerprint)
		reviewRow(c, tr(c, "密钥指纹"), e.KeyFingerprint)
		reviewRow(c, tr(c, "签名验证方式"), signingAuditAuthorization(e.Authorization))
		reviewRow(c, tr(c, "授权范围"), grantAuditPermission(e.Permission))
		if !e.Expires.IsZero() {
			reviewRow(c, tr(c, "到期时间"), e.Expires.Local().Format("2006-01-02 15:04:05"))
			reviewRow(c, tr(c, "闲置规则"), idlePolicyLabel(e.IdleSeconds))
		}
		if e.PreviousIdle != nil {
			reviewRow(c, tr(c, "原闲置规则"), idlePolicyLabel(*e.PreviousIdle))
		}
		if e.ExcerptTruncated {
			ui.Text(c, tr(c, "内容超过记录上限；操作摘要可用于核对原始请求")).FontSize(11).TextColor(c.Theme().TextMuted)
		}
		reviewRow(c, tr(c, "关联授权"), e.ParentID)
		if e.ParentID != "" {
			if components.Button(c, tr(c, "查看关联记录")).Clicked() {
				a.showRelatedAudit(e.ParentID, "")
			}
		}
		reviewRow(c, tr(c, "记录摘要"), e.RecordHash)
		reviewRow(c, tr(c, "授权世代"), e.Generation)
		reviewRow(c, tr(c, "授权来源"), historyAuthority(e))
		reviewRow(c, tr(c, "请求 ID"), e.RequestID)
		reviewRow(c, tr(c, "操作摘要"), e.OperationHash)
	})
}

func (a *app) historyLiveGuardedGrant(e auditlog.Event) (approval.Request, bool) {
	id, generation := e.ParentID, e.ParentGeneration
	if e.Kind == "grant" {
		id, generation = e.RequestID, e.Generation
	}
	if id == "" || generation == "" {
		return approval.Request{}, false
	}
	now := time.Now()
	for _, r := range a.requests {
		if r.ID == id && r.Operation.Kind == "grant" && r.Operation.Grant.Permission == "guarded" &&
			r.Status == "approved" && r.Operation.Host == e.Host && r.Generation() == generation && now.Before(r.Expires) {
			if activity := r.Activity; activity != nil && activity.IdleSeconds > 0 &&
				!now.Before(activity.LastUsed.Add(time.Duration(activity.IdleSeconds)*time.Second)) {
				continue
			}
			return r, true
		}
	}
	return approval.Request{}, false
}

func signingAuditAuthorization(value string) string {
	switch value {
	case "verified":
		return tr(nil, "本次系统验证")
	case "lease":
		return tr(nil, "限时授权")
	case "unprotected", "none":
		return tr(nil, "免验证")
	case "presence":
		return tr(nil, identitySystemAuthentication)
	case "biometry":
		return tr(nil, "仅当前指纹")
	}
	return value
}
func grantAuditPermission(value string) string {
	switch value {
	case "read":
		return tr(nil, "只读")
	case "full":
		return tr(nil, "完整权限")
	case "guarded":
		return tr(nil, "受控操作")
	case approval.MaintenancePermission:
		return tr(nil, "维护准备")
	}
	return value
}

func historyAuthority(e auditlog.Event) string {
	if e.Kind == "system-probe" {
		return tr(nil, "系统内置")
	}
	if auditlog.Category(e) == "ssh" {
		if e.Event == "ssh_authorized" || e.Event == "ssh_denied" {
			return tr(nil, "所有者决定")
		}
		if e.Authorization != "" {
			return signingAuditAuthorization(e.Authorization)
		}
		return tr(nil, "SSH 密钥代理")
	}
	if e.Event == "approved" || e.Event == "denied" && e.Reason == "" || e.Event == "grant_policy_changed" {
		return tr(nil, "所有者决定")
	}
	if e.OwnerIssued {
		return tr(nil, "本机所有者")
	}
	if e.Event == "delegated" || e.ParentID != "" {
		return tr(nil, "受托调用")
	}
	return ""
}
