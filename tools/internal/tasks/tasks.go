// Package tasks holds the repository's development tasks: the commands a
// developer, a reviewer or an agent runs instead of remembering a toolchain.
//
// Go has no npm scripts, and `go generate` is for code generation only, so the
// convention here is one Go command per task: every task is both
// `go run ./tools <task>` and `go run ./tools/<task>`, and `go run ./tools`
// lists them. CI must call these same entry points, never a second script.
package tasks

import (
	"errors"
	"flag"
	"fmt"
	"os"
)

// A task is one thing to run from the repository root. run receives the
// arguments after the task name.
type task struct {
	name    string
	summary string
	run     func(args []string) error
}

// tasks is the whole list, in the order `go run ./tools` shows it.
var tasks = []task{
	{"build", "编译原生组件与各平台 CLI 并打包桌面应用（-platform、-check）", runBuild},
	{"check", "格式检查、go vet 与测试一次跑完（CI 与本机同一批命令）", runCheck},
	{"dev", "静默开发进程：run / status / show / stop（-data-dir）", runDev},
	{"test", "只跑 go vet 与测试（-race、-short、-pkg、-timeout）", runTest},
}

// Main runs the task this command owns. A tool with no fixed name is the front
// door: it dispatches on its first argument and lists the tasks without one.
func Main(name string) {
	if err := dispatch(name, os.Args[1:]); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return
		}
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func dispatch(name string, args []string) error {
	if name == "" {
		switch {
		case len(args) == 0, args[0] == "help", args[0] == "-h", args[0] == "--help":
			list()
			return nil
		}
		name, args = args[0], args[1:]
	}
	for _, t := range tasks {
		if t.name == name {
			return t.run(args)
		}
	}
	return fmt.Errorf("未知任务 %q；运行 go run ./tools 查看可用任务", name)
}

func list() {
	fmt.Println("用法：go run ./tools <任务> [参数]")
	fmt.Println()
	for _, t := range tasks {
		fmt.Printf("  %-6s %s\n", t.name, t.summary)
	}
	fmt.Println()
	fmt.Println("每个任务同时也是独立命令，例如 go run ./tools/build -platform darwin/arm64")
	fmt.Println("日志与打包产物都在 build/；该目录与 resources/<平台>/ 均被 git 忽略。")
}

// parse parses one task's flags. A task never reads the global flag set, so
// dispatch cannot leak arguments between tasks.
func parse(fs *flag.FlagSet, args []string) error {
	fs.SetOutput(os.Stderr)
	return fs.Parse(args)
}
