package desktop

import (
	"fmt"
	"strconv"
	"strings"
	"time"
	"unicode"

	"sshclient/internal/approval"

	"github.com/egoist/mygo/ui"
)

// Untrusted server/caller metadata is displayed as text, with invisible control
// and directional characters made visible. This does not alter protocol values.
func reviewText(s string) string {
	var b strings.Builder
	for _, r := range s {
		if unicode.IsControl(r) || unicode.In(r, unicode.Cf) {
			q := strconv.QuoteRuneToASCII(r)
			b.WriteString(q[1 : len(q)-1])
		} else {
			b.WriteRune(r)
		}
	}
	return b.String()
}

func operationTitle(kind string) string {
	switch kind {
	case "docker-logs":
		return tr(nil, "读取容器日志")
	case "docker-terminal":
		return tr(nil, "打开容器终端")
	case "compose-plan":
		return tr(nil, "检查 Compose 更新")
	case "compose-update":
		return tr(nil, "更新 Compose 服务")
	case "grant":
		return tr(nil, "客户端会话授权")
	case "shell":
		return tr(nil, "打开终端")
	case "exec":
		return tr(nil, "执行命令")
	case "list":
		return tr(nil, "读取目录")
	case "get", "get-resume":
		return tr(nil, "下载文件")
	case "put", "put-resume":
		return tr(nil, "上传文件")
	case "replace":
		return tr(nil, "覆盖保存文件")
	case "mkdir":
		return tr(nil, "新建文件夹")
	case "rename":
		return tr(nil, "移动文件")
	case "remove":
		return tr(nil, "删除文件")
	case "chmod":
		return tr(nil, "修改权限")
	case "file-info":
		return tr(nil, "读取文件属性与账户名称")
	case "file-attributes":
		return tr(nil, "修改文件权限与所属用户 / 组")
	case "monitor":
		return tr(nil, "读取服务器状态")
	}
	return tr(nil, "未知操作")
}
func requestState(status string) string {
	switch status {
	case "pending":
		return tr(nil, "待批准")
	case "approved":
		return tr(nil, "已批准")
	case "running":
		return tr(nil, "运行中")
	case "finished":
		return tr(nil, "已结束")
	case "denied":
		return tr(nil, "已拒绝")
	case "revoked", "withdrawn":
		return tr(nil, "已撤销")
	}
	return tr(nil, "已到期")
}
func approvalActionLabel(r approval.Request) string {
	if r.Operation.Kind == "grant" {
		if r.Operation.Grant.ReadRoot != "" || r.Operation.Grant.WriteRoot != "" {
			if r.Renewal {
				return tr(nil, "续期限定目录访问")
			}
			return tr(nil, "允许限定目录访问")
		}
		if r.Renewal {
			if r.Operation.Grant.Permission == "full" {
				if r.Operation.PathProtection {
					return tr(nil, "续期读写")
				}
				return tr(nil, "续期完整控制")
			}
			return tr(nil, "续期只读访问")
		}
		if r.Operation.Grant.Permission == "full" {
			if r.Operation.PathProtection {
				return tr(nil, "允许读写")
			}
			return tr(nil, "允许完整控制")
		}
		return tr(nil, "允许只读访问")
	}
	return tr(nil, "允许本次")
}

func grantDuration(seconds int) string {
	if seconds%3600 == 0 {
		return fmt.Sprintf(tr(nil, "%d 小时"), seconds/3600)
	}
	if seconds%60 == 0 {
		return fmt.Sprintf(tr(nil, "%d 分钟"), seconds/60)
	}
	return (time.Duration(seconds) * time.Second).String()
}

func transferState(state string) string {
	switch state {
	case "queued":
		return tr(nil, "排队中")
	case "waiting":
		return tr(nil, "准备中")
	case "running":
		return tr(nil, "传输中")
	case "paused":
		return tr(nil, "已暂停")
	case "error":
		return tr(nil, "失败")
	case "done":
		return tr(nil, "已完成")
	}
	return tr(nil, "未知状态")
}

func (a *app) endpointLabel(endpoint string) string {
	if endpoint == localEndpoint {
		return tr(nil, "本机")
	}
	for _, connection := range a.connections {
		if connection.ID == endpoint {
			return reviewText(connection.Name)
		}
	}
	return tr(nil, "选择位置")
}

// sourceSelect displays fixed UI choices in the active language while retaining
// their source values for selection state and the logic consuming that state.
func sourceSelect(c *ui.Context, selected *string, sources []string) ui.Element {
	labels := make([]string, len(sources))
	display := *selected
	for i, source := range sources {
		labels[i] = tr(c, source)
		if source == *selected {
			display = labels[i]
		}
	}
	control := ui.Select(c, &display, labels)
	if control.Changed() {
		for i, label := range labels {
			if label == display {
				*selected = sources[i]
				break
			}
		}
	}
	return control
}

func sessionDisplayName(c *ui.Context, s *session) string {
	if s.target.Kind == "docker-terminal" && strings.HasPrefix(s.name, "容器 · ") {
		return tr(c, "容器 · ") + strings.TrimPrefix(s.name, "容器 · ")
	}
	return s.name
}
