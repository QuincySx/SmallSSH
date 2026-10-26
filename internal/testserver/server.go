// Package testserver provides generated, loopback-only SSH fixtures for tests.
package testserver

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/pem"
	"github.com/pkg/sftp"
	"golang.org/x/crypto/ssh"
	"io"
	"net"
	"os/exec"
	"path/filepath"
	"runtime"
	"sshclient/internal/accesspolicy"
	"sshclient/internal/monitor"
	"sshclient/internal/vault"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
)

type Server struct {
	Listener   net.Listener
	Host       ssh.Signer
	Key        ssh.Signer
	PrivateKey string
	password   string
	Auths      atomic.Int32
	Sizes      chan [2]uint32
	Started    chan struct{}
	mu         sync.Mutex
	conns      []net.Conn
	wg         sync.WaitGroup
	exec       func(string) (string, string, int)
	streamExec func(context.Context, string, io.Reader, io.Writer, io.Writer) int
}

func RemotePath(p string) string {
	p = filepath.ToSlash(p)
	if runtime.GOOS == "windows" {
		return "/" + p
	}
	return p
}
func LocalPath(p string) string {
	if runtime.GOOS == "windows" {
		return filepath.FromSlash(strings.TrimPrefix(p, "/"))
	}
	return p
}

func New(t testing.TB) *Server {
	return NewWithExec(t, nil)
}

// NewWithExec supplies an isolated command fixture. The default server never
// runs arbitrary commands on the developer's computer.
func NewWithExec(t testing.TB, execute func(string) (string, string, int)) *Server {
	return newServer(t, execute, nil)
}

func NewWithStreamExec(t testing.TB, execute func(context.Context, string, io.Reader, io.Writer, io.Writer) int) *Server {
	return newServer(t, nil, execute)
}

func newServer(t testing.TB, execute func(string) (string, string, int), streamExec func(context.Context, string, io.Reader, io.Writer, io.Writer) int) *Server {
	t.Helper()
	_, priv, e := ed25519.GenerateKey(rand.Reader)
	if e != nil {
		t.Fatal(e)
	}
	host, _ := ssh.NewSignerFromKey(priv)
	_, key, _ := ed25519.GenerateKey(rand.Reader)
	signer, _ := ssh.NewSignerFromKey(key)
	block, _ := ssh.MarshalPrivateKey(key, "")
	s := &Server{Host: host, Key: signer, PrivateKey: string(pem.EncodeToMemory(block)), Sizes: make(chan [2]uint32, 16), Started: make(chan struct{}, 16), password: vault.ShortID(), exec: execute, streamExec: streamExec}
	config := &ssh.ServerConfig{PasswordCallback: func(_ ssh.ConnMetadata, p []byte) (*ssh.Permissions, error) {
		s.Auths.Add(1)
		if string(p) != s.password {
			return nil, io.EOF
		}
		return nil, nil
	}, PublicKeyCallback: func(_ ssh.ConnMetadata, k ssh.PublicKey) (*ssh.Permissions, error) {
		s.Auths.Add(1)
		if string(k.Marshal()) != string(s.Key.PublicKey().Marshal()) {
			return nil, io.EOF
		}
		return nil, nil
	}}
	config.AddHostKey(host)
	s.Listener, e = net.Listen("tcp", "127.0.0.1:0")
	if e != nil {
		t.Fatal(e)
	}
	s.wg.Go(func() {
		for {
			raw, e := s.Listener.Accept()
			if e != nil {
				return
			}
			s.mu.Lock()
			s.conns = append(s.conns, raw)
			s.mu.Unlock()
			s.wg.Go(func() {
				defer raw.Close()
				conn, chans, req, e := ssh.NewServerConn(raw, config)
				if e != nil {
					return
				}
				defer conn.Close()
				go ssh.DiscardRequests(req)
				for ch := range chans {
					if ch.ChannelType() != "session" {
						ch.Reject(ssh.UnknownChannelType, "unsupported")
						continue
					}
					c, r, e := ch.Accept()
					if e != nil {
						continue
					}
					s.wg.Go(func() { s.session(c, r) })
				}
			})
		}
	})
	t.Cleanup(s.Close)
	return s
}
func (s *Server) Close() {
	s.Listener.Close()
	s.mu.Lock()
	for _, c := range s.conns {
		c.Close()
	}
	s.mu.Unlock()
	s.wg.Wait()
}
func (s *Server) Profile() vault.Profile {
	host, port, _ := net.SplitHostPort(s.Listener.Addr().String())
	n, _ := strconv.Atoi(port)
	return vault.Profile{Policy: accesspolicy.Policy{Unrestricted: true}, ID: "fixture", Name: "Local test fixture", Host: host, Port: n, User: "fixture", Fingerprint: ssh.FingerprintSHA256(s.Host.PublicKey()), Auth: "password", Password: s.password}
}
func (s *Server) session(c ssh.Channel, requests <-chan *ssh.Request) {
	defer c.Close()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	for r := range requests {
		switch r.Type {
		case "exec":
			var q struct{ Command string }
			ssh.Unmarshal(r.Payload, &q)
			r.Reply(true, nil)
			select {
			case s.Started <- struct{}{}:
			default:
			}
			if s.streamExec != nil {
				s.wg.Go(func() {
					code := s.streamExec(ctx, q.Command, c, c, c.Stderr())
					c.SendRequest("exit-status", false, ssh.Marshal(struct{ Status uint32 }{uint32(code)}))
					c.Close()
				})
				continue
			}
			if s.exec != nil {
				out, stderr, code := s.exec(q.Command)
				io.WriteString(c, out)
				io.WriteString(c.Stderr(), stderr)
				c.SendRequest("exit-status", false, ssh.Marshal(struct{ Status uint32 }{uint32(code)}))
				return
			}
			if q.Command == "block" {
				io.Copy(io.Discard, c)
				return
			}
			if (q.Command == monitor.Command || q.Command == monitor.InventoryCommand) && runtime.GOOS == "linux" {
				cmd := exec.Command("sh", "-c", q.Command)
				cmd.Stdout = c
				cmd.Stderr = c.Stderr()
				err := cmd.Run()
				code := uint32(0)
				if err != nil {
					code = 1
				}
				c.SendRequest("exit-status", false, ssh.Marshal(struct{ Status uint32 }{code}))
				return
			}
			if q.Command == monitor.Command || q.Command == monitor.InventoryCommand {
				io.WriteString(c.Stderr(), "Linux /proc is required\n")
				c.SendRequest("exit-status", false, ssh.Marshal(struct{ Status uint32 }{69}))
				return
			}
			io.WriteString(c, "fixture stdout: "+q.Command)
			io.WriteString(c.Stderr(), "fixture stderr")
			code := uint32(0)
			if q.Command == "exit7" {
				code = 7
			}
			c.SendRequest("exit-status", false, ssh.Marshal(struct{ Status uint32 }{code}))
			return
		case "subsystem":
			var q struct{ Name string }
			ssh.Unmarshal(r.Payload, &q)
			if q.Name != "sftp" {
				r.Reply(false, nil)
				continue
			}
			r.Reply(true, nil)
			server, e := sftp.NewServer(c)
			if e != nil {
				return
			}
			server.Serve()
			server.Close()
			return
		case "pty-req":
			var q struct {
				Term                      string
				Cols, Rows, Width, Height uint32
				Modes                     string
			}
			ssh.Unmarshal(r.Payload, &q)
			s.Sizes <- [2]uint32{q.Cols, q.Rows}
			r.Reply(true, nil)
		case "window-change":
			var q struct{ Cols, Rows, Width, Height uint32 }
			ssh.Unmarshal(r.Payload, &q)
			s.Sizes <- [2]uint32{q.Cols, q.Rows}
		case "shell":
			r.Reply(true, nil)
			s.Started <- struct{}{}
			go func() { io.Copy(c, c); c.Close() }()
		default:
			r.Reply(false, nil)
		}
	}
}
