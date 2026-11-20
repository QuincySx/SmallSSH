// Package vncview serves the bundled noVNC client and bridges its WebSocket to
// one native-selected VNC destination or a verified loopback SSH tunnel.
// It never opens an SSH connection itself.
package vncview

import (
	"context"
	"crypto/rand"
	"embed"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"io/fs"
	"net"
	"net/http"
	"net/netip"
	"strings"
	"sync"
	"time"

	"github.com/sagernet/ws"
	"github.com/sagernet/ws/wsutil"
	"sshclient/internal/connectcfg"
)

//go:embed web
var assets embed.FS

// Session is private to one viewer. Close releases its listener and every
// upgraded connection, including idle WebSockets that HTTP shutdown omits.
type Session struct {
	credentials               Credentials
	saveCredentials           func(context.Context, Credentials) error
	fullscreen                func(context.Context) error
	clipboard                 func(context.Context, bool, string) (string, error)
	url, host, prefix, origin string
	server                    *http.Server
	ctx                       context.Context
	cancel                    context.CancelFunc
	mu                        sync.Mutex
	connections               map[net.Conn]bool
	closed                    bool
	viewer                    chan struct{}
}

// Start accepts only the concrete loopback address returned by the SSH daemon.
// No HTTP parameter can select a destination, and no credential is in the URL.
func Start(parent context.Context, tunnel, name string, viewOnly bool) (*Session, error) {
	address, err := netip.ParseAddrPort(tunnel)
	if err != nil || !address.Addr().IsLoopback() || address.Port() == 0 {
		return nil, errors.New("VNC requires an allocated loopback SSH tunnel")
	}
	return start(parent, address.String(), name, viewOnly, true)
}

// StartDirect connects only to the target chosen by the native frontend. HTTP
// requests cannot change this destination, including after a reconnect.
func StartDirect(parent context.Context, target, name string, viewOnly bool) (*Session, error) {
	if err := connectcfg.Address(target, false); err != nil {
		return nil, err
	}
	return start(parent, target, name, viewOnly, false)
}

func start(parent context.Context, target, name string, viewOnly, viaSSH bool) (*Session, error) {
	listener, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		return nil, err
	}
	token := make([]byte, 32)
	if _, err = rand.Read(token); err != nil {
		listener.Close()
		return nil, err
	}
	ctx, cancel := context.WithCancel(parent)
	s := &Session{host: listener.Addr().String(), prefix: "/" + hex.EncodeToString(token) + "/", ctx: ctx, cancel: cancel, connections: make(map[net.Conn]bool), viewer: make(chan struct{}, 1)}
	s.origin = "http://" + s.host
	s.url = s.origin + s.prefix
	files, _ := fs.Sub(assets, "web")
	fileServer := http.StripPrefix(s.prefix, http.FileServer(http.FS(files)))
	s.server = &http.Server{ReadHeaderTimeout: 5 * time.Second, IdleTimeout: 30 * time.Second, MaxHeaderBytes: 16 << 10}
	s.server.Handler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		credentialPost := (r.URL.Path == s.prefix+"credentials" || r.URL.Path == s.prefix+"fullscreen" || r.URL.Path == s.prefix+"clipboard") && r.Method == http.MethodPost
		if r.Host != s.host || !strings.HasPrefix(r.URL.Path, s.prefix) || (r.Method != http.MethodGet && !credentialPost) || (r.Header.Get("Origin") != "" && r.Header.Get("Origin") != s.origin) {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Cache-Control", "no-store")
		w.Header().Set("Referrer-Policy", "no-referrer")
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("Content-Security-Policy", "default-src 'none'; script-src 'self'; style-src 'self'; img-src 'self' data: blob:; connect-src 'self' ws://"+s.host+"; worker-src 'self' blob:; frame-ancestors 'none'; base-uri 'none'; form-action 'none'")
		switch strings.TrimPrefix(r.URL.Path, s.prefix) {
		case "clipboard":
			s.handleClipboard(w, r)
		case "fullscreen":
			s.toggleFullscreen(w, r)
		case "credentials":
			s.storeCredentials(w, r)
		case "socket":
			if r.Header.Get("Origin") != s.origin {
				http.Error(w, "origin denied", http.StatusForbidden)
				return
			}
			s.bridge(w, r, target)
		case "config.json":
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(struct {
				Credentials Credentials `json:"credentials"`
				Name        string      `json:"name"`
				ViewOnly    bool        `json:"viewOnly"`
				ViaSSH      bool        `json:"viaSSH"`
			}{s.currentCredentials(), name, viewOnly, viaSSH})
		default:
			fileServer.ServeHTTP(w, r)
		}
	})
	context.AfterFunc(ctx, func() { s.Close() })
	go func() {
		_ = s.server.Serve(listener)
		s.Close()
	}()
	return s, nil
}

func (s *Session) URL() string { return s.url }

func (s *Session) Close() {
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return
	}
	s.closed = true
	s.credentials, s.saveCredentials, s.fullscreen, s.clipboard = Credentials{}, nil, nil, nil
	connections := make([]net.Conn, 0, len(s.connections))
	for conn := range s.connections {
		connections = append(connections, conn)
	}
	s.mu.Unlock()
	s.cancel()
	_ = s.server.Close()
	for _, conn := range connections {
		_ = conn.Close()
	}
}

func (s *Session) track(conn net.Conn) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		conn.Close()
		return false
	}
	s.connections[conn] = true
	return true
}

func (s *Session) release(conn net.Conn) {
	conn.Close()
	s.mu.Lock()
	delete(s.connections, conn)
	s.mu.Unlock()
}

func (s *Session) bridge(w http.ResponseWriter, r *http.Request, target string) {
	select {
	case s.viewer <- struct{}{}:
		defer func() { <-s.viewer }()
	default:
		http.Error(w, "viewer already connected", http.StatusConflict)
		return
	}
	dialer := net.Dialer{Timeout: 5 * time.Second}
	tcp, err := dialer.DialContext(s.ctx, "tcp", target)
	if err != nil {
		http.Error(w, "SSH tunnel unavailable", http.StatusBadGateway)
		return
	}
	if !s.track(tcp) {
		return
	}
	defer s.release(tcp)
	socket, buffered, _, err := ws.UpgradeHTTP(r, w)
	if err != nil {
		return
	}
	if !s.track(socket) {
		return
	}
	defer s.release(socket)
	_ = socket.SetDeadline(time.Time{})
	var writeMu sync.Mutex
	writeFrame := func(op ws.OpCode, payload []byte) error {
		writeMu.Lock()
		defer writeMu.Unlock()
		return wsutil.WriteServerMessage(socket, op, payload)
	}
	done := make(chan struct{})
	go func() {
		defer close(done)
		defer socket.Close()
		buffer := make([]byte, 32<<10)
		for {
			n, err := tcp.Read(buffer)
			if n > 0 {
				if err := writeFrame(ws.OpBinary, buffer[:n]); err != nil {
					return
				}
			}
			if err != nil {
				return
			}
		}
	}()
	defer func() {
		tcp.Close()
		socket.Close()
		<-done
	}()
	reader := wsutil.Reader{Source: buffered, State: ws.StateServerSide, MaxFrameSize: 1 << 20}
	reader.OnIntermediate = func(header ws.Header, payload io.Reader) error {
		writeMu.Lock()
		defer writeMu.Unlock()
		return (wsutil.ControlHandler{Src: payload, Dst: socket, State: ws.StateServerSide, DisableSrcCiphering: true}).Handle(header)
	}
	for {
		header, err := reader.NextFrame()
		if err != nil {
			return
		}
		if header.OpCode.IsControl() {
			if err := reader.OnIntermediate(header, &reader); err != nil {
				return
			}
			continue
		}
		if header.OpCode != ws.OpBinary {
			return
		}
		if _, err := io.Copy(tcp, &reader); err != nil {
			return
		}
	}
}
