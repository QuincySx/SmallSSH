package broker

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"sshclient/internal/approval"
	"sshclient/internal/localipc"
	"sshclient/internal/serverops"
	"sshclient/internal/sshconn"
)

func readComposePlan(ctx context.Context, c *sshconn.Client, target serverops.ComposeRequest) (serverops.ComposePlan, error) {
	command, err := target.Command("plan")
	if err != nil {
		return serverops.ComposePlan{}, err
	}
	r, err := c.Exec(ctx, command)
	if err != nil {
		return serverops.ComposePlan{}, err
	}
	if r.ExitCode != 0 {
		message := strings.TrimSpace(r.Stderr)
		if len(message) > 4096 {
			message = message[:4096] + "…"
		}
		return serverops.ComposePlan{}, fmt.Errorf("无法读取 Compose 计划：%s", message)
	}
	return serverops.ParseComposePlan(target, r.Stdout)
}

func (s *Server) runDockerWork(ctx context.Context, c *sshconn.Client, w *localipc.Wire, op approval.Operation, finish func(Message) error) error {
	if op.Kind == "docker-logs" {
		command, err := op.Docker.Command(op.Kind)
		if err != nil {
			return err
		}
		code, err := c.ExecStream(ctx, command, dataWriter{w, "stdout"}, dataWriter{w, "stderr"})
		if err != nil {
			return err
		}
		return finish(Message{Action: "done", ExitCode: code})
	}
	if op.Kind == "compose-update" {
		s.writes.Lock()
		defer s.writes.Unlock()
	}
	plan, err := readComposePlan(ctx, c, op.Compose)
	if err != nil {
		return err
	}
	if op.Kind == "compose-plan" {
		if err = w.Send(Message{Action: "compose-plan", ComposePlan: &plan}); err != nil {
			return err
		}
		return finish(Message{Action: "done"})
	}
	if plan.Hash != op.Compose.ConfigHash {
		return errors.New("Compose 配置已变化，请重新检查更新；尚未重建容器")
	}
	stage := func(name string) error {
		if err := ctx.Err(); err != nil {
			return err
		}
		if err := w.Send(Message{Action: "compose-phase", Phase: name}); err != nil {
			return err
		}
		command, err := op.Compose.Command(name)
		if err != nil {
			return err
		}
		code, err := c.ExecStream(ctx, command, dataWriter{w, "stdout"}, dataWriter{w, "stderr"})
		if err != nil {
			return fmt.Errorf("%s 阶段连接中断，请检查远端实际状态：%w", name, err)
		}
		if code != 0 {
			if name == "pull" {
				return fmt.Errorf("镜像下载未完成（退出码 %d）；尚未重建容器", code)
			}
			return fmt.Errorf("重建或就绪检查未通过（退出码 %d）；请查看服务状态和日志", code)
		}
		return nil
	}
	if err = stage("pull"); err != nil {
		return err
	}
	// Re-read resolved service hashes after a possibly slow download. File/env
	// changes must require a new review, rather than using an old approval.
	current, err := readComposePlan(ctx, c, op.Compose)
	if err != nil {
		return err
	}
	if current.Hash != plan.Hash {
		return errors.New("下载期间 Compose 配置发生变化；镜像已下载，尚未重建，请重新检查更新")
	}
	if err = stage("up"); err != nil {
		return err
	}
	if err = w.Send(Message{Action: "compose-phase", Phase: "verify"}); err != nil {
		return err
	}
	command, err := op.Compose.Command("status")
	if err != nil {
		return err
	}
	result, err := c.Exec(ctx, command)
	if err != nil {
		return err
	}
	if result.ExitCode != 0 {
		return errors.New("更新命令已执行，但无法核对服务状态；请检查远端结果")
	}
	rows, err := serverops.ParseComposeStatus(result.Stdout, op.Compose.ServiceNames())
	if sendErr := w.Send(Message{Action: "compose-result", ComposeStatus: rows}); sendErr != nil {
		return sendErr
	}
	if err != nil {
		return fmt.Errorf("已重建，状态核对未通过：%w", err)
	}
	return finish(Message{Action: "done"})
}
