package desktop

import (
	"context"
	"fmt"
	"strings"
	"sync/atomic"
	"time"

	"github.com/egoist/mygo/ui"
	"sshclient/internal/approval"
	"sshclient/internal/client"
	"sshclient/internal/desktop/components"
	"sshclient/internal/dockerlog"
	"sshclient/internal/monitor"
	"sshclient/internal/serverops"
)

type dockerLogView struct {
	buffer           *dockerlog.Buffer
	rows             []string
	all              []string
	query, lastQuery string
	version          uint64
	trimmed, follow  bool
	list             ui.ListState
	notify           atomic.Bool
}

type dockerDialogState struct {
	kind               string
	generation         int
	session            *session
	row                monitor.Container
	target             serverops.DockerTarget
	compose            serverops.ComposeRequest
	plan               *serverops.ComposePlan
	services           []composeServiceChoice
	job                *dockerJob
	tab                int
	shell, tail, error string
	loading, streaming bool
	detailsExpanded    bool
	sourceError        string
	log                *dockerLogView
	ctx                context.Context
	cancel             context.CancelFunc
}

func (a *app) cancelDockerDialog() {
	if s := a.prompt.docker; s != nil && s.cancel != nil {
		s.cancel()
	}
}
func (a *app) closeDockerDialog(s *dockerDialogState) {
	if s.kind == "container" {
		if s.cancel != nil {
			s.cancel()
		}
		if s.session != nil && s.session.management != nil && s.session.management.containerDetails == s {
			s.session.management.containerDetails = nil
		}
		a.invalidateWindows()
		return
	}
	if a.prompt.docker != s {
		return
	}
	if s.cancel != nil {
		s.cancel()
	}
	a.prompt = promptState{}
	a.invalidateWindows()
}
func (a *app) currentDockerDialog(s *dockerDialogState) bool {
	if s.kind == "container" {
		return a.online && s.generation == a.generation && a.hasSession(s.session) && s.session.management != nil && s.session.management.containerDetails == s
	}
	return a.online && a.prompt.open && a.prompt.docker == s && a.prompt.generation == a.generation
}

func (a *app) openContainerDetails(row monitor.Container, d *monitor.DockerInfo) {
	sess := a.serverStateSession()
	if sess == nil {
		return
	}
	state := a.managementState()
	if old := state.containerDetails; old != nil && old.cancel != nil {
		old.cancel()
	}
	s := &dockerDialogState{kind: "container", generation: a.generation, session: sess, row: row, target: serverops.DockerTarget{Socket: d.Socket, Engine: d.EngineID, ID: row.ID}, shell: "sh", tail: "200 行"}
	state.containerDetails = s
	a.diagnosticTab = 4
	a.startContainerLogs(s)
}

// At most one pending UI notification per log view, even when a container
// produces output faster than the window can render.
func (a *app) logWriter(ctx context.Context, l *dockerLogView) *dockerLogWriter {
	return &dockerLogWriter{log: l, notify: func() {
		if l.notify.CompareAndSwap(false, true) {
			time.AfterFunc(100*time.Millisecond, func() {
				a.postUI(func() {
					l.notify.Store(false)
					if ctx.Err() == nil {
						a.invalidateWindows()
					}
				})
			})
		}
	}}
}

type dockerLogWriter struct {
	log    *dockerLogView
	notify func()
}

func (w *dockerLogWriter) Write(p []byte) (int, error) {
	n, err := w.log.buffer.Write(p)
	w.notify()
	return n, err
}

func (a *app) startContainerLogs(s *dockerDialogState) {
	if s.cancel != nil {
		s.cancel()
	}
	ctx, cancel := context.WithCancel(a.operationContext())
	s.ctx, s.cancel = ctx, cancel
	s.log = &dockerLogView{buffer: &dockerlog.Buffer{}, follow: true}
	s.streaming = true
	s.error = ""
	if a.win == nil && a.dispatch == nil {
		return
	}
	tail := 200
	if s.tail == "1000 行" {
		tail = 1000
	}
	if s.tail == "2000 行" {
		tail = 2000
	}
	op := approval.Operation{Host: s.session.host, Endpoint: s.session.target.Endpoint, HostFingerprint: s.session.target.HostFingerprint, Kind: "docker-logs", Docker: serverops.DockerRequest{Target: s.target, Tail: tail, Follow: true}}
	writer := a.logWriter(ctx, s.log)
	a.beginContext(ctx, op, func(stream *client.Stream) (func(), error) {
		defer stream.Close()
		code, _, err := stream.Copy(writer, writer)
		return func() {
			if a.currentDockerDialog(s) && s.ctx == ctx {
				s.streaming = false
				if err != nil {
					s.error = err.Error()
				} else if code != 0 {
					s.error = fmt.Sprintf(tr(nil, "日志读取失败（退出码 %d），请查看返回信息"), code)
				}
			}
		}, nil
	}, func(err error) {
		if a.currentDockerDialog(s) && s.ctx == ctx {
			s.streaming = false
			s.error = err.Error()
		}
	})
}

func logRows(l *dockerLogView) []string {
	version := l.buffer.Version()
	changed := version != l.version || l.all == nil
	if changed {
		l.all, l.version, l.trimmed = l.buffer.Snapshot()
	}
	if changed || l.query != l.lastQuery || l.rows == nil {
		l.rows = l.rows[:0]
		query := strings.ToLower(l.query)
		for _, line := range l.all {
			if query == "" || strings.Contains(strings.ToLower(line), query) {
				l.rows = append(l.rows, line)
			}
		}
		if l.follow && len(l.rows) > 0 {
			l.list.ScrollTo(len(l.rows)-1, ui.End)
		}
		l.lastQuery = l.query
	}
	return l.rows
}

func dockerLogSurface(c *ui.Context, l *dockerLogView) {
	_, windowHeight := c.Size()
	ui.Column(c).FillWidth().Height(max(130, min(310, windowHeight-315))).Gap(6).Children(func() {
		ui.Row(c).FillWidth().Gap(8).AlignItems(ui.Center).Children(func() {
			ui.TextInput(c, &l.query).Grow(1).Placeholder(tr(c, "搜索日志")).Label(tr(c, "搜索日志"))
			if components.Button(c, tr(c, "复制"), components.ButtonOptions{Variant: components.Quiet}).Label(tr(c, "复制筛选日志")).Clicked() {
				lines := append([]string(nil), logRows(l)...)
				for i := range lines {
					lines[i] = reviewText(lines[i])
				}
				c.WriteClipboard(strings.Join(lines, "\n"))
			}
			if ui.Checkbox(c, &l.follow, tr(c, "跟随最新")).Changed() && l.follow && len(l.rows) > 0 {
				l.list.ScrollTo(len(l.rows)-1, ui.End)
			}
		})
		dockerLogList(c, l)
	})
}

func dockerLogList(c *ui.Context, l *dockerLogView) {
	rows := logRows(l)
	l.list.Key = func(i int) any { return i }
	l.list.Label = func(i int) string { return fmt.Sprintf(tr(c, "日志第 %d 行"), i+1) }
	components.List(c, &l.list, len(rows), func(i int) {
		ui.Text(c, reviewText(rows[i])).Font("monospace").FontSize(12).Selectable().SingleLine().FillWidth().Height(22).Tooltip(reviewText(rows[i]))
	}).FillWidth().Grow(1).MinHeight(0).Border(1, c.Theme().Border).Radius(5).Label(tr(c, "日志内容"))
	if len(rows) == 0 {
		ui.Text(c, tr(c, "暂无匹配日志")).FontSize(11).TextColor(c.Theme().TextMuted)
	}
	if l.trimmed {
		ui.Text(c, tr(c, "仅保留最近日志，较早内容已移出显示缓存")).FontSize(10).TextColor(c.Theme().TextMuted)
	}
}

func (a *app) dockerDialogView(c *ui.Context) {
	s := a.prompt.docker
	if s == nil {
		return
	}
	if s.kind == "task" {
		a.dockerJobDialog(c, s)
		return
	}
	if s.kind == "compose" {
		a.composeDialogView(c, s)
		return
	}
}

// Routine inspection stays inside the existing tool pane. Only update review
// and explicitly opened task details use a modal; neither resizes the terminal.
func (a *app) containerDetailsView(c *ui.Context, s *dockerDialogState) {
	ui.Column(c).FillWidth().Grow(1).MinHeight(0).Gap(5).Label(tr(c, "容器日志详情")).Children(func() {
		ui.Row(c).FillWidth().Height(30).Shrink(0).Gap(6).AlignItems(ui.Center).Children(func() {
			back := tr(c, "返回容器列表")
			if state := a.managementState(); state.dockerTab == 2 && state.composeProject != "" {
				back = tr(c, "返回 Compose 项目")
			}
			if components.Button(c, "‹", components.ButtonOptions{Variant: components.Quiet}).Label(back).Tooltip(back).Size(26, 26).Clicked() {
				a.closeDockerDialog(s)
			}
			ui.Text(c, reviewText(s.row.Names)).FontSize(12).Bold().SingleLine().Grow(1).MinWidth(0).Tooltip(s.row.Names)
			ui.Tabs(c, &s.tab, tr(c, "日志"), tr(c, "详情"))
			components.Button(c, tr(c, "终端 ▾"), components.ButtonOptions{Variant: components.Quiet}).Label(tr(c, "打开容器终端")).Disabled(s.row.State != "running" || s.session.closed).Menu(func(m *ui.Menu) {
				for _, shell := range []string{"sh", "bash"} {
					if m.Item(shell).Chosen() {
						a.openContainerTerminal(s.session, s.target, s.row.Names, shell, nil)
					}
				}
			})
		})
		if s.tab == 0 && s.log != nil {
			l := s.log
			ui.Row(c).FillWidth().Shrink(0).Wrap().Gap(4).AlignItems(ui.Center).Children(func() {
				ui.TextInput(c, &l.query).Grow(1).MinWidth(100).Placeholder(tr(c, "搜索日志")).Label(tr(c, "搜索日志"))
				if sourceSelect(c, &s.tail, []string{"200 行", "1000 行", "2000 行"}).Width(104).MinWidth(0).Label(tr(c, "日志行数")).Changed() {
					a.startContainerLogs(s)
				}
				label := tr(c, "继续接收")
				if s.streaming {
					label = tr(c, "暂停接收")
				}
				if components.Button(c, label).Height(28).Padding(3, 6).Clicked() {
					if s.streaming {
						s.cancel()
						s.streaming = false
					} else {
						a.startContainerLogs(s)
					}
				}
				if components.SVGButton(c, components.RefreshIcon, tr(c, "刷新日志")).Size(26, 26).Clicked() {
					a.startContainerLogs(s)
				}
				if components.Button(c, tr(c, "复制"), components.ButtonOptions{Variant: components.Quiet}).Height(28).Padding(3, 4).Label(tr(c, "复制筛选日志")).Clicked() {
					lines := append([]string(nil), logRows(l)...)
					for i := range lines {
						lines[i] = reviewText(lines[i])
					}
					c.WriteClipboard(strings.Join(lines, "\n"))
				}
				if ui.Checkbox(c, &l.follow, tr(c, "跟随")).Label(tr(c, "跟随最新")).Changed() && l.follow && len(l.rows) > 0 {
					l.list.ScrollTo(len(l.rows)-1, ui.End)
				}
			})
			dockerLogList(c, s.log)
		} else {
			ui.Scroll(c).FillWidth().Grow(1).MinHeight(0).Children(func() {
				ui.Column(c).FillWidth().Gap(6).Children(func() {
					containerInfoLayout.Grid(c, a.statusPaneWidth(c), 2, func(i int) {
						if i == 0 {
							components.Surface(c, tr(c, "容器基本信息")).Children(func() {
								sectionTitle(c, tr(c, "容器信息"))
								infoRows(c, [][2]string{{tr(c, "状态"), containerState(s.row.State) + " · " + s.row.Status}, {tr(c, "镜像"), s.row.Image}, {tr(c, "Compose 项目"), nonEmpty(s.row.Project)}, {tr(c, "容器 ID"), s.target.ID}})
							})
						} else {
							components.Surface(c, tr(c, "容器端口映射")).Children(func() {
								sectionTitle(c, tr(c, "端口映射"))
								for _, mapping := range strings.Split(nonEmpty(s.row.Ports), ",") {
									mapping = strings.TrimSpace(mapping)
									ui.Text(c, reviewText(mapping)).FillWidth().MinWidth(0).FontSize(12).Selectable().Tooltip(mapping)
								}
							})
						}
					}).Label(tr(c, "容器详情卡片"))
				})
			})
		}
		if s.error != "" {
			ui.Text(c, reviewText(s.error)).FontSize(11).FillWidth().TextColor(c.Theme().Danger)
		}
	})
}

func (a *app) openContainerTerminal(source *session, target serverops.DockerTarget, name, shell string, replace *session) {
	if source == nil || !a.hasSession(source) || !a.online {
		return
	}
	owner, err := a.unlockedOwner()
	if err != nil {
		a.status = err.Error()
		return
	}
	a.ensureTerminalGroups()
	group := a.activeTerminalGroup
	focus := a.currentTerminal()
	if replace != nil {
		group = replace.group
	}
	ctx, cancel := context.WithCancel(a.operationContext())
	cli, gen := a.client, a.generation
	op := approval.Operation{Host: source.host, Endpoint: source.target.Endpoint, HostFingerprint: source.target.HostFingerprint, Kind: "docker-terminal", ServerStatus: true, Docker: serverops.DockerRequest{Target: target, Shell: shell}}
	a.status = tr(nil, "正在打开容器终端…")
	go func() {
		stream, err := cli.StartWithOwner(ctx, op, owner)
		a.postUI(func() {
			if gen != a.generation || !a.online || ctx.Err() != nil || !a.hasSession(source) {
				cancel()
				if stream != nil {
					stream.Close()
				}
				return
			}
			if err != nil {
				cancel()
				a.status = err.Error()
				return
			}
			attached := a.attachTerminalInGroup(stream, source.host, cancel, false, group)
			if attached == nil {
				cancel()
				return
			}
			attached.name, attached.containerName = tr(nil, "容器 · ")+name, name
			for n := 2; ; n++ {
				collision := false
				for _, existing := range a.sessions {
					if existing != attached && existing != replace && existing.name == attached.name {
						collision = true
						break
					}
				}
				if !collision {
					break
				}
				attached.name = fmt.Sprintf(tr(nil, "容器 · %s (%d)"), name, n)
			}
			if replace != nil {
				a.replaceClosedTerminal(replace, attached)
			}
			if a.currentTerminal() == focus && a.page == 1 && !a.editShown {
				a.activateTerminal(attached, true)
			}
			a.status = tr(nil, "容器终端已打开")
		})
	}()
}
