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
	"sshclient/internal/client"
	"sshclient/internal/fileattrs"
	"sshclient/internal/protocol"
	"sshclient/internal/sshdata"
	"sshclient/internal/transfer"
)

func sessionCommand(ctx context.Context, c client.Client, label string, args []string) (int, error) {
	if len(args) == 0 {
		return 2, client.ErrSessionNotExportable
	}
	switch args[0] {
	case "serve":
		return serveSession(ctx, c, label, args[1:], os.Stdin, os.Stdout, os.Stderr)
	case "maintenance":
		return serveWithPermission(ctx, c, label, args[1:], os.Stdin, os.Stdout, os.Stderr, approval.MaintenancePermission)
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
	ID         string             `json:"id"`
	Action     string             `json:"action,omitempty"`
	Operation  approval.Operation `json:"operation,omitempty"`
	Remote     string             `json:"remote,omitempty"`
	Local      string             `json:"local,omitempty"`
	Directory  bool               `json:"directory,omitempty"`
	TransferID string             `json:"transferId,omitempty"`
}

type sessionOutput struct {
	ID         string            `json:"id"`
	Request    *approval.Request `json:"request,omitempty"`
	ExitCode   int               `json:"exitCode"`
	Stdout     []byte            `json:"stdout,omitempty"`
	Stderr     []byte            `json:"stderr,omitempty"`
	Files      []sshdata.File    `json:"files,omitempty"`
	FileInfo   *fileattrs.Info   `json:"fileInfo,omitempty"`
	TransferID string            `json:"transferId,omitempty"`
	Error      string            `json:"error,omitempty"`
}

// session serve owns one stdio channel for its entire lifetime. It never
// publishes a socket or a reusable session credential for other processes.
func parseCLISessionOptions(args []string, notices io.Writer) (cliSessionOptions, error) {
	return parseSessionOptions(args, notices, "")
}

func parseSessionOptions(args []string, notices io.Writer, fixedPermission string) (cliSessionOptions, error) {
	options := cliSessionOptions{permission: "guarded", lifetime: 2 * time.Hour, idle: 15 * time.Minute}
	name := "session serve"
	if fixedPermission == approval.MaintenancePermission {
		name = "session maintenance"
		options = cliSessionOptions{permission: fixedPermission, lifetime: time.Hour, idle: time.Hour}
	}
	fs := flag.NewFlagSet(name, flag.ContinueOnError)
	fs.SetOutput(notices)
	fs.StringVar(&options.readRoot, "read-root", "", "限定文件读取的绝对路径")
	if fixedPermission == "" {
		fs.StringVar(&options.writeRoot, "write-root", "", "限定文件写入的绝对根目录")
	}
	if err := fs.Parse(args); err != nil {
		return options, err
	}
	args = fs.Args()
	if fixedPermission != "" && len(args) != 1 {
		return options, errors.New("用法: smallssh session maintenance [--read-root /srv/backups] 连接ID")
	}
	if len(args) < 1 || len(args) > 4 {
		return options, errors.New("用法: smallssh session serve [--read-root /srv] [--write-root /srv] 连接ID [read|guarded|maintenance|full] [2h] [15m]")
	}
	options.host = args[0]
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
	return serveWithPermission(ctx, c, label, args, input, output, notices, "")
}

func serveWithPermission(ctx context.Context, c client.Client, label string, args []string, input io.Reader, output, notices io.Writer, fixedPermission string) (int, error) {
	options, err := parseSessionOptions(args, notices, fixedPermission)
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
				message, e := c.CallWithSession(ctx, protocol.Message{Action: "sessionStatus", ID: d.ParentID, Ticket: d.Ticket}, d)
				result.Request = message.Request
				if e != nil {
					result.Error = e.Error()
				}
			case "backup", "resume-backup":
				result = runSessionBackup(ctx, c, label, options.host, q)
			case "preview-upgrade":
				if q.Operation != (approval.Operation{}) || q.Remote != "" || q.Local != "" || q.Directory || q.TransferID != "" {
					result.Error = "升级预览不接受命令、参数或传输设置"
				} else {
					q.Operation.Kind = "upgrade-preview"
					result = runSessionOperation(ctx, c, label, options.host, q)
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

func runSessionBackup(ctx context.Context, c client.Client, label, host string, q sessionInput) sessionOutput {
	result := sessionOutput{ID: q.ID}
	if c.Delegation == nil || c.Delegation.Permission != approval.MaintenancePermission || c.Delegation.Validate(c.Dir) != nil || q.Operation != (approval.Operation{}) {
		result.Error = "备份操作需要维护准备授权"
		return result
	}
	manager := transfer.NewWithClient(c)
	id := q.TransferID
	if q.Action == "backup" {
		if id != "" || q.Local == "" || !approval.WithinRoot(c.Delegation.ReadRoot, q.Remote) {
			result.Error = "请指定授权范围内的备份来源和本地目标"
			return result
		}
		job, err := manager.AddDownload(host, q.Local, q.Remote, c.Delegation.Endpoint, c.Delegation.HostFingerprint, q.Directory)
		if err != nil {
			result.Error = err.Error()
			return result
		}
		id = job.ID
	} else if id == "" || q.Remote != "" || q.Local != "" || q.Directory {
		result.Error = "续传只能指定原备份任务编号"
		return result
	}
	result.TransferID = id
	if err := manager.Run(ctx, id, label); err != nil {
		result.Error = err.Error()
	}
	return result
}

func runSessionOperation(ctx context.Context, c client.Client, label, host string, q sessionInput) sessionOutput {
	result := sessionOutput{ID: q.ID}
	op := q.Operation
	if op.Host == "" {
		op.Host = host
	}
	op.Client = label
	switch op.Kind {
	case "exec", "list", "get", "file-info", "upgrade-preview":
	default:
		result.Error = "stdio 会话支持 exec、list、get、file-info 和 upgrade-preview"
		return result
	}
	r, ticket, err := c.Request(ctx, op, time.Minute)
	if err != nil {
		result.Error = err.Error()
		return result
	}
	if r.Status == "pending" {
		if err = c.Wait(ctx, r.ID, ticket); err != nil {
			result.Error = err.Error()
			return result
		}
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
