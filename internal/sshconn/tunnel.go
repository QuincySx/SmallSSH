package sshconn

import (
	"context"
	"errors"
	"io"
	"net"
	"sync"
	"time"

	"sshclient/internal/connectcfg"
	"sshclient/internal/netroute"
)

// Forward owns an approved listener, its SSH transport and all accepted sockets.
type Forward struct {
	listener net.Listener
	ctx      context.Context
	cancel   context.CancelFunc
	abort    func()
	done     chan struct{}
	once     sync.Once
	mu       sync.Mutex
	sockets  map[net.Conn]struct{}
	closed   bool
	failure  error
	wg       sync.WaitGroup
}

func (c *Client) Forward(parent context.Context, spec connectcfg.Tunnel) (*Forward, error) {
	if e := spec.Validate(); e != nil {
		return nil, e
	}
	ctx, cancel := context.WithCancel(parent)
	var listener net.Listener
	var e error
	if spec.Kind == "remote" {
		listener, e = c.SSH.Listen("tcp", spec.Listen)
	} else {
		listener, e = (&net.ListenConfig{}).Listen(ctx, "tcp", spec.Listen)
	}
	if e != nil {
		cancel()
		return nil, e
	}
	f := &Forward{listener: listener, ctx: ctx, cancel: cancel, abort: c.closeTransport, done: make(chan struct{}), sockets: make(map[net.Conn]struct{})}
	stop := context.AfterFunc(ctx, func() { f.Close() })
	go func() {
		defer close(f.done)
		defer stop()
		slots := make(chan struct{}, 64)
		for {
			inbound, e := listener.Accept()
			if e != nil {
				f.mu.Lock()
				f.failure = e
				f.mu.Unlock()
				return
			}
			select {
			case slots <- struct{}{}:
			default:
				inbound.Close()
				continue
			}
			if !f.track(inbound) {
				<-slots
				continue
			}
			f.wg.Go(func() {
				defer func() { <-slots }()
				defer f.release(inbound)
				target := spec.Target
				inbound.SetDeadline(time.Now().Add(10 * time.Second))
				if spec.Kind == "socks5" {
					var e error
					target, e = socksRequest(inbound)
					if e != nil {
						return
					}
				}
				dialCtx, stopDial := context.WithTimeout(ctx, 10*time.Second)
				defer stopDial()
				var outbound net.Conn
				var err error
				if spec.Kind == "remote" {
					outbound, err = (&net.Dialer{}).DialContext(dialCtx, "tcp", target)
				} else {
					outbound, err = c.SSH.DialContext(dialCtx, "tcp", target)
				}
				if err != nil {
					if spec.Kind == "socks5" {
						_, _ = inbound.Write([]byte{5, 5, 0, 1, 0, 0, 0, 0, 0, 0})
					}
					return
				}
				if !f.track(outbound) {
					return
				}
				defer f.release(outbound)
				if spec.Kind == "socks5" {
					if _, err = inbound.Write([]byte{5, 0, 0, 1, 0, 0, 0, 0, 0, 0}); err != nil {
						return
					}
				}
				inbound.SetDeadline(time.Time{})
				splice(inbound, outbound)
			})
		}
	}()
	return f, nil
}
func (f *Forward) Address() string { return f.listener.Addr().String() }
func (f *Forward) track(c net.Conn) bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.closed {
		c.Close()
		return false
	}
	f.sockets[c] = struct{}{}
	return true
}
func (f *Forward) release(c net.Conn) { c.Close(); f.mu.Lock(); delete(f.sockets, c); f.mu.Unlock() }
func (f *Forward) Close() error {
	f.once.Do(func() {
		f.cancel()
		f.abort()
		f.listener.Close()
		f.mu.Lock()
		f.closed = true
		sockets := make([]net.Conn, 0, len(f.sockets))
		for c := range f.sockets {
			sockets = append(sockets, c)
		}
		f.mu.Unlock()
		for _, c := range sockets {
			c.Close()
		}
		<-f.done
		f.wg.Wait()
	})
	return nil
}
func (f *Forward) Wait() error {
	<-f.done
	if f.ctx.Err() != nil {
		return f.ctx.Err()
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.failure
}
func splice(a, b net.Conn) {
	done := make(chan struct{})
	go func() {
		_, e := io.Copy(a, b)
		if e != nil {
			a.Close()
			b.Close()
		} else if half, ok := a.(interface{ CloseWrite() error }); ok {
			half.CloseWrite()
		}
		close(done)
	}()
	if _, e := io.Copy(b, a); e != nil {
		a.Close()
		b.Close()
	} else if half, ok := b.(interface{ CloseWrite() error }); ok {
		half.CloseWrite()
	}
	<-done
}
func socksRequest(c net.Conn) (string, error) {
	var header [2]byte
	if _, e := io.ReadFull(c, header[:]); e != nil {
		return "", e
	}
	if header[0] != 5 || header[1] == 0 {
		return "", errors.New("SOCKS5 握手无效")
	}
	methods := make([]byte, int(header[1]))
	if _, e := io.ReadFull(c, methods); e != nil {
		return "", e
	}
	accepted := false
	for _, method := range methods {
		accepted = accepted || method == 0
	}
	if !accepted {
		c.Write([]byte{5, 255})
		return "", errors.New("此回环 SOCKS5 隧道仅支持无认证 CONNECT")
	}
	if _, e := c.Write([]byte{5, 0}); e != nil {
		return "", e
	}
	var request [3]byte
	if _, e := io.ReadFull(c, request[:]); e != nil {
		return "", e
	}
	if request != [3]byte{5, 1, 0} {
		c.Write([]byte{5, 7, 0, 1, 0, 0, 0, 0, 0, 0})
		return "", errors.New("仅支持 SOCKS5 TCP CONNECT，不支持 UDP / BIND")
	}
	target, e := netroute.ReadAddress(c)
	if e != nil {
		return "", e
	}
	if e = connectcfg.Address(target, false); e != nil {
		return "", e
	}
	return target, nil
}
