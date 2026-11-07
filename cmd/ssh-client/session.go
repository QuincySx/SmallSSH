package main

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"time"

	"sshclient/internal/approval"
	"sshclient/internal/broker"
	"sshclient/internal/client"
	"sshclient/internal/fileattrs"
	"sshclient/internal/sshconn"
)

func sessionCommand(ctx context.Context, c client.Client, label string, args []string) (int, error) {
	if len(args) == 0 {
		return 2, client.ErrSessionNotExportable
	}
	switch args[0] {
	case "serve":
		return serveSession(ctx, c, label, args[1:], os.Stdin, os.Stdout, os.Stderr)
	case "exec":
		return execSession(ctx, c, label, args[1:], os.Stdin, os.Stdout, os.Stderr)
	default:
		return 2, client.ErrSessionNotExportable
	}
}

type cliSessionOptions struct {
	host, permission, readRoot, writeRoot string
	lifetime, idle                        time.Duration
}

type sessionInput struct {
	ID        string             `json:"id"`
	Action    string             `json:"action,omitempty"`
	Operation approval.Operation `json:"operation,omitempty"`
}

type sessionOutput struct {
	ID       string            `json:"id"`
	Request  *approval.Request `json:"request,omitempty"`
	ExitCode int               `json:"exitCode"`
	Stdout   []byte            `json:"stdout,omitempty"`
	Stderr   []byte            `json:"stderr,omitempty"`
	Files    []sshconn.File    `json:"files,omitempty"`
	FileInfo *fileattrs.Info   `json:"fileInfo,omitempty"`
	Error    string            `json:"error,omitempty"`
}

// session serve owns one stdio channel for its entire lifetime. It never
// publishes a socket or a reusable session credential for other processes.
func parseCLISessionOptions(args []string, notices io.Writer) (cliSessionOptions, error) {
	options := cliSessionOptions{permission: "read", lifetime: 2 * time.Hour, idle: 15 * time.Minute}
	fs := flag.NewFlagSet("session serve", flag.ContinueOnError)
	fs.SetOutput(notices)
	readRoot := fs.String("read-root", "", "限定文件读取的绝对根目录")
	writeRoot := fs.String("write-root", "", "限定文件写入的绝对根目录")
	if err := fs.Parse(args); err != nil {
		return options, err
	}
	args = fs.Args()
	if len(args) < 1 || len(args) > 4 {
		return options, errors.New("用法: smallssh session serve [--read-root /srv] [--write-root /srv] 连接ID [read|full] [2h] [15m]")
	}
	options.host, options.readRoot, options.writeRoot = args[0], *readRoot, *writeRoot
	if len(args) > 1 {
		options.permission = args[1]
	}
	var err error
	if len(args) > 2 {
		options.lifetime, err = time.ParseDuration(args[2])
		if err != nil {
			return options, err
		}
	}
	if len(args) > 3 {
		options.idle, err = time.ParseDuration(args[3])
		if err != nil {
			return options, err
		}
	}
	return options, nil
}

func approvedCLISession(ctx context.Context, c client.Client, label string, options cliSessionOptions, notices io.Writer) (client.Client, *client.Delegation, error) {
	r, d, err := c.RequestScopedSession(ctx, options.host, label, options.permission, options.lifetime, min(options.idle, options.lifetime), options.readRoot, options.writeRoot)
	if err != nil {
		return c, nil, err
	}
	fmt.Fprintf(notices, "%s\n请在所有者界面核对配对码 %s，再批准会话。退出此进程即撤销授权。\n", client.Describe(r), r.Operation.Grant.PairingCode())
	if err = c.Wait(ctx, r.ID, d.Ticket); err != nil {
		d.Close()
		return c, nil, err
	}
	c.Delegation = d
	c.OnSessionRenewal = func(r approval.Request) {
		fmt.Fprintln(notices, "会话已到期，正在等待确认续期；配对码："+r.Operation.Grant.PairingCode())
	}
	return c, d, nil
}

func serveSession(ctx context.Context, c client.Client, label string, args []string, input io.Reader, output, notices io.Writer) (int, error) {
	options, err := parseCLISessionOptions(args, notices)
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
	scanner := bufio.NewScanner(input)
	scanner.Buffer(make([]byte, 4096), 64<<10)
	encoder := json.NewEncoder(output)
	for scanner.Scan() {
		if ctx.Err() != nil {
			return 1, ctx.Err()
		}
		var q sessionInput
		result := sessionOutput{}
		if err := json.Unmarshal(scanner.Bytes(), &q); err != nil {
			result.Error = "无效 JSON 操作"
		} else {
			result.ID = q.ID
			switch q.Action {
			case "close":
				return 0, encoder.Encode(sessionOutput{ID: q.ID})
			case "status":
				message, e := c.CallWithSession(ctx, broker.Message{Action: "sessionStatus", ID: d.ParentID, Ticket: d.Ticket}, d)
				result.Request = message.Request
				if e != nil {
					result.Error = e.Error()
				}
			case "", "run":
				result = runSessionOperation(ctx, c, label, options.host, q)
			default:
				result.Error = "不支持的会话操作"
			}
		}
		if err := encoder.Encode(result); err != nil {
			return 1, err
		}
	}
	if err := scanner.Err(); err != nil {
		return 1, err
	}
	return 0, nil
}

func runSessionOperation(ctx context.Context, c client.Client, label, host string, q sessionInput) sessionOutput {
	result := sessionOutput{ID: q.ID}
	op := q.Operation
	if op.Host == "" {
		op.Host = host
	}
	op.Client = label
	switch op.Kind {
	case "exec", "list", "get", "file-info":
	default:
		result.Error = "stdio 会话支持 exec、list、get 和 file-info；流式写入或终端请使用同进程 SDK"
		return result
	}
	r, ticket, err := c.Request(ctx, op, time.Minute)
	if err != nil {
		result.Error = err.Error()
		return result
	}
	stream, err := c.Start(ctx, r.ID, ticket, 0, 0)
	if err != nil {
		result.Error = err.Error()
		return result
	}
	defer stream.Close()
	if op.Kind == "file-info" {
		info, e := stream.ReadFileInfo()
		result.FileInfo, err = &info, e
	} else {
		var out, stderr boundedSessionOutput
		result.ExitCode, result.Files, err = stream.Copy(&out, &stderr)
		result.Stdout, result.Stderr = out.Bytes(), stderr.Bytes()
	}
	if err != nil {
		result.Error = err.Error()
	}
	return result
}

type boundedSessionOutput struct{ bytes.Buffer }

func (b *boundedSessionOutput) Write(p []byte) (int, error) {
	if len(p) > (8<<20)-b.Len() {
		return 0, errors.New("stdio 单次输出超过 8 MiB，请使用 SDK 流式读取")
	}
	return b.Buffer.Write(p)
}
