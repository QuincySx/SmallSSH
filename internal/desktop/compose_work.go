package desktop

import (
	"context"
	"errors"
	"fmt"
	"io"
	"slices"
	"strings"
	"time"

	"github.com/egoist/mygo/ui"
	"sshclient/internal/approval"
	"sshclient/internal/client"
	"sshclient/internal/desktop/components"
	"sshclient/internal/dockerlog"
	"sshclient/internal/monitor"
	"sshclient/internal/serverops"
)

type composeServiceChoice struct {
	Name, Image, State string
	Selected           bool
}
type dockerJob struct {
	title, host, endpoint, phase, status, error string
	op                                          approval.Operation
	log                                         *dockerLogView
	result                                      []serverops.ComposeStatus
	started                                     time.Time
	cancel                                      context.CancelFunc
}

func (a *app) cancelDockerWork() {
	a.cancelDockerDialog()
	for _, sess := range a.sessions {
		if sess.management != nil && sess.management.containerDetails != nil {
			a.closeDockerDialog(sess.management.containerDetails)
		}
	}
	for _, job := range a.dockerJobs {
		if job.cancel != nil {
			job.cancel()
		}
	}
	a.dockerJobs = nil
	a.activityTab = 0
}

func (a *app) openComposeUpdate(rows []monitor.Container, d *monitor.DockerInfo) {
	if len(rows) == 0 || a.serverStateSession() == nil {
		return
	}
	sess := a.serverStateSession()
	representative := rows[0]
	for _, row := range rows {
		if row.State == "running" {
			representative = row
			break
		}
	}
	a.ask(tr(nil, "更新 Compose"), a.hostName(sess.host)+" · "+composeProject(representative), "", "", "", "", nil)
	s := &dockerDialogState{kind: "compose", session: sess, compose: serverops.ComposeRequest{Target: serverops.DockerTarget{Socket: d.Socket, Engine: d.EngineID, ID: representative.ID}, Project: composeProject(representative), Directory: representative.ComposeDir, Files: representative.ComposeFiles}}
	s.sourceError = composeUpdateIssue(rows, d)
	choices := map[string]composeServiceChoice{}
	for _, row := range rows {
		if row.Service == "" {
			s.sourceError = tr(nil, "项目来源标签不完整，请刷新 Docker 清单后重试")
			continue
		}
		choice := choices[row.Service]
		choice.Name, choice.Image = row.Service, row.Image
		if row.State == "running" {
			choice.Selected = true
			choice.State = "运行中"
		} else if choice.State == "" {
			choice.State = "已停止"
		}
		choices[row.Service] = choice
	}
	for _, choice := range choices {
		s.services = append(s.services, choice)
	}
	slices.SortFunc(s.services, func(a, b composeServiceChoice) int { return strings.Compare(a.Name, b.Name) })
	a.prompt.docker = s
}

func (s *dockerDialogState) composeSelection() serverops.ComposeRequest {
	r := s.compose
	r.ConfigHash = ""
	names := []string{}
	for _, choice := range s.services {
		if choice.Selected {
			names = append(names, choice.Name)
		}
	}
	r.Services = strings.Join(names, ",")
	return r
}

func (a *app) loadComposePlan(s *dockerDialogState) {
	if s.loading || !a.currentDockerDialog(s) {
		return
	}
	r := s.composeSelection()
	if s.sourceError != "" {
		s.error = s.sourceError
		return
	}
	if err := r.Validate(false); err != nil {
		s.error = err.Error()
		return
	}
	if s.cancel != nil {
		s.cancel()
	}
	ctx, cancel := context.WithTimeout(a.operationContext(), 40*time.Second)
	s.ctx, s.cancel = ctx, cancel
	s.loading, s.error, s.plan = true, "", nil
	if a.win == nil && a.dispatch == nil {
		return
	}
	op := approval.Operation{Host: s.session.host, Endpoint: s.session.target.Endpoint, HostFingerprint: s.session.target.HostFingerprint, Kind: "compose-plan", Compose: r}
	owner, err := a.unlockedOwner()
	if err != nil {
		s.loading = false
		s.error = err.Error()
		cancel()
		return
	}
	cli, gen := a.client, a.generation
	go func() {
		defer cancel()
		stream, err := cli.StartWithOwner(ctx, op, owner)
		var plan *serverops.ComposePlan
		if err == nil {
			defer stream.Close()
			for {
				var mErr error
				message, e := stream.Receive()
				mErr = e
				if mErr != nil {
					err = mErr
					break
				}
				if message.ComposePlan != nil {
					plan = message.ComposePlan
				}
				if message.Action == "done" {
					break
				}
			}
		}
		a.postUI(func() {
			if gen != a.generation || !a.currentDockerDialog(s) || s.ctx != ctx {
				return
			}
			s.loading = false
			if err != nil {
				s.error = err.Error()
				return
			}
			if plan == nil {
				s.error = tr(nil, "未收到更新计划")
				return
			}
			s.compose = r
			s.plan = plan
		})
	}()
}

// Compare the selected containers with the actual configuration plan, without
// assuming the current container tags are also the images about to be pulled.
func composePlanChangesImages(s *dockerDialogState) bool {
	if s.plan == nil {
		return false
	}
	current := []string{}
	for _, choice := range s.services {
		if choice.Selected {
			current = append(current, choice.Image)
		}
	}
	planned := slices.Clone(s.plan.Images)
	slices.Sort(current)
	slices.Sort(planned)
	return !slices.Equal(slices.Compact(current), slices.Compact(planned))
}

func (a *app) composeDialogView(c *ui.Context, s *dockerDialogState) {
	imagesChanged := composePlanChangesImages(s)
	height := float32(300 + len(s.services)*28)
	if s.detailsExpanded {
		height += 110
		if s.plan != nil && !imagesChanged {
			height += min(120, float32(len(s.plan.Images))*20)
		}
	}
	if imagesChanged {
		height += 40 + min(120, float32(len(s.plan.Images))*20)
	}
	components.Dialog(c, &a.prompt.open, components.DialogOptions{Title: tr(c, "更新 Compose"), Target: reviewText(a.prompt.target), Width: 680, Height: min(540, height), Close: func() { a.closeDockerDialog(s) }}, func() {
		ui.Column(c).FillWidth().Gap(10).Children(func() {
			ui.Text(c, tr(c, "服务")).FontSize(13).Bold()
			for i := range s.services {
				choice := &s.services[i]
				ui.Row(c).FillWidth().Gap(8).AlignItems(ui.Center).Children(func() {
					before := choice.Selected
					ui.Checkbox(c, &choice.Selected, reviewText(choice.Name)).Label(tr(c, "更新服务 ") + choice.Name).Disabled(s.loading)
					if before != choice.Selected {
						s.plan = nil
						s.error = ""
					}
					ui.Text(c, reviewText(choice.Image)).Grow(1).SingleLine().FontSize(11).TextColor(c.Theme().TextMuted).Tooltip(tr(c, "当前容器镜像"))
					ui.Text(c, tr(c, choice.State)).FontSize(11).TextColor(c.Theme().TextMuted)
				})
			}
			ui.Text(c, tr(c, "将拉取镜像并重建所选服务，可能短暂中断。")).FillWidth().FontSize(12).TextColor(c.Theme().Warning)
			if s.loading {
				ui.Text(c, tr(c, "正在核对配置与镜像…")).FontSize(12)
			}
			if s.plan != nil && imagesChanged {
				ui.Text(c, tr(c, "计划镜像与当前容器不同")).FillWidth().FontSize(12).TextColor(c.Theme().Warning)
				for _, image := range s.plan.Images {
					ui.Text(c, reviewText(image)).FillWidth().FontSize(12).Selectable()
				}
			}
			ui.Collapsible(c, tr(c, "配置与计划详情"), &s.detailsExpanded, func() {
				ui.Text(c, reviewText(s.compose.Directory)).FillWidth().FontSize(11).Selectable().TextColor(c.Theme().TextMuted)
				ui.Text(c, tr(c, "沿用配置中的镜像标签或摘要；保留挂载，不启动未选服务，不自动构建。")).FillWidth().FontSize(11).TextColor(c.Theme().TextMuted)
				if s.plan != nil {
					if !imagesChanged {
						ui.Text(c, tr(c, "计划镜像")).FontSize(12).Bold()
						for _, image := range s.plan.Images {
							ui.Text(c, reviewText(image)).FillWidth().FontSize(12).Selectable()
						}
					}
					ui.Text(c, tr(c, "预览仅核对配置，尚未拉取镜像；更新后核对运行与健康状态。")).FillWidth().FontSize(11).TextColor(c.Theme().TextMuted)
				}
			})
			if s.sourceError != "" {
				ui.Text(c, reviewText(s.sourceError)).FillWidth().FontSize(12).TextColor(c.Theme().Danger)
			}
			if s.error != "" {
				ui.Text(c, reviewText(s.error)).FillWidth().FontSize(12).TextColor(c.Theme().Danger)
			}
		})
	}, func() {
		if components.Button(c, tr(c, "取消")).Clicked() {
			a.closeDockerDialog(s)
			return
		}
		if components.Button(c, tr(c, "预览更新"), components.ButtonOptions{Busy: s.loading}).Clicked() {
			a.loadComposePlan(s)
		}
		if components.PrimaryButton(c, tr(c, "开始更新")).Disabled(s.plan == nil || s.loading || s.session.closed).Clicked() {
			a.startComposeJob(s)
		}
	})
	if !a.prompt.open {
		a.closeDockerDialog(s)
	}
}

func (a *app) startComposeJob(s *dockerDialogState) {
	if s.plan == nil || !a.currentDockerDialog(s) || s.loading {
		return
	}
	if s.session == nil || s.session.closed || !a.hasSession(s.session) {
		s.error = tr(nil, "原连接已关闭，请重新选择更新目标")
		return
	}
	r := s.compose
	r.ConfigHash = s.plan.Hash
	if s.composeSelection().Services != r.Services {
		s.plan = nil
		s.error = tr(nil, "服务选择已变化，请重新检查")
		return
	}
	for _, job := range a.dockerJobs {
		if job.status == "running" && job.host == s.session.host && job.op.Compose.Project == r.Project && job.op.Compose.Target.Engine == r.Target.Engine {
			s.error = tr(nil, "此项目已有更新正在执行")
			return
		}
	}
	owner, err := a.unlockedOwner()
	if err != nil {
		s.error = err.Error()
		return
	}
	op := approval.Operation{Host: s.session.host, Endpoint: s.session.target.Endpoint, HostFingerprint: s.session.target.HostFingerprint, Kind: "compose-update", Compose: r}
	ctx, cancel := context.WithTimeout(a.operationContext(), 12*time.Minute)
	job := &dockerJob{title: tr(nil, "更新 ") + r.Project, host: s.session.host, endpoint: s.session.target.Endpoint, phase: "checking", status: "running", op: op, log: &dockerLogView{buffer: &dockerlog.Buffer{}, follow: true}, started: time.Now(), cancel: cancel}
	if len(a.dockerJobs) >= 20 {
		removed := false
		for i, old := range a.dockerJobs {
			if old.status != "running" {
				a.dockerJobs = append(a.dockerJobs[:i], a.dockerJobs[i+1:]...)
				removed = true
				break
			}
		}
		if !removed {
			cancel()
			s.error = tr(nil, "进行中的更新任务过多，请等待一项完成")
			return
		}
	}
	a.dockerJobs = append(a.dockerJobs, job)
	a.activityTab = 1
	source := s.session
	state := source.management
	if state == nil {
		state = newManagementUI()
		source.management = state
	}
	state.busy = true
	state.message = job.title + tr(nil, " · 正在检查")
	a.closeDockerDialog(s)
	cli, gen := a.client, a.generation
	writer := a.logWriter(ctx, job.log)
	go func() {
		stream, err := cli.StartWithOwner(ctx, op, owner)
		var result []serverops.ComposeStatus
		if err == nil {
			defer stream.Close()
			for {
				message, e := stream.Receive()
				if e != nil {
					err = e
					break
				}
				switch message.Action {
				case "stdout", "stderr":
					writer.Write(message.Data)
				case "compose-phase":
					phase := message.Phase
					a.postUI(func() {
						if gen == a.generation {
							job.phase = phase
							state.message = job.title + " · " + composePhase(phase)
						}
					})
				case "compose-result":
					result = message.ComposeStatus
				}
				if message.Action == "done" {
					if message.ExitCode != 0 {
						err = fmt.Errorf(tr(nil, "更新退出码 %d"), message.ExitCode)
					}
					break
				}
			}
		}
		interrupted := ctx.Err() != nil
		cancel()
		a.postUI(func() {
			if gen != a.generation || !a.online {
				return
			}
			state.busy = false
			job.result = result
			if interrupted {
				job.status = "error"
				job.error = tr(nil, "已停止等待，请核对服务器实际状态")
			} else if err != nil {
				job.status = "error"
				job.error = err.Error()
			} else {
				job.status = "done"
				job.phase = "done"
			}
			state.message = job.title + " · " + composeJobState(job)
			if job.error != "" {
				state.message += " · " + job.error
			}
			a.refreshDockerInventory(source)
		})
	}()
}

func composePhase(phase string) string {
	switch phase {
	case "checking":
		return tr(nil, "核对配置")
	case "pull":
		return tr(nil, "下载镜像")
	case "up":
		return tr(nil, "重建并等待就绪")
	case "verify":
		return tr(nil, "核对服务状态")
	case "done":
		return tr(nil, "更新完成")
	}
	return phase
}
func composeJobState(job *dockerJob) string {
	if job.status == "done" {
		return tr(nil, "更新完成")
	}
	if job.status == "error" {
		return tr(nil, "需要检查")
	}
	return composePhase(job.phase)
}

func (a *app) refreshDockerInventory(sess *session) {
	if !a.hasSession(sess) || sess.closed {
		return
	}
	ctx, cancel := context.WithTimeout(a.operationContext(), 20*time.Second)
	a.beginContext(ctx, approval.Operation{Host: sess.host, Kind: "exec", Endpoint: sess.target.Endpoint, HostFingerprint: sess.target.HostFingerprint, Command: monitor.InventoryCommand}, func(stream *client.Stream) (func(), error) {
		defer stream.Close()
		var out, stderr managementOutput
		out.limit, stderr.limit = 2<<20, 16<<10
		code, _, err := stream.Copy(&out, &stderr)
		if err != nil {
			return nil, err
		}
		if code != 0 {
			return nil, errors.New(tr(nil, "Docker 清单稍后自动刷新"))
		}
		info := monitor.ParseInventory(out.String())
		return func() {
			defer cancel()
			if a.hasSession(sess) && !sess.closed && sess.stats != nil {
				a.updateServerInventory(sess, &info)
			}
		}, nil
	}, func(error) { cancel() })
}

func (a *app) dockerJobsView(c *ui.Context) {
	ui.Scroll(c).FillWidth().Grow(1).Children(func() {
		ui.Column(c).FillWidth().Gap(10).Children(func() {
			for i := len(a.dockerJobs) - 1; i >= 0; i-- {
				job := a.dockerJobs[i]
				ui.Column(c).FillWidth().Gap(5).Padding(8).Border(1, c.Theme().Border).Radius(6).Children(func() {
					ui.Row(c).FillWidth().AlignItems(ui.Center).Gap(8).Children(func() {
						ui.Text(c, reviewText(job.title)).FontSize(12).Bold().Grow(1)
						if components.Button(c, tr(c, "详情")).Label(tr(c, "任务详情 ") + job.title).Clicked() {
							a.openDockerJob(job)
						}
					})
					ui.Text(c, reviewText(a.hostName(job.host)+" · "+composeJobState(job))).SingleLine().FontSize(11).TextColor(c.Theme().TextMuted)
				})
			}
		})
	})
}
func (a *app) openDockerJob(job *dockerJob) {
	a.transfersOpen = false
	a.ask(tr(nil, "更新任务"), a.hostName(job.host)+" · "+job.endpoint, "", "", "", "", nil)
	a.prompt.docker = &dockerDialogState{kind: "task", job: job}
}
func (a *app) dockerJobDialog(c *ui.Context, s *dockerDialogState) {
	job := s.job
	components.Dialog(c, &a.prompt.open, components.DialogOptions{Title: job.title, Target: reviewText(a.prompt.target), Width: 820, Height: 580, Close: func() { a.closeDockerDialog(s) }}, func() {
		ui.Column(c).FillWidth().Gap(10).Children(func() {
			ui.Text(c, composeJobState(job)).FontSize(13).Bold()
			if job.error != "" {
				ui.Text(c, reviewText(job.error)).FillWidth().FontSize(12).TextColor(c.Theme().Danger)
			}
			dockerLogSurface(c, job.log)
			for _, row := range job.result {
				health := row.Health
				if health == "" {
					health = tr(c, "未配置健康检查")
				}
				ui.Text(c, reviewText(row.Service+" · "+row.State+" · "+health)).FontSize(11).FillWidth()
			}
		})
	}, func() {
		if job.status == "running" && components.Button(c, tr(c, "停止等待")).Clicked() {
			job.cancel()
		}
		if components.Button(c, tr(c, "关闭")).Clicked() {
			a.closeDockerDialog(s)
		}
	})
	if !a.prompt.open {
		a.closeDockerDialog(s)
	}
}

var _ io.Writer = (*dockerLogWriter)(nil)
