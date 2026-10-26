package desktop

import (
	"cmp"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/egoist/mygo/ui"
	"sshclient/internal/desktop/components"
	"sshclient/internal/monitor"
)

func (a *app) dockerView(c *ui.Context, info *monitor.Inventory) {
	if s := a.managementState().containerDetails; s != nil {
		if info != nil && info.Docker != nil && info.Docker.EngineID == s.target.Engine {
			for _, row := range info.Docker.Containers {
				if row.ID == s.target.ID {
					s.row = row
					break
				}
			}
		}
		a.containerDetailsView(c, s)
		return
	}
	if info == nil || info.Docker == nil {
		ui.Text(c, tr(c, "正在读取 Docker 信息…")).FontSize(12)
		return
	}
	d := info.Docker
	if d.Status != "ready" {
		text := tr(c, "Docker 信息读取中…")
		switch d.Status {
		case "missing":
			text = tr(c, "未安装 Docker")
		case "unavailable":
			text = tr(c, "Docker 已安装，当前账户无法连接本机 Docker。请检查服务是否运行，以及账户权限。")
		}
		ui.Text(c, text).FontSize(12).FillWidth()
		if d.ClientVersion != "" {
			ui.Text(c, reviewText(d.ClientVersion)).FontSize(11).TextColor(c.Theme().TextMuted)
		}
		if d.Note != "" {
			ui.Text(c, reviewText(d.Note)).FontSize(11).FillWidth().TextColor(c.Theme().TextMuted)
		}
		return
	}
	state := a.managementState()
	narrow := a.statusPaneWidth(c) < 560
	ui.Row(c).FillWidth().Shrink(0).AlignItems(ui.Center).Gap(8).Children(func() {
		tabs := ui.Tabs(c, &state.dockerTab, tr(c, "容器"), tr(c, "镜像"), "Compose")
		if narrow {
			tabs.Grow(1)
		} else {
			ui.TextInput(c, &state.dockerSearch).Grow(1).MinWidth(0).Placeholder(tr(c, "搜索名称、镜像或状态")).Label(tr(c, "搜索 Docker"))
		}
		if state.dockerTab == 0 {
			components.IconButton(c, "☷", tr(c, "容器显示设置")).Menu(func(m *ui.Menu) {
				if m.Item(tr(c, "卡片视图")).Checked(!state.containerList).Chosen() {
					state.containerList = false
				}
				if m.Item(tr(c, "列表视图")).Checked(state.containerList).Chosen() {
					state.containerList = true
				}
				m.Separator()
				for _, v := range []struct{ label, column string }{{tr(c, "按名称排序"), "name"}, {tr(c, "按 CPU 排序"), "cpu"}, {tr(c, "按内存排序"), "memory"}} {
					if m.Item(v.label).Checked(state.containerSort.Column == v.column).Chosen() {
						state.containerSort = ui.SortOrder{Column: v.column, Descending: v.column != "name"}
					}
				}
			})
		}
	})
	if narrow {
		ui.TextInput(c, &state.dockerSearch).FillWidth().Shrink(0).Placeholder(tr(c, "搜索名称、镜像或状态")).Label(tr(c, "搜索 Docker"))
	}
	if state.dockerTab == 1 {
		a.dockerImages(c, d, state)
	} else if state.dockerTab == 2 {
		a.composeView(c, d, state)
	} else {
		a.dockerContainers(c, d, state)
	}
	note := fmt.Sprintf(tr(c, "%d 个容器 · %d 个镜像"), len(d.Containers), len(d.Images))
	if state.dockerTab == 2 {
		projects := map[string]bool{}
		containers := 0
		for _, row := range d.Containers {
			if name := composeProject(row); name != "" {
				projects[name] = true
				containers++
			}
		}
		note = fmt.Sprintf(tr(c, "%d 个项目 · %d 个容器"), len(projects), containers)
	}
	tip := tr(c, "容器资源最短每 10 秒、清单最短每分钟更新；读取失败时会退避重试。")
	if d.Version != "" {
		tip += "\nDocker " + d.Version
	}
	if d.ClientVersion != "" {
		tip += "\n" + d.ClientVersion
	}
	warnings := []string{}
	if d.Note != "" {
		warnings = append(warnings, d.Note)
	}
	if s := a.serverStateSession(); s != nil && s.stats != nil && s.stats.DockerStats != nil {
		metrics := s.stats.DockerStats
		if !metrics.Time.IsZero() {
			tip += tr(c, "\n最近资源采样 ") + metrics.Time.Local().Format("15:04:05")
		}
		if metrics.Note != "" && metrics.Note != d.Note {
			warnings = append(warnings, metrics.Note)
		}
	}
	ui.Text(c, note).FontSize(10).SingleLine().TextColor(c.Theme().TextMuted).Tooltip(reviewText(tip)).Label(tr(c, "Docker 清单摘要"))
	for _, warning := range warnings {
		ui.Text(c, reviewText(warning)).FillWidth().FontSize(11).TextColor(c.Theme().Warning)
	}
}

func (a *app) dockerContainers(c *ui.Context, d *monitor.DockerInfo, state *serverManagementUI) {
	rows := make([]monitor.Container, 0, len(d.Containers))
	q := strings.ToLower(state.dockerSearch)
	for _, row := range d.Containers {
		if strings.Contains(strings.ToLower(row.Names+" "+row.Image+" "+row.State+" "+row.Status), q) {
			rows = append(rows, row)
		}
	}
	metrics := a.containerMetrics(d)
	order := state.containerSort
	slices.SortStableFunc(rows, func(a, b monitor.Container) int {
		left, right := a.Names, b.Names
		switch order.Column {
		case "image":
			left, right = a.Image, b.Image
		case "state":
			left, right = a.State, b.State
		case "ports":
			left, right = a.Ports, b.Ports
		}
		v := compareText(left, right)
		switch order.Column {
		case "cpu":
			v = cmp.Compare(metrics[a.ID].CPU, metrics[b.ID].CPU)
		case "memory":
			v = cmp.Compare(metrics[a.ID].Memory, metrics[b.ID].Memory)
		case "network":
			v = cmp.Compare(float64(metrics[a.ID].RX)+float64(metrics[a.ID].TX), float64(metrics[b.ID].RX)+float64(metrics[b.ID].TX))
		case "block":
			v = cmp.Compare(float64(metrics[a.ID].Read)+float64(metrics[a.ID].Write), float64(metrics[b.ID].Read)+float64(metrics[b.ID].Write))
		}
		if order.Descending {
			v = -v
		}
		if v == 0 {
			return cmp.Compare(a.ID, b.ID)
		}
		return v
	})
	var chosen *monitor.Container
	if state.containerChoice.Len() == 1 {
		for n := range rows {
			if state.containerChoice.Has(containerIdentity{d.EngineID, rows[n].ID}) {
				chosen = &rows[n]
				break
			}
		}
	}
	if state.containerList {
		ui.Row(c).FillWidth().Shrink(0).Children(func() {
			ui.Text(c, fmt.Sprintf(tr(c, "%d 项"), len(rows))).FontSize(11).TextColor(c.Theme().TextMuted).Grow(1)
			actionMenu(c, chosen == nil || state.busy || !a.statusActionsAvailable(), func(m *ui.Menu) {
				if chosen != nil {
					a.containerMenu(m, *chosen, d)
				}
			})
		})
	}
	if !state.containerList {
		state.containerGrid.Selected = &state.containerSelected
		state.containerGrid.Selection = &state.containerChoice
		state.containerGrid.Key = func(i int) any { return containerIdentity{d.EngineID, rows[i].ID} }
		state.containerGrid.Label = func(i int) string { return rows[i].Names }
		containerCardLayout.CardGrid(c, &state.containerGrid, a.statusPaneWidth(c), len(rows), 148, func(i int) { a.containerCard(c, rows[i], d, metrics[rows[i].ID]) }).FillWidth().Grow(1).Label(tr(c, "Docker 容器卡片"))
		return
	}
	state.containerTable.Selected = &state.containerSelected
	state.containerTable.Selection = &state.containerChoice
	state.containerTable.Sort = &state.containerSort
	state.containerTable.Key = func(i int) any { return containerIdentity{d.EngineID, rows[i].ID} }
	state.containerTable.Label = func(i int) string { return rows[i].Names }
	components.Table(c, &state.containerTable, []ui.TableColumn{{ID: "name", Title: tr(c, "容器"), MinWidth: 110, Sortable: true}, {ID: "image", Title: tr(c, "镜像"), Width: 200, Sortable: true}, {ID: "state", Title: tr(c, "状态"), Width: 160, Sortable: true}, {ID: "ports", Title: tr(c, "端口"), Width: 180, Sortable: true}, {ID: "cpu", Title: "CPU %", Width: 80, Sortable: true}, {ID: "memory", Title: tr(c, "内存"), Width: 90, Sortable: true}, {ID: "network", Title: tr(c, "累计网络 ↓ / ↑"), Width: 180, Sortable: true}, {ID: "block", Title: tr(c, "累计磁盘 读 / 写"), Width: 180, Sortable: true}}, len(rows), func(i, col int) {
		row := rows[i]
		text := row.Names
		switch col {
		case 1:
			text = row.Image
		case 2:
			text = containerState(row.State) + " · " + row.Status
		case 3:
			text = nonEmpty(row.Ports)
		case 4:
			text = "—"
			if m := metrics[row.ID]; m.CPUValid {
				text = percentText(m.CPU)
			}
		case 5:
			text = "—"
			if m := metrics[row.ID]; m.MemoryValid {
				text = size(m.Memory)
			}
		case 6:
			text = "—"
			if m := metrics[row.ID]; m.NetworkValid {
				text = size(m.RX) + " / " + size(m.TX)
			}
		case 7:
			text = "—"
			if m := metrics[row.ID]; m.BlockValid {
				text = size(m.Read) + " / " + size(m.Write)
			}
		}
		cell := ui.Row(c).Fill().AlignItems(ui.Center).Children(func() { components.CellText(c, reviewText(text)).Tooltip(text) })
		cell.ContextMenu(func(m *ui.Menu) {
			state.containerSelected = i
			state.containerChoice.Clear()
			state.containerChoice.Add(containerIdentity{d.EngineID, row.ID})
			a.containerMenu(m, row, d)
		})
		if cell.DoubleClicked() {
			a.openContainerDetails(row, d)
		}
	}).FillWidth().Grow(1).MinHeight(0).Label(tr(c, "Docker 容器列表"))
}

func (a *app) dockerImages(c *ui.Context, d *monitor.DockerInfo, state *serverManagementUI) {
	rows := make([]monitor.Image, 0, len(d.Images))
	q := strings.ToLower(state.dockerSearch)
	for _, row := range d.Images {
		if strings.Contains(strings.ToLower(row.Repository+" "+row.Tag+" "+row.ID), q) {
			rows = append(rows, row)
		}
	}
	order := state.imageSort
	slices.SortStableFunc(rows, func(a, b monitor.Image) int {
		left, right := a.Repository, b.Repository
		switch order.Column {
		case "tag":
			left, right = a.Tag, b.Tag
		case "id":
			left, right = a.ID, b.ID
		}
		v := compareText(left, right)
		if order.Column == "size" {
			v = cmp.Compare(dockerImageSize(a.Size), dockerImageSize(b.Size))
		}
		if order.Column == "created" {
			v = dockerImageTime(a.CreatedAt).Compare(dockerImageTime(b.CreatedAt))
		}
		if order.Descending {
			v = -v
		}
		if v == 0 {
			return cmp.Compare(a.ID+a.Tag, b.ID+b.Tag)
		}
		return v
	})
	state.imageTable.Sort = &state.imageSort
	state.imageTable.Key = func(i int) any {
		return struct{ ID, Repository, Tag string }{rows[i].ID, rows[i].Repository, rows[i].Tag}
	}
	state.imageTable.Label = func(i int) string { return rows[i].Repository + ":" + rows[i].Tag }
	components.Table(c, &state.imageTable, []ui.TableColumn{{ID: "name", Title: tr(c, "镜像"), MinWidth: 120, Sortable: true}, {ID: "tag", Title: tr(c, "标签"), Width: 120, Sortable: true}, {ID: "id", Title: "ID", Width: 120, Sortable: true}, {ID: "size", Title: tr(c, "大小"), Width: 85, Align: ui.End, Sortable: true}, {ID: "created", Title: tr(c, "创建时间"), Width: 130, Sortable: true}}, len(rows), func(i, col int) {
		row := rows[i]
		text := row.Repository
		switch col {
		case 1:
			text = row.Tag
		case 2:
			text = strings.TrimPrefix(row.ID, "sha256:")
			text = text[:min(len(text), 12)]
		case 3:
			text = row.Size
		case 4:
			text = row.CreatedSince
		}
		components.CellText(c, reviewText(text)).Tooltip(text)
	}).FillWidth().Grow(1).MinHeight(0).Label(tr(c, "Docker 镜像列表"))
}

func dockerImageSize(text string) float64 { v, _ := monitor.ByteAmount(text); return v }

func dockerImageTime(text string) time.Time {
	for _, layout := range []string{"2006-01-02 15:04:05 -0700 MST", "2006-01-02 15:04:05 -0700", time.RFC3339} {
		if value, err := time.Parse(layout, text); err == nil {
			return value
		}
	}
	return time.Time{}
}

func containerState(state string) string {
	switch state {
	case "running":
		return tr(nil, "运行中")
	case "exited":
		return tr(nil, "已停止")
	case "created":
		return tr(nil, "已创建")
	case "paused":
		return tr(nil, "已暂停")
	case "restarting":
		return tr(nil, "重启中")
	case "dead":
		return tr(nil, "异常")
	case "removing":
		return tr(nil, "删除中")
	}
	return state
}
