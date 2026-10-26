package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"slices"
	"strings"
	"time"

	"golang.org/x/term"
	"sshclient/internal/approval"
	"sshclient/internal/broker"
	"sshclient/internal/client"
	"sshclient/internal/serverops"
)

func dockerOperation(label string, args []string) (approval.Operation, error) {
	var op approval.Operation
	op.Client = label
	if len(args) == 0 {
		return op, errors.New("用法: smallssh docker logs|terminal|plan|update [选项] 连接ID 目标")
	}
	fs := flag.NewFlagSet("smallssh docker "+args[0], flag.ContinueOnError)
	socket := fs.String("socket", "/var/run/docker.sock", "Docker 本地 socket")
	engine := fs.String("engine", "", "Docker 实例 ID（来自监控清单）")
	id := fs.String("container", "", "Compose 项目中的完整容器 ID")
	directory := fs.String("directory", "", "Compose 工作目录")
	files := fs.String("files", "", "Compose 配置文件，按原顺序用逗号分隔")
	services := fs.String("services", "", "要更新的服务，用逗号分隔")
	hash := fs.String("hash", "", "预览计划的 hash")
	shell := fs.String("shell", "sh", "容器 Shell：sh/bash")
	tail := fs.Int("tail", 200, "日志末尾行数，1..2000")
	follow := fs.Bool("follow", true, "持续接收日志")
	if err := fs.Parse(args[1:]); err != nil {
		return op, err
	}
	targets := fs.Args()
	if len(targets) != 2 {
		return op, errors.New("选项需放在连接ID之前；用法: smallssh docker logs/terminal [选项] 连接ID 完整容器ID；smallssh docker plan/update [选项] 连接ID 项目名")
	}
	op.Host = targets[0]
	switch args[0] {
	case "logs", "terminal":
		op.Kind = "docker-" + args[0]
		op.Docker = serverops.DockerRequest{Target: serverops.DockerTarget{Socket: *socket, Engine: *engine, ID: targets[1]}}
		if args[0] == "logs" {
			op.Docker.Tail, op.Docker.Follow = *tail, *follow
		} else {
			op.Docker.Shell = *shell
		}
		return op, op.Docker.Validate(op.Kind)
	case "plan", "update":
		op.Kind = "compose-" + args[0]
		names := strings.Split(*services, ",")
		slices.Sort(names)
		op.Compose = serverops.ComposeRequest{Target: serverops.DockerTarget{Socket: *socket, Engine: *engine, ID: *id}, Project: targets[1], Directory: *directory, Files: *files, Services: strings.Join(names, ","), ConfigHash: *hash}
		return op, op.Compose.Validate(args[0] == "update")
	}
	return op, errors.New("不支持的 Docker 子命令")
}

func dockerCommand(ctx context.Context, c client.Client, label string, args []string) (int, error) {
	op, err := dockerOperation(label, args)
	if err != nil {
		return 2, err
	}
	if op.Kind == "docker-terminal" && !term.IsTerminal(int(os.Stdin.Fd())) {
		return 1, errors.New("容器终端需要交互式本地终端")
	}
	r, ticket, err := c.Request(ctx, op, 15*time.Minute)
	if err != nil {
		return 1, err
	}
	defer func() {
		cleanup, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		c.Call(cleanup, broker.Message{Action: "withdraw", ID: r.ID, Ticket: ticket})
	}()
	writeApprovalNotice(os.Stderr, r)
	if err = c.Wait(ctx, r.ID); err != nil {
		return 1, err
	}
	stream, err := c.Start(ctx, r.ID, ticket, 100, 30)
	if err != nil {
		return 1, err
	}
	defer stream.Close()
	if op.Kind == "docker-terminal" {
		old, e := term.MakeRaw(int(os.Stdin.Fd()))
		if e != nil {
			return 1, e
		}
		defer term.Restore(int(os.Stdin.Fd()), old)
		stop := watchResize(ctx, stream)
		defer stop()
		go func() { io.Copy(stream, os.Stdin); stream.Close() }()
		code, _, e := stream.Copy(os.Stdout, os.Stderr)
		return code, e
	}
	for {
		m, err := stream.Receive()
		if err != nil {
			return 1, err
		}
		switch m.Action {
		case "stdout":
			os.Stdout.Write(m.Data)
		case "stderr":
			os.Stderr.Write(m.Data)
		case "compose-plan":
			if err = json.NewEncoder(os.Stdout).Encode(m.ComposePlan); err != nil {
				return 1, err
			}
		case "compose-phase":
			fmt.Fprintln(os.Stderr, "阶段:", m.Phase)
		case "compose-result":
			if err = json.NewEncoder(os.Stdout).Encode(m.ComposeStatus); err != nil {
				return 1, err
			}
		case "done":
			return m.ExitCode, nil
		}
	}
}
