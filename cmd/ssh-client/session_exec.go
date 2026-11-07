package main

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"strings"
	"time"

	"sshclient/internal/approval"
	"sshclient/internal/client"
)

// execSession keeps the approving process and control connection alive for
// several command lines. It never exports authority to a later CLI process.
func execSession(ctx context.Context, c client.Client, label string, args []string, input io.Reader, output, notices io.Writer) (int, error) {
	if len(args) < 1 || len(args) > 3 || strings.HasPrefix(args[0], "-") {
		return 2, errors.New("用法: smallssh session exec 连接ID [2h] [15m]；或 smallssh exec 连接ID")
	}
	terms := append([]string{args[0], "full"}, args[1:]...)
	options, err := parseCLISessionOptions(terms, notices)
	if err != nil {
		return 2, err
	}
	c, d, err := approvedCLISession(ctx, c, label, options, notices)
	if err != nil {
		return 1, err
	}
	defer d.Close()
	if closer, ok := input.(io.Closer); ok {
		stop := context.AfterFunc(ctx, func() { closer.Close() })
		defer stop()
	}
	fmt.Fprintln(notices, "会话已批准。每行执行一条独立远程命令；:quit 或输入结束退出。多条命令共享本次授权，不共享 cd / 环境变量。")
	scanner := bufio.NewScanner(input)
	scanner.Buffer(make([]byte, 4096), 32768+2)
	lastCode := 0
	for scanner.Scan() {
		if err := ctx.Err(); err != nil {
			return 1, err
		}
		command := scanner.Text()
		if strings.TrimSpace(command) == ":quit" {
			return lastCode, nil
		}
		if strings.TrimSpace(command) == "" {
			continue
		}
		if len(command) > 32768 {
			return 1, errors.New("单条命令超过 32 KiB")
		}
		op := approval.Operation{Host: options.host, Client: label, Kind: "exec", Command: command}
		r, ticket, err := c.Request(ctx, op, 5*time.Minute)
		if err != nil {
			// Refused commands do not consume or widen the session. A revoked,
			// disconnected or failed session ends here; never fall back to a
			// one-shot request, which would ask for consent for every line.
			if errors.Is(err, client.ErrAutomationPolicy) {
				fmt.Fprintln(notices, "命令未执行:", err)
				lastCode = 1
				continue
			}
			return 1, err
		}
		stream, err := c.Start(ctx, r.ID, ticket, 0, 0)
		if err != nil {
			return 1, err
		}
		code, _, err := stream.Copy(output, notices)
		stream.Close()
		if err != nil {
			return 1, err
		}
		lastCode = code
		fmt.Fprintf(notices, "\n[退出码 %d]\n", code)
	}
	if err := ctx.Err(); err != nil {
		return 1, err
	}
	if err := scanner.Err(); err != nil {
		if errors.Is(err, bufio.ErrTooLong) {
			return 1, errors.New("单条命令超过 32 KiB")
		}
		return 1, err
	}
	return lastCode, nil
}
