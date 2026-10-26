package desktop

import (
	"fmt"
	"time"

	"github.com/egoist/mygo/ui"
	"sshclient/internal/approval"
)

// Keep the decision separate from protocol diagnostics. Caller labels remain
// visibly self-reported; only the pairing fingerprint identifies a grant key.
func reviewRow(c *ui.Context, label, value string) {
	if value == "" {
		return
	}
	ui.Row(c).FillWidth().MinWidth(0).Gap(12).AlignItems(ui.Start).Children(func() {
		ui.Text(c, label).Width(76).Shrink(0).FontSize(12).TextColor(c.Theme().TextMuted)
		ui.Text(c, reviewText(value)).Grow(1).MinWidth(0).Selectable().FontSize(13)
	})
}

func approvalHeading(r approval.Request) string {
	if r.Operation.Kind == "grant" {
		if r.Status != "pending" {
			return tr(nil, "客户端会话授权")
		}
		if r.Renewal {
			return tr(nil, "会话已过期，允许续期？")
		}
		return tr(nil, "允许持续访问服务器？")
	}
	return operationTitle(r.Operation.Kind)
}

func approvalAuthReason(r approval.Request) string {
	title := operationTitle(r.Operation.Kind)
	if r.Operation.Kind == "grant" {
		title = approvalActionLabel(r)
	}
	// The broker supplies this endpoint from the stored connection. Do not put
	// self-reported caller names or opaque request IDs into the OS prompt.
	target := []rune(reviewText(r.Operation.Endpoint))
	if len(target) > 96 {
		target = append(target[:95], '…')
	}
	if len(target) == 0 {
		return title
	}
	return title + " · " + string(target)
}

func approvalPermission(r approval.Request) (label, actions, caution string) {
	switch r.Operation.Kind {
	case "grant":
		if r.Operation.Grant.Permission == "full" {
			if r.Operation.PathProtection {
				return tr(nil, "读写（受服务器策略限制）"), "", tr(nil, "保护开启时，AI 不开放任意命令和终端")
			}
			return tr(nil, "完整控制"), "", tr(nil, "可使用此 SSH 账户的全部权限，包括任意命令和文件修改")
		}
		return tr(nil, "只读访问"), tr(nil, "浏览、下载文件，读取状态和容器日志"), tr(nil, "可读取此账户有权访问的所有路径")
	case "exec":
		return tr(nil, "执行命令"), "", tr(nil, "使用此 SSH 账户的权限执行")
	case "shell":
		return tr(nil, "交互终端"), "", tr(nil, "可执行任意命令和修改文件")
	case "docker-terminal":
		return tr(nil, "容器终端"), "", tr(nil, "可在容器内执行任意命令")
	case "compose-update":
		return tr(nil, "更新项目"), "", tr(nil, "下载镜像并重建所选服务，可能短暂中断")
	case "remove":
		return tr(nil, "删除文件"), "", tr(nil, "删除后无法从此工具恢复")
	case "put", "put-resume", "replace", "mkdir", "rename", "chmod", "file-attributes":
		return tr(nil, "修改文件"), "", ""
	case "compose-plan", "list", "file-info", "get", "get-resume", "monitor", "docker-logs":
		return tr(nil, "只读操作"), "", ""
	default:
		return tr(nil, "未知操作"), "", tr(nil, "请先核对请求详情")
	}
}

func requestReview(c *ui.Context, r approval.Request, serverNames ...string) {
	op := r.Operation
	panel := ui.Column(c).Key(r.ID).FillWidth().MinWidth(0).Gap(16)
	details := ui.Local(panel, "details", func() bool { return false })
	panel.Children(func() {
		ui.Text(c, approvalHeading(r)).FontSize(22).Bold().FillWidth()
		ui.Column(c).FillWidth().MinWidth(0).Gap(14).Children(func() {
			caller := op.Client
			if r.OwnerIssued {
				caller = tr(c, "本机所有者")
			}
			ui.Row(c).FillWidth().MinWidth(0).Gap(12).AlignItems(ui.Start).Children(func() {
				ui.Text(c, tr(c, "申请方")).Width(76).Shrink(0).FontSize(12).TextColor(c.Theme().TextMuted)
				ui.Column(c).Grow(1).MinWidth(0).Gap(3).Children(func() {
					ui.Text(c, reviewText(caller)).FillWidth().Selectable().FontSize(15).Bold()
					if !r.OwnerIssued {
						note := tr(c, "自报名称 · 未验证软件身份")
						if op.Kind == "grant" {
							note = tr(c, "自报名称 · 用配对码核对申请端")
						}
						ui.Text(c, note).FillWidth().FontSize(11).TextColor(c.Theme().TextMuted)
					}
				})
			})
			target := op.Endpoint
			if target == "" {
				target = op.Host
			}
			ui.Row(c).FillWidth().MinWidth(0).Gap(12).AlignItems(ui.Start).Children(func() {
				ui.Text(c, tr(c, "服务器")).Width(76).Shrink(0).FontSize(12).TextColor(c.Theme().TextMuted)
				ui.Column(c).Grow(1).MinWidth(0).Gap(3).Children(func() {
					if len(serverNames) > 0 && serverNames[0] != "" && serverNames[0] != target {
						ui.Text(c, reviewText(serverNames[0])).FillWidth().FontSize(14).Bold()
					}
					ui.Text(c, reviewText(target)).FillWidth().Selectable().FontSize(13)
				})
			})
			permission, actions, caution := approvalPermission(r)
			ui.Row(c).FillWidth().MinWidth(0).Gap(12).AlignItems(ui.Start).Children(func() {
				ui.Text(c, tr(c, "授权范围")).Width(76).Shrink(0).FontSize(12).TextColor(c.Theme().TextMuted)
				ui.Column(c).Grow(1).MinWidth(0).Gap(4).Children(func() {
					color := c.Theme().Accent
					if caution != "" && !(op.Kind == "grant" && op.Grant.Permission == "read") {
						color = c.Theme().Warning
					}
					if permission != approvalHeading(r) {
						ui.Text(c, permission).FontSize(14).Bold().TextColor(color)
					}
					if actions != "" {
						ui.Text(c, actions).FillWidth().FontSize(12)
					}
					if caution != "" {
						ui.Text(c, caution).FillWidth().FontSize(12).TextColor(color)
					}
				})
			})
			if op.Kind == "grant" {
				term := fmt.Sprintf(tr(c, "最长 %s · 闲置 %s 后失效"), grantDuration(op.Grant.LifetimeSeconds), grantDuration(op.Grant.IdleSeconds))
				if r.Status == "approved" {
					term = r.Expires.Local().Format(tr(c, "01-02 15:04 到期")) + tr(c, " · 闲置 ") + grantDuration(op.Grant.IdleSeconds) + tr(c, " 后失效")
				}
				reviewRow(c, tr(c, "授权期限"), term)
				ui.Column(c).FillWidth().Padding(12, 14).Radius(8).Gap(5).Background(c.Theme().Accent.Alpha(.06)).Border(1, c.Theme().Accent.Alpha(.16)).Children(func() {
					ui.Row(c).FillWidth().Gap(8).AlignItems(ui.Center).Children(func() {
						ui.Text(c, tr(c, "配对码")).FontSize(12).TextColor(c.Theme().TextMuted)
						ui.Text(c, tr(c, "与申请端一致再允许")).Grow(1).FontSize(11).TextAlign(ui.End).TextColor(c.Theme().TextMuted)
					})
					ui.Text(c, op.Grant.PairingCode()).FillWidth().Selectable().FontSize(18).Bold()
				})
				ui.Text(c, tr(c, "锁定、退出或撤销后失效")).FillWidth().FontSize(11).TextColor(c.Theme().TextMuted)
			} else {
				lifetime := tr(c, "本次操作 · 有效至 ") + r.Expires.Local().Format("01-02 15:04:05")
				if r.OwnerSession {
					lifetime = tr(c, "关闭终端、锁定或退出时结束")
				}
				reviewRow(c, tr(c, "有效期"), lifetime)
			}
			requestOperationReview(c, r)
		})
		ui.Collapsible(c, tr(c, "技术详情"), details, func() {
			requestTechnicalReview(c, r)
		})
	})
}

func (a *app) approvalServerName(r approval.Request) string {
	for _, connection := range a.connections {
		if connection.ID == r.Operation.Host {
			return connection.Name
		}
	}
	return ""
}

func (a *app) reviewedApproval(id string) (approval.Request, bool) {
	for _, r := range a.requests {
		if r.ID == id {
			return r, true
		}
	}
	return approval.Request{}, false
}

// Security-relevant operation data is shared by compact prompts and the review center.
func requestOperationReview(c *ui.Context, r approval.Request) {
	op := r.Operation
	if op.Command != "" {
		ui.Column(c).FillWidth().MinWidth(0).Gap(6).Children(func() {
			ui.Text(c, tr(c, "完整命令")).FontSize(12).TextColor(c.Theme().TextMuted)
			ui.Scroll(c).FillWidth().MaxHeight(144).Children(func() {
				ui.Text(c, reviewText(op.Command)).FillWidth().Padding(10).Radius(6).Background(c.Theme().Surface).Font("monospace").FontSize(12).Selectable()
			})
		})
	}
	reviewRow(c, tr(c, "路径"), op.Path)
	reviewRow(c, tr(c, "目标路径"), op.Destination)
	if op.Kind == "docker-logs" || op.Kind == "docker-terminal" {
		reviewRow(c, tr(c, "容器 ID"), op.Docker.Target.ID)
		if op.Kind == "docker-terminal" {
			reviewRow(c, tr(c, "容器 Shell"), op.Docker.Shell)
		} else {
			reviewRow(c, tr(c, "日志行数"), fmt.Sprint(op.Docker.Tail))
		}
	}
	if op.Kind == "compose-plan" || op.Kind == "compose-update" {
		reviewRow(c, tr(c, "项目"), op.Compose.Project)
		reviewRow(c, tr(c, "服务"), op.Compose.Services)
	}
	if op.Kind == "chmod" {
		reviewRow(c, tr(c, "权限"), fmt.Sprintf("%03o", op.Mode))
	}
	if op.Kind == "file-attributes" {
		if op.Attributes.SetMode {
			reviewRow(c, tr(c, "权限"), fmt.Sprintf("%03o → %03o", op.Attributes.Expected.Mode&0777, op.Attributes.Mode))
		}
		if op.Attributes.SetUID {
			reviewRow(c, tr(c, "用户 UID"), fmt.Sprintf("%d → %d", op.Attributes.Expected.UID, op.Attributes.UID))
		}
		if op.Attributes.SetGID {
			reviewRow(c, tr(c, "组 GID"), fmt.Sprintf("%d → %d", op.Attributes.Expected.GID, op.Attributes.GID))
		}
	}
	if op.ServerStatus {
		ui.Text(c, tr(c, "含自动读取 CPU、内存、进程、服务和 Docker 状态")).FillWidth().FontSize(11).TextColor(c.Theme().TextMuted)
	}
}

func requestTechnicalReview(c *ui.Context, r approval.Request) {
	op := r.Operation
	reviewRow(c, tr(c, "主机指纹"), op.HostFingerprint)
	reviewRow(c, tr(c, "内容摘要"), op.ContentHash)
	reviewRow(c, tr(c, "原内容摘要"), op.OriginalHash)
	if op.Docker.Target.Engine != "" {
		reviewRow(c, tr(c, "Docker 实例"), op.Docker.Target.Engine)
	}
	if op.Compose.Project != "" {
		reviewRow(c, tr(c, "工作目录"), op.Compose.Directory)
		reviewRow(c, tr(c, "配置文件"), op.Compose.Files)
		reviewRow(c, tr(c, "计划摘要"), op.Compose.ConfigHash)
	}
	if op.TransferID != "" {
		reviewRow(c, tr(c, "续传"), fmt.Sprintf(tr(c, "%s · %d / %d 字节"), op.TransferID, op.Offset, op.Size))
	}
	reviewRow(c, tr(c, "请求到期"), r.Expires.Local().Format(time.DateTime))
	reviewRow(c, tr(c, "请求 ID"), r.ID)
	reviewRow(c, tr(c, "父授权 ID"), r.ParentID)
}
