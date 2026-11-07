package sshconn

import (
	"bufio"
	"context"
	"io"
	"net"
	"net/http"
	"strconv"
	"sync/atomic"
	"testing"
	"time"

	"sshclient/internal/connectcfg"
	"sshclient/internal/netroute"
	"sshclient/internal/testserver"
	"sshclient/internal/vault"
)

func echoFixture(t *testing.T) string {
	t.Helper()
	l, e := net.Listen("tcp", "127.0.0.1:0")
	if e != nil {
		t.Fatal(e)
	}
	t.Cleanup(func() { l.Close() })
	go func() {
		for {
			c, e := l.Accept()
			if e != nil {
				return
			}
			go func() { defer c.Close(); c.SetDeadline(time.Now().Add(3 * time.Second)); io.Copy(c, c) }()
		}
	}()
	return l.Addr().String()
}
func TestSSHTunnelsCarryTrafficAndCloseAcceptedSockets(t *testing.T) {
	for _, kind := range []string{"local", "remote", "socks5"} {
		t.Run(kind, func(t *testing.T) {
			fixture := testserver.NewWithForwarding(t)
			ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
			defer cancel()
			ssh, e := Dial(ctx, fixture.Profile())
			if e != nil {
				t.Fatal(e)
			}
			defer ssh.Close()
			spec := connectcfg.Tunnel{Kind: kind, Listen: "127.0.0.1:0", Target: echoFixture(t)}
			if kind == "socks5" {
				spec.Target = ""
			}
			forward, e := ssh.Forward(ctx, spec)
			if e != nil {
				t.Fatal(e)
			}
			defer forward.Close()
			var socket net.Conn
			if kind == "socks5" {
				socket, e = netroute.Dial(ctx, echoFixture(t), connectcfg.Proxy{Mode: "socks5", Address: forward.Address()})
			} else {
				socket, e = net.DialTimeout("tcp", forward.Address(), time.Second)
			}
			if e != nil {
				t.Fatal(e)
			}
			defer socket.Close()
			socket.SetDeadline(time.Now().Add(2 * time.Second))
			if _, e = io.WriteString(socket, "forwarded bytes"); e != nil {
				t.Fatal(e)
			}
			b := make([]byte, len("forwarded bytes"))
			if _, e = io.ReadFull(socket, b); e != nil || string(b) != "forwarded bytes" {
				t.Fatal(string(b), e)
			}
			cancel()
			closed := make(chan struct{})
			go func() { forward.Close(); close(closed) }()
			select {
			case <-closed:
			case <-time.After(time.Second):
				t.Fatal("forward ignored cancellation")
			}
			if c, e := net.DialTimeout("tcp", forward.Address(), 100*time.Millisecond); e == nil {
				c.Close()
				t.Fatal("cancelled listener stayed open")
			}
			if n, e := socket.Read(b); e == nil || n != 0 {
				t.Fatal("accepted tunnel connection survived cancellation", n, e)
			}
		})
	}
}
func TestSSHRelayVerifiesEveryHopAndExecutesThroughTheRelay(t *testing.T) {
	relay := testserver.NewWithForwarding(t)
	target := testserver.New(t)
	first, last := relay.Profile(), target.Profile()
	first.ID = "relay"
	last.ID = "destination"
	last.Network.Relay = first.ID
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	identity, e := ProbeRoute(ctx, last.Host, last.Port, last.Network, []vault.Profile{first})
	if e != nil || identity.Fingerprint != last.Fingerprint {
		t.Fatal("route probe did not reach the pinned destination", e)
	}
	connection, e := DialRoute(ctx, []vault.Profile{last, first})
	if e != nil {
		t.Fatal(e)
	}
	defer connection.Close()
	if result, e := connection.Exec(ctx, "through-relay"); e != nil || result.ExitCode != 0 {
		t.Fatal(result, e)
	}
	if target.Auths.Load() != 1 {
		t.Fatal("probe authenticated to the destination or the route bypassed authentication")
	}
	changed := first
	changed.Fingerprint = "SHA256:invalid"
	if bad, e := DialRoute(ctx, []vault.Profile{last, changed}); e == nil {
		bad.Close()
		t.Fatal("unverified relay accepted")
	}
	if bad, e := Dial(ctx, last); e == nil {
		bad.Close()
		t.Fatal("unresolved relay silently dialed directly")
	}
}
func TestSSHProxyFailureNeverFallsBackToDirect(t *testing.T) {
	server := testserver.New(t)
	profile := server.Profile()
	profile.Network.Proxy = connectcfg.Proxy{Mode: "http", Address: "127.0.0.1:1"}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if c, e := Dial(ctx, profile); e == nil {
		c.Close()
		t.Fatal("proxy failure fell back to direct")
	}
	if server.Auths.Load() != 0 {
		t.Fatal("direct destination received authentication after proxy failure")
	}
}
func TestSSHKeepaliveDetectsAnUnresponsivePeer(t *testing.T) {
	fixture := testserver.NewWithStalledKeepalive(t)
	profile := fixture.Profile()
	profile.Network.KeepaliveSeconds = 5
	profile.Network.KeepaliveFailures = 1
	ctx, cancel := context.WithTimeout(context.Background(), 13*time.Second)
	defer cancel()
	c, e := Dial(ctx, profile)
	if e != nil {
		t.Fatal(e)
	}
	defer c.Close()
	select {
	case request := <-fixture.Requests:
		if request != "keepalive@openssh.com" {
			t.Fatal(request)
		}
	case <-ctx.Done():
		t.Fatal("keepalive was never sent")
	}
	done := make(chan error, 1)
	go func() { done <- c.SSH.Wait() }()
	select {
	case <-done:
	case <-ctx.Done():
		t.Fatal("unresponsive SSH peer stayed connected")
	}
}

func TestRouteMeasurementsStopBeforeDestinationAuthenticationAndVerifyPins(t *testing.T) {
	relay, target := testserver.NewWithForwarding(t), testserver.New(t)
	first, last := relay.Profile(), target.Profile()
	first.ID = "relay"
	last.ID = "target"
	last.Network.Relay = first.ID
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	defer cancel()
	measured, e := MeasureRoutes(ctx, []vault.Profile{last, first})
	if e != nil || len(measured) != 2 {
		t.Fatal(measured, e)
	}
	for _, result := range measured {
		if !result.Available || result.HandshakeMillis < 1 {
			t.Fatal(result)
		}
	}
	if target.Auths.Load() != 0 {
		t.Fatal("route measurements authenticated to the destination")
	}
	last.Fingerprint = "SHA256:incorrect"
	measured, e = MeasureRoutes(ctx, []vault.Profile{last, first})
	if e != nil {
		t.Fatal(e)
	}
	for _, result := range measured {
		if result.Available {
			t.Fatal("recommended a route with the wrong destination identity")
		}
	}
	if target.Auths.Load() != 0 {
		t.Fatal("incorrect fingerprint caused destination authentication")
	}
}

func TestSSHAuthenticationAndHostProbeUseHTTPAndSOCKS5Routes(t *testing.T) {
	for _, mode := range []string{"http", "socks5"} {
		t.Run(mode, func(t *testing.T) {
			fixture := testserver.New(t)
			profile := fixture.Profile()
			target := net.JoinHostPort(profile.Host, strconv.Itoa(profile.Port))
			listener, e := net.Listen("tcp", "127.0.0.1:0")
			if e != nil {
				t.Fatal(e)
			}
			defer listener.Close()
			var connects atomic.Int32
			ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
			defer cancel()
			go func() {
				for {
					inbound, e := listener.Accept()
					if e != nil {
						return
					}
					go func() {
						defer inbound.Close()
						inbound.SetDeadline(time.Now().Add(3 * time.Second))
						address := ""
						if mode == "http" {
							request, e := http.ReadRequest(bufio.NewReader(inbound))
							if e != nil {
								return
							}
							address = request.Host
						} else {
							var greeting [3]byte
							if _, e = io.ReadFull(inbound, greeting[:]); e != nil || greeting != [3]byte{5, 1, 0} {
								return
							}
							inbound.Write([]byte{5, 0})
							var command [3]byte
							if _, e = io.ReadFull(inbound, command[:]); e != nil || command != [3]byte{5, 1, 0} {
								return
							}
							address, e = netroute.ReadAddress(inbound)
							if e != nil {
								return
							}
						}
						if address != target {
							return
						}
						connects.Add(1)
						outbound, e := net.DialTimeout("tcp", target, time.Second)
						if e != nil {
							return
						}
						defer outbound.Close()
						stop := context.AfterFunc(ctx, func() { inbound.Close(); outbound.Close() })
						defer stop()
						if mode == "http" {
							io.WriteString(inbound, "HTTP/1.1 200 Connection established\r\n\r\n")
						} else {
							inbound.Write([]byte{5, 0, 0, 1, 0, 0, 0, 0, 0, 0})
						}
						inbound.SetDeadline(time.Time{})
						splice(inbound, outbound)
					}()
				}
			}()
			profile.Network.Proxy = connectcfg.Proxy{Mode: mode, Address: listener.Addr().String()}
			if identity, e := ProbeRoute(ctx, profile.Host, profile.Port, profile.Network, nil); e != nil || identity.Fingerprint != profile.Fingerprint {
				t.Fatal(identity, e)
			}
			if fixture.Auths.Load() != 0 {
				t.Fatal("proxied host probe authenticated to destination")
			}
			connection, e := Dial(ctx, profile)
			if e != nil {
				t.Fatal(e)
			}
			if result, e := connection.Exec(ctx, "via-proxy"); e != nil || result.ExitCode != 0 {
				t.Fatal(result, e)
			}
			connection.Close()
			profile.Fingerprint = "SHA256:incorrect"
			if bad, e := Dial(ctx, profile); e == nil {
				bad.Close()
				t.Fatal("proxy bypassed host fingerprint validation")
			}
			if fixture.Auths.Load() != 1 || connects.Load() != 3 {
				t.Fatal("SSH bypassed proxy route or authenticated before fingerprint validation", fixture.Auths.Load(), connects.Load())
			}
		})
	}
}
