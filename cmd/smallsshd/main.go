package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"sshclient/internal/hardware"
	"sshclient/internal/localipc"
	"sshclient/internal/service"
)

func main() {
	if hardware.MaybeWorker() {
		return
	}
	if err := run(); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return
		}
		fmt.Fprintln(os.Stderr, "smallsshd: 错误:", err)
		os.Exit(1)
	}
}

func run() error {
	flags := flag.NewFlagSet("smallsshd", flag.ContinueOnError)
	flags.SetOutput(os.Stdout)
	dir := flags.String("data-dir", localipc.DefaultDir(), "私有数据目录")
	flags.Usage = func() {
		fmt.Fprintln(flags.Output(), "用法: smallsshd [--data-dir 私有目录]")
		flags.PrintDefaults()
	}
	if err := flags.Parse(os.Args[1:]); err != nil {
		return err
	}
	if flags.NArg() != 0 {
		return errors.New("不接受位置参数")
	}
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()
	return service.Run(ctx, *dir)
}
