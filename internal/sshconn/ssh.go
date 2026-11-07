// Package sshconn is shared by the agent, desktop and CLI. Host verification is mandatory.
package sshconn

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"path"
	"strings"
	"sync"
	"time"

	"github.com/pkg/sftp"
	"golang.org/x/crypto/ssh"
	"golang.org/x/crypto/ssh/agent"
	"sshclient/internal/connectcfg"
	"sshclient/internal/hardware"
	"sshclient/internal/netroute"
	"sshclient/internal/terminalcodec"
	"sshclient/internal/vault"
)

type Client struct {
	SSH      *ssh.Client
	cancel   func() bool
	agent    io.Closer
	upstream *Client
	terminal connectcfg.Terminal
}

func Dial(ctx context.Context, p vault.Profile) (*Client, error) {
	if p.Network.Relay != "" {
		return nil, errors.New("中转连接必须先由保险库解析完整路由")
	}
	return dial(ctx, p, nil)
}

func DialRoute(ctx context.Context, route []vault.Profile) (*Client, error) {
	if len(route) == 0 || len(route) > 4 {
		return nil, errors.New("SSH 中转路由需为 1–4 跳")
	}
	var parent *Client
	for i := len(route) - 1; i >= 0; i-- {
		next, e := dial(ctx, route[i], parent)
		if e != nil {
			if parent != nil {
				parent.Close()
			}
			return nil, e
		}
		if parent != nil {
			parent.Detach()
		}
		parent = next
	}
	return parent, nil
}
func dial(ctx context.Context, p vault.Profile, upstream *Client) (*Client, error) {
	if err := vault.Validate(p); err != nil {
		return nil, err
	}
	handshake, cancelHandshake := context.WithTimeout(ctx, 15*time.Second)
	defer cancelHandshake()
	var auth ssh.AuthMethod
	var authResource io.Closer
	switch p.Auth {
	case "password":
		auth = ssh.Password(p.Password)
	case "key":
		signer, err := privateSigner(p.PrivateKey, p.KeyPassphrase)
		if err != nil {
			return nil, err
		}
		auth = ssh.PublicKeys(signer)
	case "agent":
		agentConn, err := dialAgent(handshake)
		if err != nil {
			return nil, errors.New("SSH密钥代理不可用")
		}
		authResource = agentConn
		stopAgent := context.AfterFunc(handshake, func() { agentConn.Close() })
		defer stopAgent()
		if nc, ok := agentConn.(net.Conn); ok {
			nc.SetDeadline(time.Now().Add(15 * time.Second))
		}
		signers, err := agent.NewClient(agentConn).Signers()
		if err != nil {
			agentConn.Close()
			return nil, errors.New("无法读取SSH代理身份")
		}
		selected := []ssh.Signer{}
		for _, s := range signers {
			if ssh.FingerprintSHA256(s.PublicKey()) == p.AgentFingerprint {
				selected = append(selected, s)
			}
		}
		if len(selected) != 1 {
			agentConn.Close()
			return nil, errors.New("SSH代理中未找到指定的唯一身份")
		}
		auth = ssh.PublicKeys(selected...)
	case "pkcs11":
		signer, resource, err := hardware.Open(handshake, p.Hardware)
		if err != nil {
			return nil, err
		}
		authResource = resource
		auth = ssh.PublicKeys(signer)
	}
	addr := net.JoinHostPort(p.Host, fmt.Sprint(p.Port))
	cfg := &ssh.ClientConfig{User: p.User, Auth: []ssh.AuthMethod{auth}, Timeout: 15 * time.Second,
		HostKeyCallback: func(_ string, _ net.Addr, key ssh.PublicKey) error {
			if ssh.FingerprintSHA256(key) != p.Fingerprint {
				return errors.New("服务器主机指纹与已确认记录不一致，连接已拒绝")
			}
			return nil
		}}
	var raw net.Conn
	var err error
	if upstream == nil {
		raw, err = netroute.Dial(handshake, addr, p.Network.Proxy)
	} else {
		raw, err = upstream.SSH.DialContext(handshake, "tcp", addr)
	}
	if err != nil {
		if authResource != nil {
			authResource.Close()
		}
		return nil, err
	}
	abort := func() {
		if upstream != nil {
			upstream.closeTransport()
		}
		raw.Close()
		if authResource != nil {
			authResource.Close()
		}
	}
	stop := context.AfterFunc(ctx, abort)
	stopHandshake := context.AfterFunc(handshake, abort)
	defer stopHandshake()
	raw.SetDeadline(time.Now().Add(15 * time.Second))
	conn, ch, req, err := ssh.NewClientConn(raw, addr, cfg)
	stopHandshake()
	if handshake.Err() != nil {
		err = handshake.Err()
	}
	if err != nil {
		stop()
		raw.Close()
		if authResource != nil {
			authResource.Close()
		}
		return nil, err
	}
	raw.SetDeadline(time.Time{})
	// Authentication has completed. Hardware PINs and agent sessions need not
	// survive for the transport's lifetime or remain in the idle pool.
	if authResource != nil {
		authResource.Close()
	}
	// The connection stays bound to the operation's context: a revoked or
	// expired operation closes its own transport instead of leaving a wedged
	// channel behind. A connection that outlives its operation is detached by
	// the pool, not here.
	client := &Client{SSH: ssh.NewClient(conn, ch, req), cancel: stop, agent: authResource, upstream: upstream, terminal: p.Terminal}
	client.keepalive(p.Network)
	return client, nil
}

// Detach ends the connection's tie to the operation context, so the pool can
// hand it to a later operation without the first operation's deadline or
// cancellation closing it.
func (c *Client) Detach() {
	if c == nil || c.cancel == nil {
		return
	}
	c.cancel()
	c.cancel = nil
}

// Bind transfers cancellation ownership to a new operation before it opens any
// channels. Callers must own the client exclusively, as with Detach and Close.
func (c *Client) Bind(ctx context.Context) {
	c.Detach()
	c.cancel = context.AfterFunc(ctx, c.closeTransport)
}

// closeTransport never reads mutable cancellation ownership.
func (c *Client) closeTransport() {
	if c.SSH != nil {
		c.SSH.Close()
	}
	if c.agent != nil {
		c.agent.Close()
	}
	if c.upstream != nil {
		c.upstream.closeTransport()
	}
}
func (c *Client) Close() error {
	if c != nil {
		if c.cancel != nil {
			c.cancel()
		}
		c.closeTransport()
	}
	return nil
}

func (c *Client) keepalive(settings connectcfg.Network) {
	if settings.KeepaliveSeconds == 0 {
		return
	}
	interval := time.Duration(settings.KeepaliveSeconds) * time.Second
	failures := settings.KeepaliveFailures
	if failures == 0 {
		failures = 3
	}
	done := make(chan struct{})
	go func() { c.SSH.Wait(); close(done) }()
	go func() {
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		for {
			select {
			case <-done:
				return
			case <-ticker.C:
			}
			reply := make(chan error, 1)
			go func() { _, _, e := c.SSH.SendRequest("keepalive@openssh.com", true, nil); reply <- e }()
			timeout := time.NewTimer(interval * time.Duration(failures))
			select {
			case <-done:
				timeout.Stop()
				return
			case e := <-reply:
				timeout.Stop()
				if e != nil {
					c.closeTransport()
					return
				}
			case <-timeout.C:
				c.closeTransport()
				return
			}
		}
	}()
}

type Result struct {
	Stdout   string `json:"stdout"`
	Stderr   string `json:"stderr"`
	ExitCode int    `json:"exitCode"`
}
type bounded struct {
	buf   []byte
	limit int
	mu    sync.Mutex
}

func (b *bounded) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if len(p) > b.limit-len(b.buf) {
		return 0, errors.New("输出超过上限，请使用交互终端或下载文件")
	}
	b.buf = append(b.buf, p...)
	return len(p), nil
}
func (c *Client) Exec(ctx context.Context, command string) (Result, error) {
	s, err := c.SSH.NewSession()
	if err != nil {
		return Result{}, err
	}
	defer s.Close()
	stop := context.AfterFunc(ctx, func() { s.Close() })
	defer stop()
	out, eout := &bounded{limit: 8 << 20}, &bounded{limit: 8 << 20}
	s.Stdout = out
	s.Stderr = eout
	err = s.Run(command)
	r := Result{Stdout: string(out.buf), Stderr: string(eout.buf)}
	if err != nil {
		var exit *ssh.ExitError
		if errors.As(err, &exit) {
			r.ExitCode = exit.ExitStatus()
			return r, nil
		}
		return r, err
	}
	return r, nil
}

type Shell struct {
	session *ssh.Session
	input   io.WriteCloser
	output  *io.PipeReader
	writer  *io.PipeWriter
	once    sync.Once
	decoded io.ReadCloser
	encoded io.WriteCloser
}

func (c *Client) Shell(ctx context.Context, cols, rows int) (*Shell, error) {
	return c.ShellCommand(ctx, cols, rows, "")
}

// ShellCommand uses the same PTY, resize and cancellation lifecycle for a
// broker-constructed container exec. An empty command opens the host shell.
func (c *Client) ShellCommand(ctx context.Context, cols, rows int, command string) (*Shell, error) {
	if cols > 1000 || rows > 1000 || cols < 0 || rows < 0 {
		return nil, errors.New("终端大小应在 1 到 1000 之间")
	}
	s, err := c.SSH.NewSession()
	if err != nil {
		return nil, err
	}
	in, err := s.StdinPipe()
	if err != nil {
		s.Close()
		return nil, err
	}
	r, w := io.Pipe()
	s.Stdout = w
	s.Stderr = w
	if cols < 1 {
		cols = 80
	}
	if rows < 1 {
		rows = 24
	}
	if err = s.RequestPty("xterm-256color", rows, cols, ssh.TerminalModes{ssh.VERASE: uint32(c.terminal.Erase()), ssh.ECHO: 1, ssh.TTY_OP_ISPEED: 115200, ssh.TTY_OP_OSPEED: 115200}); err != nil {
		s.Close()
		r.Close()
		w.Close()
		return nil, err
	}
	if command == "" {
		err = s.Shell()
	} else {
		err = s.Start(command)
	}
	if err != nil {
		s.Close()
		r.Close()
		w.Close()
		return nil, err
	}
	decoded, encoded, e := terminalcodec.New(r, in, c.terminal)
	if e != nil {
		s.Close()
		r.Close()
		w.Close()
		return nil, e
	}
	sh := &Shell{session: s, input: in, output: r, writer: w, decoded: decoded, encoded: encoded}
	stop := context.AfterFunc(ctx, func() { sh.Close() })
	go func() { s.Wait(); stop(); w.Close() }()
	return sh, nil
}
func (s *Shell) Read(b []byte) (int, error)  { return s.decoded.Read(b) }
func (s *Shell) Write(b []byte) (int, error) { return s.encoded.Write(b) }
func (s *Shell) Resize(cols, rows int) error {
	if cols < 1 || rows < 1 || cols > 1000 || rows > 1000 {
		return errors.New("终端大小不合法")
	}
	return s.session.WindowChange(rows, cols)
}
func (s *Shell) Close() error {
	s.once.Do(func() {
		s.session.Close()
		s.input.Close()
		s.output.Close()
		s.writer.Close()
		s.decoded.Close()
		s.encoded.Close()
	})
	return nil
}

type File struct {
	Modified  time.Time `json:"modified"`
	Name      string    `json:"name"`
	Size      int64     `json:"size"`
	Directory bool      `json:"directory"`
	Mode      string    `json:"mode"`
}

func remotePath(p string) error {
	if !strings.HasPrefix(p, "/") || strings.ContainsAny(p, "\x00\r\n") || path.Clean(p) != p {
		return errors.New("远程路径需为规范的绝对路径")
	}
	return nil
}
func (c *Client) List(ctx context.Context, p string) ([]File, error) {
	if err := remotePath(p); err != nil {
		return nil, err
	}
	s, err := sftp.NewClient(c.SSH)
	if err != nil {
		return nil, err
	}
	defer s.Close()
	stop := context.AfterFunc(ctx, func() { s.Close() })
	defer stop()
	entries, err := s.ReadDirContext(ctx, p)
	if err != nil {
		return nil, err
	}
	out := make([]File, 0, len(entries))
	for _, e := range entries {
		out = append(out, File{Modified: e.ModTime(), Name: e.Name(), Size: e.Size(), Directory: e.IsDir(), Mode: e.Mode().String()})
	}
	return out, nil
}
func (c *Client) Download(ctx context.Context, p string, w io.Writer) error {
	if err := remotePath(p); err != nil {
		return err
	}
	s, err := sftp.NewClient(c.SSH)
	if err != nil {
		return err
	}
	defer s.Close()
	info, err := s.Stat(p)
	if err != nil {
		return err
	}
	if !info.Mode().IsRegular() {
		return errors.New("只能读取普通文件")
	}
	f, err := s.Open(p)
	if err != nil {
		return err
	}
	defer f.Close()
	info, err = f.Stat()
	if err != nil || !info.Mode().IsRegular() {
		return errors.New("目标不是可读取的普通文件")
	}
	stop := context.AfterFunc(ctx, func() { f.Close(); s.Close() })
	defer stop()
	_, err = io.Copy(w, f)
	return err
}

// Upload never replaces an existing remote file. Editor saves require a separate reviewed overwrite flow.
func (c *Client) Upload(ctx context.Context, p string, r io.Reader, expectedHash string) error {
	if err := remotePath(p); err != nil {
		return err
	}
	s, err := sftp.NewClient(c.SSH)
	if err != nil {
		return err
	}
	defer s.Close()
	if _, err = s.Lstat(p); err == nil {
		return errors.New("目标已存在，初版拒绝覆盖")
	}
	if !os.IsNotExist(err) {
		return err
	}
	tmp := path.Join(path.Dir(p), ".ssh-upload-"+vault.ShortID())
	f, err := s.OpenFile(tmp, os.O_WRONLY|os.O_CREATE|os.O_EXCL)
	if err != nil {
		return err
	}
	defer s.Remove(tmp)
	defer f.Close()
	if err = f.Chmod(0600); err != nil {
		return err
	}
	stop := context.AfterFunc(ctx, func() { f.Close(); s.Close() })
	defer stop()
	hash := sha256.New()
	_, err = io.Copy(f, io.TeeReader(r, hash))
	if err != nil {
		return err
	}
	if hex.EncodeToString(hash.Sum(nil)) != expectedHash {
		return errors.New("上传内容与批准的文件指纹不一致")
	}
	if err = f.Close(); err != nil {
		return err
	}
	// OpenSSH's hardlink extension is an atomic no-clobber commit. A plain
	// rename can overwrite on some servers even when the SFTP spec forbids it.
	if err = s.Link(tmp, p); err != nil {
		return fmt.Errorf("无法无覆盖提交文件（服务器需支持 hardlink 扩展）: %w", err)
	}
	return nil
}
