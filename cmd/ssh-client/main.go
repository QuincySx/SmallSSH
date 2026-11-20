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
	"sshclient/internal/client"
	"sshclient/internal/hardware"
	"sshclient/internal/localipc"
	"sshclient/internal/protocol"
	"sshclient/internal/transfer"
	"sshclient/internal/vault"
)

const help = `SmallSSH · 开发预览版

用法: smallssh [--data-dir 私有目录] <命令>
 init                          创建加密保险库
 host add                      添加或更新连接（需先关闭代理）
 host options 连接ID           从 stdin JSON 修改 terminal/network/hardware（所有者验证）
 host hardware 模块 [序列号]   读取 PKCS#11 令牌与公钥；选定令牌后读取本地 PIN
 tunnel local|remote|socks5 连接ID 回环监听地址 [目标主机:端口]
                               创建需批准的 TCP 隧道；Ctrl+C 关闭
 host group 连接ID 分组名       保留凭据并修改分组（需代理运行）
 probe 主机:端口               查看主机指纹；请通过服务器控制台另行核对
 agent                         启动代理；主密码仅从本地终端读取
 hosts                         列出连接 ID、分组与名称（无需主密码；不含地址与账号）
 monitor 连接ID                 连续 Linux 监控（JSON 行）
 docker logs/terminal [选项] 连接ID 完整容器ID
                               容器日志或交互终端，需指定 --engine
 docker plan/update [选项] 连接ID 项目名
                               预览 / 更新 Compose，需绑定配置与计划摘要
 transfer list                  查看持久化传输队列
 transfer resume 任务ID          校验并断点继续（可复用客户端会话）
 pending                       所有者验证后查看待授权和运行中的操作
 session serve [选项] 连接ID [read|guarded|maintenance|full] [2h] [15m]
                               常驻 JSON 行客户端；进程退出即撤销会话
 session maintenance [--read-root /绝对路径] 连接ID
                               一次批准系统检查、指定来源备份与 APT 升级预览
 session exec 连接ID [2h] [15m] 连续命令，获批后每行一条；:quit 退出
 --broker-key sha256:...        可选：显式固定可信所有者界面的代理公钥
 SSH_CLIENT_BROKER_KEY=sha256:... 覆盖首次可信解锁建立的本机配对
 approve ID / deny ID          核对完整操作并用主密码授权或拒绝
 revoke ID                     用主密码撤销授权并关闭连接
 exec 连接ID                   连续执行命令，复用本进程的已批准会话
 exec 连接ID '完整命令'        单次授权执行；多次运行不会共享授权
 shell 连接ID                  申请最长15分钟的完整交互会话
 ls 连接ID /绝对路径           浏览远程目录
 get 连接ID /远程路径 本地文件 下载（不覆盖本地文件）
 put 连接ID 本地文件 /远程路径 上传（绑定内容，不覆盖远程文件）
 replace 连接ID 本地文件 /远程路径 原文件SHA256
                               核对原内容、保留旧副本并原子覆盖

AI 通过常驻 session serve 或同进程 SDK 申请会话；范围和期限内逐次签发子授权。
会话仅绑定当前进程实例和活连接；断线、重启后生成新密钥并重新批准。
维护准备到期即结束；其他会话自然过期可申请原范围续期。
单次请求留在授权中心，不逐条弹窗。
允许 exec/shell 即允许该 SSH 账户权限范围内的操作。主密码不要写进参数。
`

func main() {
	if hardware.MaybeWorker() {
		return
	}
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
	biometric := fs.Bool("biometric", false, "已停用：系统验证只在桌面端使用")
	label := fs.String("client", "CLI", "申请方标签（未经身份验证）")
	sessionFile := fs.String("session", "", "已停用：会话文件不能跨进程复用")
	brokerKey := fs.String("broker-key", os.Getenv("SSH_CLIENT_BROKER_KEY"), "可选：显式固定可信所有者的代理公钥 sha256:...（默认使用本机配对）")
	if e := fs.Parse(os.Args[1:]); e != nil {
		return 2, e
	}
	args := fs.Args()
	// System verification can be raised by any process that runs the CLI in a
	// terminal it controls, with whatever prompt it likes; it now lives only in
	// the desktop app, which shows what is being authorised.
	if *biometric {
		return 2, errors.New("--biometric 已停用：系统验证（Touch ID 等）只在桌面端使用；CLI 请在本机终端输入主密码")
	}
	if len(args) == 0 || args[0] == "help" {
		fmt.Print(help)
		return 0, nil
	}
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt)
	defer cancel()
	c := client.Client{Dir: *dir, BrokerKey: *brokerKey}
	sessionFlagSet := false
	fs.Visit(func(f *flag.Flag) {
		if f.Name == "session" {
			sessionFlagSet = true
		}
	})
	selectedSession := sessionForCommand(args, *sessionFile, sessionFlagSet, os.Getenv("SSH_CLIENT_SESSION"))
	if selectedSession != "" {
		return 1, client.ErrSessionNotExportable
	}

	switch args[0] {
	case "tunnel":
		return tunnelCommand(ctx, c, *label, args[1:])
	case "docker":
		return dockerCommand(ctx, c, *label, args[1:])
	case "session":
		return sessionCommand(ctx, c, *label, args[1:])
	case "exec":
		if len(args) == 2 {
			return execSession(ctx, c, *label, args[1:], os.Stdin, os.Stdout, os.Stderr)
		}
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
		if e = vault.Save(vault.Path(*dir), v, true); e != nil {
			return 1, e
		}
		fmt.Println("保险库已创建。接下来运行 host add。")
		return 0, nil
	case "host":
		if len(args) == 3 && args[1] == "options" {
			return configureHost(ctx, c, *dir, args[2])
		}
		if len(args) >= 3 && args[1] == "hardware" {
			return inspectHardwareCommand(ctx, c, *dir, args[2:])
		}
		if len(args) == 4 && args[1] == "group" {
			owner, e := openOwner(ctx, *dir, "修改 SSH 连接分组")
			if e != nil {
				return 1, e
			}
			defer owner.Close()
			h, e := c.CallWithOwner(ctx, protocol.Message{Action: "hosts"}, owner)
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
		return 2, errors.New("系统验证改在桌面端「设置 → 安全与解锁」管理；CLI 只使用主密码")
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
		local, err := client.NewLocalService(*dir)
		if err != nil {
			return 1, err
		}
		if err = local.Ensure(ctx); err != nil {
			return 1, err
		}
		owner, err := openOwner(ctx, *dir, "解锁 SSH 代理")
		if err != nil {
			return 1, err
		}
		prepared, err := local.Client().PrepareOwner(ctx, owner, 8*time.Hour)
		owner.Close()
		if err != nil {
			return 1, err
		}
		defer prepared.Close()
		session, err := prepared.Commit(ctx)
		if err != nil {
			return 1, err
		}
		defer session.Close()
		pin, _ := session.BrokerPublicKeyPin()
		fmt.Fprintln(os.Stderr, "代理已启动。保持此窗口打开；退出会锁定并撤销所有会话。")
		fmt.Fprintln(os.Stderr, "本机代理公钥：", pin)
		select {
		case <-ctx.Done():
			cleanup, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			_, err = session.Lock(cleanup)
			return 0, err
		case <-session.Done():
			return 1, errors.New("本地服务所有者会话已结束，请重新解锁")
		}

	case "hosts":
		// Discovery needs no master password: without owner proof the broker
		// returns connection IDs, names and groups, never an address, account or
		// automation policy. That is what an automated caller needs to ask for
		// access to a machine it was told about.
		r, e := c.Call(ctx, protocol.Message{Action: "hosts"})
		if e != nil {
			return 1, e
		}
		for _, h := range r.Connections {
			fmt.Printf("%q\t%q\t%q\n", h.ID, h.Group, h.Name)
		}
		return 0, nil
	case "pending":
		owner, err := openOwner(ctx, *dir, "查看 SSH 管理信息")
		if err != nil {
			return 1, err
		}
		defer owner.Close()
		r, e := c.CallWithOwner(ctx, protocol.Message{Action: "pending"}, owner)
		if e != nil {
			return 1, e
		}
		for _, q := range r.Requests {
			fmt.Printf("%s\n状态: %s\n\n", client.Describe(q), q.Status)
		}
		return 0, nil
	case "approve", "deny", "revoke":
		if len(args) != 2 {
			return 2, errors.New("请提供完整授权 ID")
		}
		owner, err := openOwner(ctx, *dir, "查看并处理 SSH 授权")
		if err != nil {
			return 1, err
		}
		defer owner.Close()
		r, e := c.CallWithOwner(ctx, protocol.Message{Action: "status", ID: args[1]}, owner)
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
	case "replace":
		if len(args) != 5 {
			return 2, errors.New("用法: smallssh replace 连接ID 本地文件 /远程路径 原文件SHA256")
		}
		op.Kind = "replace"
		op.Path = args[3]
		op.OriginalHash = args[4]
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
	if op.Kind == "replace" {
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
	code, files, e := stream.Copy(os.Stdout, os.Stderr)
	if e != nil {
		return 1, e
	}
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
	v, e := vault.Read(vault.Path(dir), pass)
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
	return vault.Save(vault.Path(dir), v, false)
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

func openOwner(_ context.Context, dir string, reason string) (*vault.Session, error) {
	pass, err := password(reason + " · 主密码: ")
	if err != nil {
		return nil, err
	}
	defer wipe(pass)
	return vault.Read(vault.Path(dir), pass)
}
