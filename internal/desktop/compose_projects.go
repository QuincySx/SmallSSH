package desktop

import (
	"fmt"
	"slices"
	"strings"

	"github.com/egoist/mygo/ui"
	"sshclient/internal/desktop/components"
	"sshclient/internal/monitor"
	"sshclient/internal/serverops"
)

func projectStatus(p monitor.ComposeProject) string {
	return map[string]string{"running": tr(nil, "运行中"), "partial": tr(nil, "部分运行"), "attention": tr(nil, "需关注"), "starting": tr(nil, "启动中"), "paused": tr(nil, "已暂停"), "stopped": tr(nil, "已停止"), "unknown": tr(nil, "状态不完整")}[p.State()]
}

func projectColor(c *ui.Context, p monitor.ComposeProject) ui.Color {
	switch p.State() {
	case "running":
		return c.Theme().Success
	case "attention":
		return c.Theme().Danger
	case "partial", "starting", "unknown":
		return c.Theme().Warning
	}
	return c.Theme().TextMuted
}

func conditionText(row monitor.Container) string {
	return map[string]string{"running": tr(nil, "运行中"), "healthy": tr(nil, "健康"), "unhealthy": tr(nil, "不健康"), "starting": tr(nil, "健康检查中"), "failed": tr(nil, "退出异常"), "restarting": tr(nil, "重启中"), "paused": tr(nil, "已暂停"), "stopped": tr(nil, "已停止"), "created": tr(nil, "未启动"), "dead": tr(nil, "异常"), "removing": tr(nil, "删除中"), "unknown": tr(nil, "未知")}[monitor.ContainerCondition(row)]
}

func conditionColor(c *ui.Context, row monitor.Container) ui.Color {
	switch monitor.ContainerCondition(row) {
	case "healthy", "running":
		return c.Theme().Success
	case "unhealthy", "failed", "dead", "restarting":
		return c.Theme().Danger
	case "starting", "unknown", "removing":
		return c.Theme().Warning
	}
	return c.Theme().TextMuted
}

func projectCounts(p monitor.ComposeProject) string {
	text := fmt.Sprintf(tr(nil, "%d / %d 容器运行"), p.Running, len(p.Containers))
	if p.Faults > 0 {
		text += fmt.Sprintf(tr(nil, " · %d 异常"), p.Faults)
	}
	return text
}

func projectMetric(value string, samples, wanted int, unknown bool) string {
	if wanted == 0 && !unknown {
		return value
	}
	if samples == 0 {
		return tr(nil, "未采集")
	}
	if samples < wanted || unknown {
		return value + tr(nil, " · 部分")
	}
	return value
}

func projectResourceValues(p monitor.ComposeProject) (string, string) {
	return projectMetric(percentText(p.CPU), p.CPUSamples, p.ResourceContainers, p.Unknown > 0),
		projectMetric(size(p.Memory), p.MemorySamples, p.ResourceContainers, p.Unknown > 0)
}

func composeUpdateIssue(rows []monitor.Container, d *monitor.DockerInfo) string {
	if len(rows) == 0 || d == nil {
		return tr(nil, "未发现项目容器")
	}
	if strings.Contains(d.Note, "容器清单读取不完整") || strings.Contains(d.Note, "清单已截断") {
		return tr(nil, "容器清单不完整，无法确认项目范围，请刷新后重试")
	}
	representative := rows[0]
	services := []string{}
	for _, row := range rows {
		if composeProject(row) != composeProject(representative) || row.ComposeDir != representative.ComposeDir || row.ComposeFiles != representative.ComposeFiles {
			return tr(nil, "同项目容器的配置来源不一致，请先在服务器核对")
		}
		if row.Service == "" {
			return tr(nil, "项目来源标签不完整，请刷新 Docker 清单后重试")
		}
		services = append(services, row.Service)
	}
	slices.Sort(services)
	r := serverops.ComposeRequest{Target: serverops.DockerTarget{Socket: d.Socket, Engine: d.EngineID, ID: representative.ID}, Project: composeProject(representative), Directory: representative.ComposeDir, Files: representative.ComposeFiles, Services: strings.Join(slices.Compact(services), ",")}
	if err := r.Validate(false); err != nil {
		return err.Error()
	}
	return ""
}

func (a *app) composeView(c *ui.Context, d *monitor.DockerInfo, state *serverManagementUI) {
	projects := monitor.ComposeProjects(d.Containers, a.containerMetrics(d))
	if state.composeProject != "" {
		a.composeProjectDetails(c, d, state, projects)
		return
	}
	query := strings.ToLower(state.dockerSearch)
	rows := make([]monitor.ComposeProject, 0, len(projects))
	for _, p := range projects {
		matches := strings.Contains(strings.ToLower(p.Name+" "+projectStatus(p)), query)
		for _, row := range p.Containers {
			matches = matches || composeContainerMatches(row, query)
		}
		if matches {
			rows = append(rows, p)
		}
	}
	if len(rows) == 0 {
		text := tr(c, "未发现 Compose 项目")
		if len(projects) > 0 {
			text = tr(c, "没有匹配的项目")
		}
		empty := ui.Text(c, text).FontSize(12).TextColor(c.Theme().TextMuted)
		if len(projects) == 0 {
			empty.Tooltip(tr(c, "按 Docker 的 Compose 标签识别；普通容器在“容器”页查看。"))
		}
	}
	state.composeGrid.Key = func(i int) any { return d.EngineID + ":" + d.Socket + ":" + rows[i].Name }
	state.composeGrid.Label = func(i int) string { return rows[i].Name }
	containerCardLayout.CardGrid(c, &state.composeGrid, max(0, a.statusPaneWidth(c)-statusScrollGutter), len(rows), 164, func(i int) {
		a.composeProjectCard(c, rows[i], d, state)
	}).Padding(0, statusScrollGutter, 0, 0).FillWidth().Grow(1).Label(tr(c, "Compose 项目卡片"))
}

func (a *app) composeProjectCard(c *ui.Context, p monitor.ComposeProject, d *monitor.DockerInfo, state *serverManagementUI) {
	card := components.Button(c, "").Fill().Padding(9).Label(tr(c, "Compose 项目 ") + p.Name).Tooltip(tr(c, "查看项目中的容器"))
	card.Children(func() {
		ui.Column(c).Fill().Gap(6).Children(func() {
			ui.Row(c).FillWidth().Gap(8).AlignItems(ui.Center).Children(func() {
				ui.Text(c, reviewText(p.Name)).FontSize(13).Bold().Grow(1).MinWidth(0).SingleLine().Tooltip(p.Name)
				ui.Text(c, "● "+projectStatus(p)).FontSize(10).TextColor(projectColor(c, p))
				ui.Text(c, "›").TextColor(c.Theme().TextMuted)
			})
			ui.Text(c, projectCounts(p)).FontSize(10).SingleLine().TextColor(c.Theme().TextMuted).
				Tooltip(fmt.Sprintf(tr(c, "%d 个服务；按 Docker 当前保留的容器统计，未创建或已删除的服务不在清单中。运行中不等于通过健康检查。"), p.Services))
			cpu, memory := projectResourceValues(p)
			ui.Grid(c).FillWidth().Columns(2).Gap(8).Children(func() {
				components.Metric(c, "CPU", cpu).Label(tr(c, "项目 CPU ") + p.Name).Tooltip(fmt.Sprintf(tr(c, "单核口径合计，可超过 100%%；已采集 %d / %d 个活动容器"), p.CPUSamples, p.ResourceContainers))
				components.Metric(c, tr(c, "内存"), memory).Label(tr(c, "项目内存 ") + p.Name).Tooltip(fmt.Sprintf(tr(c, "Docker 内存口径合计；已采集 %d / %d 个活动容器"), p.MemorySamples, p.ResourceContainers))
			})
			ui.Column(c).FillWidth().Gap(3).Children(func() {
				for _, row := range p.Containers[:min(3, len(p.Containers))] {
					a.composeContainerStatus(c, row)
				}
				if len(p.Containers) > 3 {
					ui.Text(c, fmt.Sprintf(tr(c, "另有 %d 个容器 ›"), len(p.Containers)-3)).FontSize(10).TextColor(c.Theme().TextMuted)
				}
			})
		})
	})
	card.ContextMenu(func(m *ui.Menu) {
		a.composeProjectMenu(c, p, d, state, m)
	})
	if card.Clicked() {
		state.composeProject, state.composeEngine, state.composeSocket = p.Name, d.EngineID, d.Socket
		state.composeContainers = ui.GridState{}
	}
}

func (a *app) composeContainerStatus(c *ui.Context, row monitor.Container) {
	name := row.Names
	if name == "" {
		name = row.Service
	}
	ui.Row(c).FillWidth().MinWidth(0).Gap(6).AlignItems(ui.Center).Label(tr(c, "项目子状态 ") + row.ID).Children(func() {
		ui.Text(c, "●").FontSize(9).TextColor(conditionColor(c, row))
		ui.Text(c, reviewText(name)).FontSize(11).Grow(1).MinWidth(0).SingleLine().Tooltip(row.Service + " · " + row.Image)
		ui.Text(c, conditionText(row)).FontSize(10).TextColor(conditionColor(c, row)).SingleLine().Tooltip(row.Status)
	})
}

func (a *app) composeProjectDetails(c *ui.Context, d *monitor.DockerInfo, state *serverManagementUI, projects []monitor.ComposeProject) {
	var project *monitor.ComposeProject
	if state.composeEngine == d.EngineID && state.composeSocket == d.Socket {
		for i := range projects {
			if projects[i].Name == state.composeProject {
				project = &projects[i]
				break
			}
		}
	}
	ui.Row(c).FillWidth().Shrink(0).Height(30).Gap(6).AlignItems(ui.Center).Children(func() {
		if components.Button(c, "‹", components.ButtonOptions{Variant: components.Quiet}).Size(26, 26).Label(tr(c, "返回 Compose 项目列表")).Tooltip(tr(c, "返回项目列表")).Clicked() {
			state.composeProject = ""
		}
		ui.Text(c, reviewText(state.composeProject)).FontSize(13).Bold().Grow(1).MinWidth(0).SingleLine().Tooltip(state.composeProject)
		if project != nil {
			a.composeProjectMenuButton(c, *project, d, state)
		}
	})
	if project == nil {
		ui.Text(c, tr(c, "项目已不在当前 Docker 清单中，请返回项目列表。")).FontSize(12).TextColor(c.Theme().TextMuted)
		return
	}
	p := *project
	cpu, memory := projectResourceValues(p)
	ui.Row(c).FillWidth().Shrink(0).Wrap().Gap(10).Children(func() {
		ui.Text(c, "● "+projectStatus(p)).FontSize(11).TextColor(projectColor(c, p))
		ui.Text(c, projectCounts(p)).FontSize(11).TextColor(c.Theme().TextMuted)
		ui.Text(c, "CPU "+cpu+tr(c, " · 内存 ")+memory).FontSize(11)
	})
	if issue := composeUpdateIssue(p.Containers, d); issue != "" {
		ui.Text(c, issue).FillWidth().MinWidth(0).FontSize(10).TextColor(c.Theme().Warning)
	} else {
		ui.Text(c, reviewText(p.Containers[0].ComposeDir)).FillWidth().MinWidth(0).SingleLine().FontSize(10).TextColor(c.Theme().TextMuted).Tooltip(p.Containers[0].ComposeFiles)
	}
	metrics := a.containerMetrics(d)
	query := strings.ToLower(state.dockerSearch)
	rows := make([]monitor.Container, 0, len(p.Containers))
	for _, row := range p.Containers {
		if strings.Contains(strings.ToLower(p.Name+" "+projectStatus(p)), query) || composeContainerMatches(row, query) {
			rows = append(rows, row)
		}
	}
	if len(rows) == 0 {
		ui.Text(c, tr(c, "没有匹配的容器")).FontSize(11).TextColor(c.Theme().TextMuted)
	}
	state.composeContainers.Key = func(i int) any { return containerIdentity{d.EngineID, rows[i].ID} }
	containerCardLayout.CardGrid(c, &state.composeContainers, max(0, a.statusPaneWidth(c)-statusScrollGutter), len(rows), 148, func(i int) {
		row := rows[i]
		a.containerCard(c, row, d, metrics[row.ID])
	}).Padding(0, statusScrollGutter, 0, 0).FillWidth().Grow(1).Label(tr(c, "Compose 项目容器"))
}

func composeContainerMatches(row monitor.Container, query string) bool {
	return strings.Contains(strings.ToLower(row.Service+" "+row.Names+" "+row.Image+" "+row.State+" "+row.Status+" "+conditionText(row)), query)
}
