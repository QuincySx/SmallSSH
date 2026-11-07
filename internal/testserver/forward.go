package testserver

import (
	"context"
	"io"
	"net"
	"strconv"
	"sync"
	"testing"

	"golang.org/x/crypto/ssh"
)

type serverOptions struct {
	publicKey                 ssh.PublicKey
	sessionGate               func() bool
	forwarding, dropKeepalive bool
	password                  *string
}

func NewWithAcceptedPublicKey(t testing.TB, key ssh.PublicKey) *Server {
	return newServer(t, nil, nil, serverOptions{publicKey: key})
}

func NewWithForwarding(t testing.TB) *Server {
	return newServer(t, nil, nil, serverOptions{forwarding: true})
}
func NewWithStalledKeepalive(t testing.TB) *Server {
	return newServer(t, nil, nil, serverOptions{dropKeepalive: true})
}
func loopback(host string) bool { ip := net.ParseIP(host); return ip != nil && ip.IsLoopback() }
func (s *Server) directForward(ctx context.Context, request ssh.NewChannel) {
	var target struct {
		Host       string
		Port       uint32
		Origin     string
		OriginPort uint32
	}
	if ssh.Unmarshal(request.ExtraData(), &target) != nil || !loopback(target.Host) || target.Port == 0 || target.Port > 65535 {
		request.Reject(ssh.Prohibited, "fixture permits loopback targets only")
		return
	}
	socket, e := (&net.Dialer{}).DialContext(ctx, "tcp", net.JoinHostPort(target.Host, strconv.Itoa(int(target.Port))))
	if e != nil {
		request.Reject(ssh.ConnectionFailed, "fixture target unavailable")
		return
	}
	channel, requests, e := request.Accept()
	if e != nil {
		socket.Close()
		return
	}
	go ssh.DiscardRequests(requests)
	fixtureSplice(ctx, socket, channel)
}
func fixtureSplice(ctx context.Context, a, b io.ReadWriteCloser) {
	defer a.Close()
	defer b.Close()
	stop := context.AfterFunc(ctx, func() { a.Close(); b.Close() })
	defer stop()
	done := make(chan struct{})
	go func() { io.Copy(a, b); a.Close(); b.Close(); close(done) }()
	io.Copy(b, a)
	a.Close()
	b.Close()
	<-done
}
func (s *Server) handleRequests(ctx context.Context, conn *ssh.ServerConn, requests <-chan *ssh.Request, options serverOptions) {
	var mu sync.Mutex
	listeners := map[string]net.Listener{}
	stop := context.AfterFunc(ctx, func() {
		mu.Lock()
		defer mu.Unlock()
		for _, l := range listeners {
			l.Close()
		}
	})
	defer stop()
	defer func() {
		mu.Lock()
		defer mu.Unlock()
		for _, l := range listeners {
			l.Close()
		}
	}()
	for request := range requests {
		select {
		case s.Requests <- request.Type:
		default:
		}
		if request.Type == "keepalive@openssh.com" && options.dropKeepalive {
			continue
		}
		if !options.forwarding || request.Type != "tcpip-forward" && request.Type != "cancel-tcpip-forward" {
			request.Reply(false, nil)
			continue
		}
		var target struct {
			Host string
			Port uint32
		}
		if ssh.Unmarshal(request.Payload, &target) != nil || !loopback(target.Host) || target.Port > 65535 {
			request.Reply(false, nil)
			continue
		}
		key := net.JoinHostPort(target.Host, strconv.Itoa(int(target.Port)))
		if request.Type == "cancel-tcpip-forward" {
			mu.Lock()
			l := listeners[key]
			delete(listeners, key)
			mu.Unlock()
			if l != nil {
				l.Close()
			}
			request.Reply(l != nil, nil)
			continue
		}
		listener, e := (&net.ListenConfig{}).Listen(ctx, "tcp", key)
		if e != nil {
			request.Reply(false, nil)
			continue
		}
		port := uint32(listener.Addr().(*net.TCPAddr).Port)
		key = net.JoinHostPort(target.Host, strconv.Itoa(int(port)))
		mu.Lock()
		listeners[key] = listener
		mu.Unlock()
		request.Reply(true, ssh.Marshal(struct{ Port uint32 }{port}))
		s.wg.Go(func() {
			for {
				socket, e := listener.Accept()
				if e != nil {
					return
				}
				s.wg.Go(func() {
					origin := socket.RemoteAddr().(*net.TCPAddr)
					payload := struct {
						Host       string
						Port       uint32
						Origin     string
						OriginPort uint32
					}{target.Host, port, origin.IP.String(), uint32(origin.Port)}
					channel, reqs, e := conn.OpenChannel("forwarded-tcpip", ssh.Marshal(payload))
					if e != nil {
						socket.Close()
						return
					}
					go ssh.DiscardRequests(reqs)
					fixtureSplice(ctx, socket, channel)
				})
			}
		})
	}
}
