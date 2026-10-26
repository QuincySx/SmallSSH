// dev runs the pinned MyGo watcher with silent startup on every rebuild.
package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"sshclient/internal/desktopcontrol"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run() error {
	dir := flag.String("data-dir", desktopcontrol.DefaultDir(), "独立开发数据目录")
	flag.Parse()
	abs, err := filepath.Abs(*dir)
	if err != nil {
		return err
	}
	*dir = abs
	action := "run"
	if flag.NArg() > 0 {
		action = flag.Arg(0)
	}
	if flag.NArg() > 1 {
		return errors.New("用法：go run ./tools/dev [-data-dir 目录] [run|status|show|stop]")
	}
	if action == "show" || action == "status" || action == "stop" {
		command := action
		if command == "stop" {
			command = "quit"
		}
		state, err := desktopcontrol.Command(context.Background(), *dir, command)
		if err != nil {
			return fmt.Errorf("无法联系开发进程：%w", err)
		}
		return json.NewEncoder(os.Stdout).Encode(state)
	}
	if action != "run" {
		return errors.New("支持 run、status、show、stop；只有 show 会显示窗口")
	}
	if _, err := desktopcontrol.Command(context.Background(), *dir, "status"); err == nil {
		return errors.New("开发进程已运行；状态检查不会显示窗口")
	}
	cmd := exec.Command("go", "run", "github.com/egoist/mygo/cmd/mygo@v0.5.5", "dev")
	cmd.Env = developmentEnv(os.Environ(), *dir)
	cmd.Stdin, cmd.Stdout, cmd.Stderr = os.Stdin, os.Stdout, os.Stderr
	return cmd.Run()
}

func developmentEnv(base []string, dir string) []string {
	out := make([]string, 0, len(base)+2)
	for _, e := range base {
		if !strings.HasPrefix(e, "SSH_WORKBENCH_DEV_BACKGROUND=") && !strings.HasPrefix(e, "SSH_WORKBENCH_DEV_DATA_DIR=") {
			out = append(out, e)
		}
	}
	return append(out, "SSH_WORKBENCH_DEV_BACKGROUND=1", "SSH_WORKBENCH_DEV_DATA_DIR="+dir)
}
