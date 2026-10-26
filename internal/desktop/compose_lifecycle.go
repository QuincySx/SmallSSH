package desktop

import (
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/egoist/mygo/ui"
	"sshclient/internal/desktop/components"
	"sshclient/internal/monitor"
	"sshclient/internal/serverops"
)

func composeLifecycleTarget(p monitor.ComposeProject, d *monitor.DockerInfo) (serverops.ComposeRequest, []serverops.ComposeMember) {
	row := p.Containers[0]
	services := []string{}
	members := []serverops.ComposeMember{}
	for _, row := range p.Containers {
		services = append(services, row.Service)
		members = append(members, serverops.ComposeMember{ID: row.ID, Service: row.Service})
	}
	slices.Sort(services)
	return serverops.ComposeRequest{Target: serverops.DockerTarget{Socket: d.Socket, Engine: d.EngineID, ID: row.ID}, Project: p.Name, Directory: row.ComposeDir, Files: row.ComposeFiles, Services: strings.Join(slices.Compact(services), ",")}, members
}

func composeLifecycleAvailable(p monitor.ComposeProject, action string) bool {
	for _, row := range p.Containers {
		switch action {
		case "start":
			if row.State == "exited" || row.State == "created" {
				return true
			}
		case "stop":
			if row.State == "running" || row.State == "paused" || row.State == "restarting" {
				return true
			}
		case "restart":
			if row.State != "dead" && row.State != "removing" && row.State != "" {
				return true
			}
		}
	}
	return false
}

func (a *app) composeProjectMenu(c *ui.Context, p monitor.ComposeProject, d *monitor.DockerInfo, state *serverManagementUI, m *ui.Menu) {
	source := a.serverStateSession()
	disabled := composeUpdateIssue(p.Containers, d) != "" || !a.statusActionsAvailable() || state.busy
	if m.Item(tr(c, "更新项目…")).Disabled(disabled).Chosen() {
		if a.serverStateSession() == source {
			a.openComposeUpdate(p.Containers, d)
		}
	}
	m.Separator()
	for _, action := range []struct{ label, verb string }{{tr(c, "启动项目…"), "start"}, {tr(c, "停止项目…"), "stop"}, {tr(c, "重启项目…"), "restart"}} {
		if m.Item(action.label).Disabled(disabled || !composeLifecycleAvailable(p, action.verb)).Chosen() {
			if a.serverStateSession() == source {
				a.confirmComposeLifecycle(p, d, action.verb)
			}
		}
	}
}

func (a *app) composeProjectMenuButton(c *ui.Context, p monitor.ComposeProject, d *monitor.DockerInfo, state *serverManagementUI) {
	components.IconButton(c, "⋯", tr(c, "项目菜单 ")+p.Name).Menu(func(m *ui.Menu) {
		a.composeProjectMenu(c, p, d, state, m)
	})
}

func (a *app) confirmComposeLifecycle(p monitor.ComposeProject, d *monitor.DockerInfo, action string) {
	if issue := composeUpdateIssue(p.Containers, d); issue != "" {
		a.managementState().message = issue
		return
	}
	r, members := composeLifecycleTarget(p, d)
	command, err := serverops.ComposeLifecycle(r, action, members)
	if err != nil {
		a.managementState().message = err.Error()
		return
	}
	if len(command)+len(monitor.InventoryCommand)+512 > 32768 {
		a.managementState().message = tr(nil, "项目操作范围过大，无法生成完整确认请求")
		return
	}
	title := map[string]string{"start": tr(nil, "启动项目"), "stop": tr(nil, "停止项目"), "restart": tr(nil, "重启项目")}[action]
	description := map[string]string{
		"start":   tr(nil, "启动此项目已有的服务容器，不创建容器或升级镜像。已暂停的容器不会自动解除暂停。"),
		"stop":    tr(nil, "停止此项目已有的服务容器，相关服务将不可用。容器及数据卷保留，每个容器最多等待 10 秒正常退出。"),
		"restart": tr(nil, "重启此项目全部已有服务，包括当前已停止的服务；会短暂中断，不升级镜像或应用配置变更。每个容器最多等待 10 秒正常退出。"),
	}[action]
	message := fmt.Sprintf(tr(nil, "服务：%s\n配置：%s\n\n%s"), r.Services, r.Files, description)
	target := fmt.Sprintf(tr(nil, "%s · %d 服务 · %d 容器"), p.Name, p.Services, len(p.Containers))
	a.confirmServerActionWithTimeout(title, target, message, command, action != "start", 6*time.Minute)
}
