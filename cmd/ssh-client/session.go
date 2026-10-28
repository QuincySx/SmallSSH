package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"os"
	"time"

	"sshclient/internal/broker"
	"sshclient/internal/client"
)

func sessionCommand(ctx context.Context, c client.Client, label string, args []string) (int, error) {
	if len(args) == 2 && (args[0] == "status" || args[0] == "revoke") {
		d, err := client.ReadDelegation(args[1], c.Dir)
		if err != nil {
			return 1, err
		}
		defer d.Close()
		if args[0] == "revoke" {
			_, err = c.Call(ctx, broker.Message{Action: "withdraw", ID: d.ParentID, Ticket: d.Ticket})
			if err == nil {
				fmt.Println("会话及其子授权已撤销；会话文件已失效。")
			}
			return 0, err
		}
		r, err := c.Call(ctx, broker.Message{Action: "sessionStatus", ID: d.ParentID, Ticket: d.Ticket})
		if err != nil {
			return 1, err
		}
		if r.Request == nil {
			return 1, errors.New("会话不存在或已失效")
		}
		fmt.Println(client.Describe(*r.Request))
		fmt.Println("状态:", r.Request.Status)
		return 0, nil
	}
	fs := flag.NewFlagSet("session request", flag.ContinueOnError)
	readRoot := fs.String("read-root", "", "限定文件读取的绝对根目录")
	writeRoot := fs.String("write-root", "", "限定文件写入的绝对根目录")
	if len(args) > 0 && args[0] == "request" {
		if err := fs.Parse(args[1:]); err != nil {
			return 2, err
		}
		args = append([]string{"request"}, fs.Args()...)
	}
	if len(args) < 3 || len(args) > 6 || args[0] != "request" {
		return 2, errors.New("用法: smallssh session request 连接ID 新会话文件 [read|full] [2h] [15m]；smallssh session status/revoke 会话文件")
	}
	path, pathErr := client.DelegationPath(c.Dir, args[2])
	if pathErr != nil {
		return 1, pathErr
	}
	if _, err := os.Lstat(path); !os.IsNotExist(err) {
		return 1, errors.New("会话文件已存在或不可访问，请使用新的文件名")
	}
	permission, lifetime, idle := "read", 2*time.Hour, 15*time.Minute
	if len(args) > 3 {
		permission = args[3]
	}
	var err error
	if len(args) > 4 {
		lifetime, err = time.ParseDuration(args[4])
		if err != nil {
			return 2, err
		}
	}
	if len(args) > 5 {
		idle, err = time.ParseDuration(args[5])
		if err != nil {
			return 2, err
		}
	}
	idle = min(idle, lifetime)
	r, d, err := c.RequestScopedSession(ctx, args[1], label, permission, lifetime, idle, *readRoot, *writeRoot)
	if err != nil {
		return 1, err
	}
	defer d.Close()
	stored := false
	defer func() {
		if !stored {
			cleanup, cancel := context.WithTimeout(context.Background(), 2*time.Second)
			defer cancel()
			_, _ = c.Call(cleanup, broker.Message{Action: "withdraw", ID: r.ID, Ticket: d.Ticket})
		}
	}()
	fmt.Fprintf(os.Stderr, "%s\n请在所有者界面核对配对码 %s，再批准会话。\n", client.Describe(r), r.Operation.Grant.PairingCode())
	if err = c.Wait(ctx, r.ID, d.Ticket); err != nil {
		return 1, err
	}
	if err = d.Save(path); err != nil {
		return 1, err
	}
	stored = true
	fmt.Fprintf(os.Stderr, "会话已保存到 %q。此文件代表指定服务器的委托权限，请仅交给你授权的客户端。\n", path)
	return 0, nil
}
