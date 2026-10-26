package desktop

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/egoist/mygo/ui"
	"sshclient/internal/approval"
	"sshclient/internal/desktop/components"
	"sshclient/internal/monitor"
	"sshclient/internal/serverops"
)

type serverManagementUI struct {
	containerDetails                                       *dockerDialogState
	processTable, serviceTable, containerTable, imageTable ui.ListState
	processSelected, serviceSelected, containerSelected    int
	dockerTab                                              int
	dockerSearch                                           string
	containerSort, imageSort                               ui.SortOrder
	busy                                                   bool
	message                                                string
	processChoice                                          ui.Selection[processIdentity]
	serviceChoice                                          ui.Selection[string]
	containerChoice                                        ui.Selection[containerIdentity]
	containerGrid                                          ui.GridState
	composeGrid, composeContainers                         ui.GridState
	composeProject, composeEngine, composeSocket           string
	containerList                                          bool
	virtualOpen                                            bool
	networkList                                            ui.ListState
	resourceOverviewScroll, resourceDetailScroll           ui.ScrollState
	resourceViewportHeight                                 float32
	resourceViewportWidth                                  float32
}

type processIdentity struct {
	PID   int
	Ticks uint64
	Boot  string
}
type containerIdentity struct{ Engine, ID string }

func newManagementUI() *serverManagementUI {
	return &serverManagementUI{processSelected: -1, serviceSelected: -1, containerSelected: -1,
		containerSort: ui.SortOrder{Column: "name"}, imageSort: ui.SortOrder{Column: "name"}}
}

func (a *app) managementState() *serverManagementUI {
	if s := a.serverStateSession(); s != nil {
		if s.management == nil {
			s.management = newManagementUI()
		}
		return s.management
	}
	if a.managementFallback == nil {
		a.managementFallback = newManagementUI()
	}
	return a.managementFallback
}

func (a *app) statusActionsAvailable() bool {
	s := a.serverStateSession()
	return a.online && s != nil && !s.closed && s.target.Endpoint != "" && s.target.HostFingerprint != ""
}

func actionMenu(c *ui.Context, disabled bool, menu func(*ui.Menu)) {
	components.Button(c, tr(c, "操作 ▾")).Label(tr(c, "列表操作")).Disabled(disabled).Menu(menu)
}

func (a *app) processMenu(m *ui.Menu, p monitor.Process, stats *monitor.Stats) {
	_, err := serverops.KillProcess(p.PID, p.StartTicks, stats.BootID, p.Name, "TERM")
	for _, action := range []struct{ label, signal string }{{tr(nil, "结束进程…"), "TERM"}, {tr(nil, "强制结束…"), "KILL"}} {
		if m.Item(action.label).Disabled(err != nil || !a.statusActionsAvailable() || a.managementState().busy).Chosen() {
			a.confirmProcess(p, stats.BootID, action.signal)
		}
	}
}

func (a *app) confirmProcess(p monitor.Process, boot, signal string) {
	command, err := serverops.KillProcess(p.PID, p.StartTicks, boot, p.Name, signal)
	if err != nil {
		a.status = err.Error()
		return
	}
	title, message := tr(nil, "结束进程"), tr(nil, "请求进程正常退出。由服务或容器托管的进程可能被自动重启。")
	if signal == "KILL" {
		title = tr(nil, "强制结束进程")
		message = tr(nil, "立即结束进程，未保存的数据可能丢失。")
	}
	a.confirmServerAction(title, fmt.Sprintf("%s · PID %d", p.Name, p.PID), message, command, true)
}

func (a *app) serviceMenu(m *ui.Menu, s monitor.Service, manager string) {
	for _, action := range []struct{ label, verb string }{{tr(nil, "启动服务…"), "start"}, {tr(nil, "停止服务…"), "stop"}, {tr(nil, "重启服务…"), "restart"}, {tr(nil, "开启自启…"), "enable"}, {tr(nil, "关闭自启…"), "disable"}} {
		_, err := serverops.Service(s.Name, action.verb)
		disabled := manager != "systemd" || err != nil || !a.statusActionsAvailable() || a.managementState().busy
		if action.verb == "enable" {
			disabled = disabled || s.Enabled != "disabled"
		}
		if action.verb == "disable" {
			disabled = disabled || (s.Enabled != "enabled" && s.Enabled != "enabled-runtime")
		}
		if action.verb == "start" {
			disabled = disabled || s.Active == "active" || s.Enabled == "masked" || s.Enabled == "masked-runtime"
		}
		if action.verb == "stop" {
			disabled = disabled || s.Active == "inactive"
		}
		if m.Item(action.label).Disabled(disabled).Chosen() {
			a.confirmService(s, action.verb)
		}
	}
}

func (a *app) confirmService(s monitor.Service, action string) {
	command, err := serverops.Service(s.Name, action)
	if err != nil {
		a.status = err.Error()
		return
	}
	if action == "disable" && s.Enabled == "enabled-runtime" {
		command = strings.Replace(command, "--no-ask-password ", "--no-ask-password --runtime ", 1)
	}
	titles := map[string]string{"start": tr(nil, "启动服务"), "stop": tr(nil, "停止服务"), "restart": tr(nil, "重启服务"), "enable": tr(nil, "开启自启"), "disable": tr(nil, "关闭自启")}
	messages := map[string]string{"start": tr(nil, "启动当前服务。"), "stop": tr(nil, "停止当前运行，相关功能将暂时不可用。"), "restart": tr(nil, "先停止再启动，相关功能可能短暂中断。"), "enable": tr(nil, "设置为开机自启。如需现在运行，可再选择启动服务。"), "disable": tr(nil, "取消自启设置。当前运行的服务继续运行，仍可能由其他服务唤起。")}
	if s.Name == "ssh.service" || s.Name == "sshd.service" {
		messages[action] += tr(nil, " SSH 服务停止后可能无法重新连接。")
	}
	a.confirmServerAction(titles[action], s.Name, messages[action], command, action == "stop" || action == "restart" || action == "disable")
}

func (a *app) containerMenu(m *ui.Menu, row monitor.Container, d *monitor.DockerInfo) {
	valid := serverops.DockerTarget{Socket: d.Socket, Engine: d.EngineID, ID: row.ID}.Validate() == nil && a.statusActionsAvailable()
	if m.Item(tr(nil, "日志与详情")).Disabled(!valid).Chosen() {
		a.openContainerDetails(row, d)
	}
	if m.Item(tr(nil, "打开容器终端")).Disabled(!valid || row.State != "running").Chosen() {
		a.openContainerTerminal(a.serverStateSession(), serverops.DockerTarget{Socket: d.Socket, Engine: d.EngineID, ID: row.ID}, row.Names, "sh", nil)
	}
	m.Separator()
	for _, action := range []struct{ label, verb string }{{tr(nil, "启动容器…"), "start"}, {tr(nil, "停止容器…"), "stop"}, {tr(nil, "重启容器…"), "restart"}} {
		_, err := serverops.DockerContainer(d.Socket, d.EngineID, row.ID, action.verb)
		disabled := err != nil || d.Status != "ready" || !a.statusActionsAvailable() || a.managementState().busy
		if action.verb == "start" {
			disabled = disabled || row.State == "running" || row.State == "paused" || row.State == "restarting"
		}
		if action.verb != "start" {
			disabled = disabled || row.State != "running"
		}
		if m.Item(action.label).Disabled(disabled).Chosen() {
			a.confirmContainer(row, d, action.verb)
		}
	}
}

func (a *app) confirmContainer(row monitor.Container, d *monitor.DockerInfo, action string) {
	command, err := serverops.DockerContainer(d.Socket, d.EngineID, row.ID, action)
	if err != nil {
		a.status = err.Error()
		return
	}
	titles := map[string]string{"start": tr(nil, "启动容器"), "stop": tr(nil, "停止容器"), "restart": tr(nil, "重启容器")}
	message := tr(nil, "启动这个容器。")
	if action != "start" {
		message = tr(nil, "容器内的服务会中断。先请求正常停止，10 秒后仍未退出会强制结束。")
	}
	a.confirmServerAction(titles[action], row.Names+" · "+row.ID[:12], message, command, action != "start")
}

// Capture the actual session and command before the modal opens. Switching tabs
// or selecting a different row cannot redirect the reviewed operation.
func (a *app) confirmServerAction(title, target, message, command string, danger bool) {
	a.confirmServerActionWithTimeout(title, target, message, command, danger, 40*time.Second)
}

func (a *app) confirmServerActionWithTimeout(title, target, message, command string, danger bool, timeout time.Duration) {
	sess := a.serverStateSession()
	if !a.statusActionsAvailable() {
		return
	}
	name := sess.host
	for _, connection := range a.connections {
		if connection.ID == sess.host && connection.Name != "" {
			name = connection.Name
			break
		}
	}
	a.ask(title+"？", name+" · "+sess.target.Endpoint+" · "+target, message, "", "", title, func(string) {
		if !a.hasSession(sess) || sess.closed {
			a.status = tr(nil, "原连接已关闭，请重新选择操作目标")
			return
		}
		a.runServerActionWithTimeout(sess, title, command, timeout)
	})
	a.prompt.danger = danger
}

func (a *app) runServerAction(sess *session, title, command string) {
	a.runServerActionWithTimeout(sess, title, command, 40*time.Second)
}

func (a *app) runServerActionWithTimeout(sess *session, title, command string, timeout time.Duration) {
	if sess == nil || sess.closed || !a.hasSession(sess) {
		return
	}
	if sess.management == nil {
		sess.management = newManagementUI()
	}
	state := sess.management
	if state.busy {
		return
	}
	owner, err := a.unlockedOwner()
	if err != nil {
		state.message = err.Error()
		return
	}
	state.busy = true
	state.message = tr(nil, "正在") + title + "…"
	cli, gen, host := a.client, a.generation, sess.host
	ctx, cancel := context.WithTimeout(a.operationContext(), timeout)
	if sess.term != nil {
		go func() {
			select {
			case <-sess.term.Done():
				cancel()
			case <-ctx.Done():
			}
		}()
	}
	// The exact mutation and following inventory read share one reviewed request.
	const marker = "@@MANAGEMENT_OK"
	full := "LC_ALL=C; export LC_ALL\n" + command + "\nmanagement_result=$?\n[ \"$management_result\" -eq 0 ] || exit \"$management_result\"\nprintf '\\n" + marker + "\\n'\n" + monitor.InventoryCommand
	go func() {
		defer cancel()
		var output, stderr managementOutput
		output.limit, stderr.limit = 2<<20, 16<<10
		stream, e := cli.StartWithOwner(ctx, approval.Operation{Host: host, Kind: "exec", Command: full, Endpoint: sess.target.Endpoint, HostFingerprint: sess.target.HostFingerprint}, owner)
		code := 0
		if e == nil {
			code, _, e = stream.Copy(&output, &stderr)
			stream.Close()
		}
		if e == nil && code != 0 {
			e = fmt.Errorf(tr(nil, "%s失败：%s"), title, strings.TrimSpace(stderr.String()))
			if stderr.Len() == 0 {
				e = fmt.Errorf(tr(nil, "%s未完成（退出码 %d）"), title, code)
			}
		}
		completed := strings.Contains(output.String(), "\n"+marker+"\n")
		info := monitor.ParseInventory(output.String())
		if output.truncated && info.Docker != nil {
			info.Docker.Note = tr(nil, "Docker 清单超出显示上限，已截断")
			if info.Docker.Status == "checking" {
				info.Docker.Status = "unavailable"
			}
		}
		a.postUI(func() {
			if gen != a.generation || !a.hasSession(sess) || sess.closed {
				return
			}
			state.busy = false
			if completed {
				state.message = title + tr(nil, "已执行")
				if info.Manager != "" && sess.stats != nil {
					a.updateServerInventory(sess, &info)
				}
				if e != nil {
					state.message += tr(nil, "，状态将在后续采样刷新")
				}
			} else if e != nil {
				if errors.Is(e, context.DeadlineExceeded) || ctx.Err() == context.DeadlineExceeded {
					state.message = tr(nil, "操作等待超时，服务器可能仍在处理；请检查状态")
				} else {
					state.message = e.Error()
				}
			} else {
				state.message = tr(nil, "未收到操作完成确认，请检查服务器状态")
			}
		})
	}()
}

// Bound presentation data while continuing to drain the SSH result. Truncated
// diagnostics must not be mistaken for an editor/download error.
type managementOutput struct {
	bytes.Buffer
	limit     int
	truncated bool
}

func (b *managementOutput) Write(p []byte) (int, error) {
	n := min(len(p), max(0, b.limit-b.Len()))
	b.Buffer.Write(p[:n])
	b.truncated = b.truncated || n < len(p)
	return len(p), nil
}

func (a *app) managementFeedback(c *ui.Context) {
	state := a.managementState()
	if state.message != "" {
		ui.Row(c).FillWidth().Shrink(0).Gap(5).AlignItems(ui.Center).Children(func() {
			if state.busy {
				ui.Spinner(c).Size(12, 12)
			}
			ui.Text(c, reviewText(state.message)).SingleLine().FontSize(11).Grow(1).Tooltip(state.message)
			if !state.busy && components.IconButton(c, "×", tr(c, "关闭操作结果")).Size(20, 20).Clicked() {
				state.message = ""
			}
		})
	}
}
