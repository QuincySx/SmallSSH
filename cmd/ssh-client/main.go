package main

import (
	"bufio"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net"
	"os"
	"os/signal"
	"strings"
	"time"

	"golang.org/x/crypto/ssh"
	"golang.org/x/term"
	"sshclient/internal/approval"
	"sshclient/internal/broker"
	"sshclient/internal/client"
	"sshclient/internal/localipc"
	"sshclient/internal/systemauth"
	"sshclient/internal/transfer"
	"sshclient/internal/vault"
)

const help = `SmallSSH · 开发预览版

用法: smallssh [--data-dir 私有目录] <命令>
 init                          创建加密保险库
 host add                      添加或更新连接（需先关闭代理）
 host group 连接ID 分组名       保留凭据并修改分组（需代理运行）
 probe 主机:端口               查看主机指纹；请通过服务器控制台另行核对
 agent                         启动代理；主密码仅从本地终端读取
 hosts                         所有者验证后列出连接 ID、分组与地址
 biometric status|enable|disable 系统验证设置
 --biometric agent / approve ID  用系统验证解锁或授权
 monitor 连接ID                 连续 Linux 监控（JSON 行）
 docker logs/terminal [选项] 连接ID 完整容器ID
                               容器日志或交互终端，需指定 --engine
 docker plan/update [选项] 连接ID 项目名
                               预览 / 更新 Compose，需绑定配置与计划摘要
 transfer list                  查看持久化传输队列
 transfer resume 任务ID          校验并断点继续（可复用客户端会话）
 pending                       所有者验证后查看待授权和运行中的操作
 session request 连接ID 文件 [read|full] [2h] [15m]
                               申请有期限的客户端会话；核对配对码后批准
 session request --read-root /srv --write-root /srv ...
                               可选限定文件读写目录；选项置于连接 ID 前
 --session 文件 exec/ls/get/... 使用已批准会话签发一次性子授权
 SSH_CLIENT_SESSION=文件       在当前客户端环境中复用同一授权，无需逐条传参
 session status 文件            查看会话范围与期限
 session revoke 文件            撤销自己的会话及全部子授权
 approve ID / deny ID          核对完整操作并用主密码授权或拒绝
 revoke ID                     用主密码撤销授权并关闭连接
 exec 连接ID '完整命令'        申请授权，批准后执行一次
 shell 连接ID                  申请最长15分钟的完整交互会话
 ls 连接ID /绝对路径           浏览远程目录
 get 连接ID /远程路径 本地文件 下载（不覆盖本地文件）
 put 连接ID 本地文件 /远程路径 上传（绑定内容，不覆盖远程文件）
 replace 连接ID 本地文件 /远程路径 原文件SHA256
                               核对原内容、保留旧副本并原子覆盖

AI 先申请一次客户端会话，范围和期限内的操作自动领取子授权。
自然过期后自动申请原范围续期，批准后继续；撤销、拒绝或越权不自动重试。
单次请求留在授权中心，不逐条弹窗。
允许 exec/shell 即允许该 SSH 账户权限范围内的操作。主密码不要写进参数。
`

func main() {
	code, e := run()
	if e != nil {
		fmt.Fprintln(os.Stderr, "smallssh: 错误:", e)
		if code == 0 {
			code = 1
		}
	}
	os.Exit(code)
}
func run() (int, error) {
	fs := flag.NewFlagSet("smallssh", flag.ContinueOnError)
	fs.Usage = func() {
		fmt.Fprint(fs.Output(), help+"\n选项:\n")
		fs.PrintDefaults()
	}
	dir := fs.String("data-dir", localipc.DefaultDir(), "私有数据目录")
	biometric := fs.Bool("biometric", false, "通过本机系统验证解锁 / 授权")
	label := fs.String("client", "CLI", "申请方标签（未经身份验证）")
	sessionFile := fs.String("session", "", "使用已批准的客户端会话文件")
	if e := fs.Parse(os.Args[1:]); e != nil {
		return 2, e
	}
	args := fs.Args()
	if len(args) == 0 || args[0] == "help" {
		fmt.Print(help)
		return 0, nil
	}
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt)
	defer cancel()
	c := client.Client{Dir: *dir}
	sessionFlagSet := false
	fs.Visit(func(f *flag.Flag) {
		if f.Name == "session" {
			sessionFlagSet = true
		}
	})
	selectedSession := sessionForCommand(args, *sessionFile, sessionFlagSet, os.Getenv("SSH_CLIENT_SESSION"))
	if selectedSession != "" {
		d, e := client.ReadDelegation(selectedSession, *dir)
		if e != nil {
			return 1, e
		}
		defer d.Close()
		c.Delegation = d
		c.OnSessionRenewal = func(r approval.Request) {
			fmt.Fprintln(os.Stderr, "会话已到期，正在等待确认续期；批准后继续。配对码："+r.Operation.Grant.PairingCode())
		}
	}
	switch args[0] {
	case "docker":
		return dockerCommand(ctx, c, *label, args[1:])
	case "session":
		return sessionCommand(ctx, client.Client{Dir: *dir}, *label, args[1:])
	case "init":
		release, e := localipc.Acquire(*dir)
		if e != nil {
			return 1, e
		}
		defer release()
		p, e := password("创建主密码（至少12字节）: ")
		if e != nil {
			return 1, e
		}
		defer wipe(p)
		confirm, e := password("再次输入: ")
		if e != nil {
			return 1, e
		}
		defer wipe(confirm)
		if string(p) != string(confirm) {
			return 1, errors.New("两次主密码不一致")
		}
		v, e := vault.New(p)
		if e != nil {
			return 1, e
		}
		defer v.Close()
		if e = vault.Save(broker.VaultPath(*dir), v, true); e != nil {
			return 1, e
		}
		fmt.Println("保险库已创建。接下来运行 host add。")
		return 0, nil
	case "host":
		if len(args) == 4 && args[1] == "group" {
			owner, e := openOwner(ctx, *dir, *biometric, "修改 SSH 连接分组")
			if e != nil {
				return 1, e
			}
			defer owner.Close()
			h, e := c.CallWithOwner(ctx, broker.Message{Action: "hosts"}, owner)
			if e != nil {
				return 1, e
			}
			var d *vault.Summary
			for _, value := range h.Connections {
				if value.ID == args[2] {
					value := value
					d = &value
					break
				}
			}
			if d == nil {
				return 1, errors.New("连接不存在")
			}
			d.Group = args[3]
			return 0, c.PutDetails(ctx, *d, owner)
		}
		if len(args) != 2 || args[1] != "add" {
			return 2, errors.New("用法: smallssh host add")
		}
		return 0, addHost(*dir)
	case "probe":
		if len(args) != 2 {
			return 2, errors.New("用法: smallssh probe 主机:端口")
		}
		return 0, probe(ctx, args[1])
	case "biometric":
		svc := systemauth.New(0)
		if len(args) != 2 {
			return 2, errors.New("用法: smallssh biometric status / enable / disable")
		}
		if args[1] == "status" {
			b, _ := json.MarshalIndent(svc.Status(ctx, *dir), "", "  ")
			fmt.Println(string(b))
			return 0, nil
		}
		if args[1] != "enable" && args[1] != "disable" {
			return 2, errors.New("未知系统验证设置")
		}
		p, e := password("主密码确认系统验证设置: ")
		if e != nil {
			return 1, e
		}
		defer wipe(p)
		if args[1] == "enable" {
			e = svc.Enroll(ctx, *dir, p)
		} else {
			e = svc.Disable(*dir, p)
		}
		return 0, e
	case "transfer":
		m := transfer.NewWithClient(c)
		if len(args) == 2 && args[1] == "list" {
			b, _ := json.MarshalIndent(m.List(), "", "  ")
			fmt.Println(string(b))
			return 0, nil
		}
		if len(args) == 3 && args[1] == "pause" {
			m.Pause(args[2])
			return 0, nil
		}
		if len(args) == 3 && args[1] == "resume" {
			return 0, m.Run(ctx, args[2], *label)
		}
		return 2, errors.New("用法: smallssh transfer list / smallssh transfer pause 任务ID / smallssh transfer resume 任务ID")
	case "get", "put":
		if len(args) != 4 {
			return 2, errors.New("用法: smallssh get 连接ID /远程路径 本地文件；smallssh put 连接ID 本地文件 /远程路径")
		}
		direction, local, remote := "upload", args[2], args[3]
		if args[0] == "get" {
			direction, local, remote = "download", args[3], args[2]
		}
		m := transfer.NewWithClient(c)
		j, e := m.Add(args[1], direction, local, remote)
		if e != nil {
			return 1, e
		}
		fmt.Fprintf(os.Stderr, "传输任务 %s；中断后可用 smallssh transfer resume %s 继续。\n", j.ID, j.ID)
		return 0, m.Run(ctx, j.ID, *label)
	case "agent":
		if *biometric {
			v, e := systemauth.New(0).Open(ctx, *dir, "解锁 SSH 代理")
			if e != nil {
				return 1, e
			}
			s, e := broker.OpenSession(*dir, v)
			if e != nil {
				return 1, e
			}
			defer s.Close()
			fmt.Fprintln(os.Stderr, "系统验证已解锁代理")
			<-ctx.Done()
			return 0, nil
		}
		p, e := password("解锁保险库: ")
		if e != nil {
			return 1, e
		}
		s, e := broker.Open(*dir, p)
		wipe(p)
		if e != nil {
			return 1, e
		}
		defer s.Close()
		fmt.Fprintln(os.Stderr, "代理已启动。保持此窗口打开；退出会锁定并撤销所有会话。")
		<-ctx.Done()
		return 0, nil
	case "hosts", "pending":
		owner, err := openOwner(ctx, *dir, *biometric, "查看 SSH 管理信息")
		if err != nil {
			return 1, err
		}
		defer owner.Close()
		action := args[0]
		r, e := c.CallWithOwner(ctx, broker.Message{Action: action}, owner)
		if e != nil {
			return 1, e
		}
		if action == "hosts" {
			for _, h := range r.Connections {
				fmt.Printf("%q\t%q\t%q\t%q\n", h.ID, h.Group, h.Name, h.Host)
			}
		} else {
			for _, q := range r.Requests {
				fmt.Printf("%s\n状态: %s\n\n", client.Describe(q), q.Status)
			}
		}
		return 0, nil
	case "approve", "deny", "revoke":
		if len(args) != 2 {
			return 2, errors.New("请提供完整授权 ID")
		}
		owner, err := openOwner(ctx, *dir, *biometric, "查看并处理 SSH 授权")
		if err != nil {
			return 1, err
		}
		defer owner.Close()
		r, e := c.CallWithOwner(ctx, broker.Message{Action: "status", ID: args[1]}, owner)
		if e != nil {
			return 1, e
		}
		if r.Request == nil {
			return 1, approval.ErrDenied
		}
		fmt.Println(client.Describe(*r.Request))
		tty, e := openTTY()
		if e != nil {
			return 1, e
		}
		fmt.Fprint(os.Stderr, "核对上面的具体操作后，输入 yes 确认本次决定: ")
		answer, e := bufio.NewReader(tty).ReadString('\n')
		tty.Close()
		if e != nil {
			return 1, e
		}
		if strings.TrimSpace(answer) != "yes" {
			return 1, errors.New("已取消授权决定")
		}
		e = c.DecideReviewedWithOwner(ctx, *r.Request, args[0] == "approve", args[0] == "revoke", owner)
		if e == nil {
			fmt.Println("已处理")
		}
		return 0, e
	}
	op := approval.Operation{Client: *label}
	ttl := 5 * time.Minute
	if len(args) < 2 {
		return 2, errors.New("缺少连接 ID")
	}
	op.Host = args[1]
	var source *os.File
	switch args[0] {
	case "exec":
		if len(args) != 3 {
			return 2, errors.New("用法: smallssh exec 连接ID '完整命令'")
		}
		op.Kind = "exec"
		op.Command = args[2]
	case "monitor":
		op.Kind = "monitor"
		ttl = 15 * time.Minute
	case "shell":
		if len(args) != 2 {
			return 2, errors.New("用法: smallssh shell 连接ID")
		}
		op.Kind = "shell"
		ttl = 15 * time.Minute
	case "ls":
		if len(args) != 3 {
			return 2, errors.New("用法: smallssh ls 连接ID /绝对路径")
		}
		op.Kind = "list"
		op.Path = args[2]
	case "get":
		if len(args) != 4 {
			return 2, errors.New("用法: smallssh get 连接ID /远程路径 本地文件")
		}
		op.Kind = "get"
		op.Path = args[2]
	case "put", "replace":
		if (args[0] == "put" && len(args) != 4) || (args[0] == "replace" && len(args) != 5) {
			return 2, errors.New("用法: smallssh put 连接ID 本地文件 /远程路径；smallssh replace 需再提供原文件 SHA256")
		}
		op.Kind = args[0]
		op.Path = args[3]
		if op.Kind == "replace" {
			op.OriginalHash = args[4]
		}
		var e error
		source, e = os.Open(args[2])
		if e != nil {
			return 1, e
		}
		defer source.Close()
		st, e := source.Stat()
		if e != nil {
			return 1, e
		}
		if !st.Mode().IsRegular() {
			return 1, errors.New("只支持上传普通文件")
		}
		h := sha256.New()
		if _, e = io.Copy(h, source); e != nil {
			return 1, e
		}
		op.ContentHash = hex.EncodeToString(h.Sum(nil))
		if _, e = source.Seek(0, 0); e != nil {
			return 1, e
		}
	default:
		return 2, errors.New("未知命令；运行 smallssh help 查看用法")
	}
	r, ticket, e := c.Request(ctx, op, ttl)
	if e != nil {
		return 1, e
	}
	writeApprovalNotice(os.Stderr, r)
	if e = c.Wait(ctx, r.ID, ticket); e != nil {
		return 1, e
	}
	cols, rows := 80, 24
	if term.IsTerminal(int(os.Stdin.Fd())) {
		cols, rows, _ = term.GetSize(int(os.Stdin.Fd()))
	}
	stream, e := c.Start(ctx, r.ID, ticket, cols, rows)
	if e != nil {
		return 1, e
	}
	defer stream.Close()
	if op.Kind == "monitor" {
		for {
			m, e := stream.Receive()
			if e != nil {
				return 1, e
			}
			if m.Error != "" {
				return 1, errors.New(m.Error)
			}
			if m.Stats != nil {
				b, _ := json.Marshal(m.Stats)
				fmt.Println(string(b))
			}
			if m.Action == "done" {
				return 0, nil
			}
		}
	}
	if op.Kind == "shell" {
		if !term.IsTerminal(int(os.Stdin.Fd())) {
			return 1, errors.New("交互会话需要终端")
		}
		old, e := term.MakeRaw(int(os.Stdin.Fd()))
		if e != nil {
			return 1, e
		}
		defer term.Restore(int(os.Stdin.Fd()), old)
		stop := watchResize(ctx, stream)
		defer stop()
		go func() { io.Copy(stream, os.Stdin); stream.Close() }()
		returnCode, _, e := stream.Copy(os.Stdout, os.Stderr)
		return returnCode, e
	}
	if op.Kind == "put" || op.Kind == "replace" {
		go func() {
			if _, e := io.Copy(stream, source); e != nil {
				stream.Close()
				return
			}
			if stream.End() != nil {
				stream.Close()
			}
		}()
	}
	out := io.Writer(os.Stdout)
	var target *os.File
	success := false
	if op.Kind == "get" {
		target, e = os.OpenFile(args[3], os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
		if e != nil {
			return 1, e
		}
		defer func() {
			target.Close()
			if !success {
				os.Remove(args[3])
			}
		}()
		out = target
	}
	code, files, e := stream.Copy(out, os.Stderr)
	if e != nil {
		return 1, e
	}
	if target != nil {
		if e = target.Sync(); e != nil {
			return 1, e
		}
	}
	success = true
	if op.Kind == "list" {
		return 0, json.NewEncoder(os.Stdout).Encode(files)
	}
	return code, nil
}

func addHost(dir string) error {
	release, e := localipc.Acquire(dir)
	if e != nil {
		return e
	}
	defer release()
	pass, e := password("解锁保险库: ")
	if e != nil {
		return e
	}
	defer wipe(pass)
	v, e := vault.Read(broker.VaultPath(dir), pass)
	if e != nil {
		return e
	}
	defer v.Close()
	tty, e := openTTY()
	if e != nil {
		return e
	}
	defer tty.Close()
	reader := bufio.NewReader(tty)
	ask := func(label string) string {
		fmt.Fprint(os.Stderr, label)
		s, _ := reader.ReadString('\n')
		return strings.TrimSpace(s)
	}
	p := vault.Profile{ID: ask("连接 ID（唯一；相同ID会更新）: "), Name: ask("显示名称: "), Group: ask("连接分组（可空）: "), Host: ask("服务器地址: "), Port: 22, User: ask("SSH 用户: "), Fingerprint: ask("已通过可信渠道核对的 SHA256 主机指纹: "), Auth: ask("登录方式 password / key / agent: ")}
	port := ask("端口（默认22）: ")
	if port != "" {
		if _, e = fmt.Sscan(port, &p.Port); e != nil {
			return e
		}
	}
	switch p.Auth {
	case "password":
		b, e := password("SSH 密码: ")
		if e != nil {
			return e
		}
		p.Password = string(b)
		wipe(b)
	case "key":
		name := ask("私钥文件路径: ")
		f, e := os.Open(name)
		if e != nil {
			return e
		}
		b, e := io.ReadAll(io.LimitReader(f, 1<<20))
		f.Close()
		if e != nil {
			return e
		}
		p.PrivateKey = string(b)
		wipe(b)
		b, e = password("私钥口令（没有则回车）: ")
		if e != nil {
			return e
		}
		p.KeyPassphrase = string(b)
		wipe(b)
	case "agent":
		p.AgentFingerprint = ask("指定 SSH 代理公钥 SHA256 指纹: ")
	}
	if e = v.Put(p); e != nil {
		return e
	}
	return vault.Save(broker.VaultPath(dir), v, false)
}
func probe(ctx context.Context, addr string) error {
	if _, _, e := net.SplitHostPort(addr); e != nil {
		return errors.New("请提供 主机:端口，例如 example.com:22")
	}
	c, e := (&net.Dialer{Timeout: 10 * time.Second}).DialContext(ctx, "tcp", addr)
	if e != nil {
		return e
	}
	defer c.Close()
	c.SetDeadline(time.Now().Add(10 * time.Second))
	seen := false
	_, _, _, e = ssh.NewClientConn(c, addr, &ssh.ClientConfig{User: "fingerprint-probe", HostKeyCallback: func(_ string, _ net.Addr, k ssh.PublicKey) error {
		seen = true
		fmt.Println(ssh.FingerprintSHA256(k))
		return errors.New("probe complete")
	}})
	if seen {
		fmt.Fprintln(os.Stderr, "这是网络探测结果，尚未验证；请与服务器控制台显示的指纹比对后录入。")
		return nil
	}
	return e
}
func password(prompt string) ([]byte, error) {
	f, e := openTTY()
	if e != nil {
		return nil, e
	}
	defer f.Close()
	fmt.Fprint(os.Stderr, prompt)
	p, e := term.ReadPassword(int(f.Fd()))
	fmt.Fprintln(os.Stderr)
	return p, e
}
func wipe(b []byte) {
	for i := range b {
		b[i] = 0
	}
}

func openOwner(ctx context.Context, dir string, biometric bool, reason string) (*vault.Session, error) {
	if biometric {
		return systemauth.New(0).Open(ctx, dir, reason)
	}
	pass, err := password(reason + " · 主密码: ")
	if err != nil {
		return nil, err
	}
	defer wipe(pass)
	return vault.Read(broker.VaultPath(dir), pass)
}
