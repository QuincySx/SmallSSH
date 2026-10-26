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
	"sshclient/internal/vault"
)

type Client struct {
	SSH    *ssh.Client
	cancel func() bool
	agent  io.Closer
}

func Dial(ctx context.Context, p vault.Profile) (*Client, error) {
	if err := vault.Validate(p); err != nil {
		return nil, err
	}
	var auth ssh.AuthMethod
	var agentConn io.ReadWriteCloser
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
		var err error
		agentConn, err = dialAgent(ctx)
		if err != nil {
			return nil, errors.New("SSH密钥代理不可用")
		}
		stopAgent := context.AfterFunc(ctx, func() { agentConn.Close() })
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
	}
	addr := net.JoinHostPort(p.Host, fmt.Sprint(p.Port))
	cfg := &ssh.ClientConfig{User: p.User, Auth: []ssh.AuthMethod{auth}, Timeout: 15 * time.Second,
		HostKeyCallback: func(_ string, _ net.Addr, key ssh.PublicKey) error {
			if ssh.FingerprintSHA256(key) != p.Fingerprint {
				return errors.New("服务器主机指纹与已确认记录不一致，连接已拒绝")
			}
			return nil
		}}
	raw, err := (&net.Dialer{Timeout: 15 * time.Second}).DialContext(ctx, "tcp", addr)
	if err != nil {
		if agentConn != nil {
			agentConn.Close()
		}
		return nil, err
	}
	stop := context.AfterFunc(ctx, func() {
		raw.Close()
		if agentConn != nil {
			agentConn.Close()
		}
	})
	raw.SetDeadline(time.Now().Add(15 * time.Second))
	conn, ch, req, err := ssh.NewClientConn(raw, addr, cfg)
	if err != nil {
		stop()
		raw.Close()
		if agentConn != nil {
			agentConn.Close()
		}
		return nil, err
	}
	raw.SetDeadline(time.Time{})
	if nc, ok := agentConn.(net.Conn); ok {
		nc.SetDeadline(time.Time{})
	}
	return &Client{SSH: ssh.NewClient(conn, ch, req), cancel: stop, agent: agentConn}, nil
}
func (c *Client) Close() error {
	c.cancel()
	if c.agent != nil {
		defer c.agent.Close()
	}
	return c.SSH.Close()
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
	if err = s.RequestPty("xterm-256color", rows, cols, ssh.TerminalModes{ssh.ECHO: 1, ssh.TTY_OP_ISPEED: 115200, ssh.TTY_OP_OSPEED: 115200}); err != nil {
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
	sh := &Shell{session: s, input: in, output: r, writer: w}
	stop := context.AfterFunc(ctx, func() { sh.Close() })
	go func() { s.Wait(); stop(); w.Close() }()
	return sh, nil
}
func (s *Shell) Read(b []byte) (int, error)  { return s.output.Read(b) }
func (s *Shell) Write(b []byte) (int, error) { return s.input.Write(b) }
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
