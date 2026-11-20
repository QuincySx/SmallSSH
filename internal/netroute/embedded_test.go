//go:build with_utls && with_quic

package netroute

import (
	"context"
	"errors"
	"io"
	"net"
	"testing"
	"time"

	"sshclient/internal/connectcfg"
	"sshclient/internal/proxyconfig"
	"sshclient/internal/testproxy"
)

func TestEmbeddedRouteSurvivesHandshakeContextAndClosesItsSessionPool(t *testing.T) {
	for _, kind := range []string{"anytls", "hysteria2", "snell6", "snell6-unshaped", "snell6-unsafe-raw"} {
		t.Run(kind, func(t *testing.T) {
			p := testproxy.New(t, kind)
			echo := proxyFixture(t, func(c net.Conn) { _, _ = io.Copy(c, c) })
			ctx, cancel := context.WithTimeout(context.Background(), 4*time.Second)
			conn, err := Dial(ctx, echo, p)
			if err != nil {
				cancel()
				t.Fatal(err)
			}
			defer conn.Close()
			cancel() // SSH cancels its handshake context after successful dialing.
			_ = conn.SetDeadline(time.Now().Add(time.Second))
			if _, err := io.WriteString(conn, "alive"); err != nil {
				t.Fatal("successful socket tied to dial context", err)
			}
			data := make([]byte, 5)
			if _, err := io.ReadFull(conn, data); err != nil || string(data) != "alive" {
				t.Fatal("proxy session did not survive handshake context", err)
			}
			closed := make(chan struct{})
			go func() { _ = conn.Close(); _ = conn.Close(); close(closed) }()
			select {
			case <-closed:
			case <-time.After(2 * time.Second):
				t.Fatal("closing the socket left proxy lifecycle running")
			}
			if _, err := conn.Write([]byte("closed")); err == nil {
				t.Fatal("closed route accepted traffic")
			}
		})
	}
}

func TestEmbeddedRouteFailureNeverFallsBackAndHonorsCancellation(t *testing.T) {
	contacted := make(chan struct{}, 1)
	target := proxyFixture(t, func(net.Conn) { contacted <- struct{}{} })
	nodes, err := proxyconfig.Import(`{"type":"anytls","server":"127.0.0.1","server_port":1,"password":"synthetic","tls":{"enabled":true,"server_name":"proxy.invalid"}}`)
	if err != nil {
		t.Fatal(err)
	}
	n := nodes[0]
	p := connectcfg.Proxy{Mode: n.Kind, Address: n.Address, Config: n.Config, Password: n.Secret}
	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()
	if conn, err := Dial(ctx, target, p); err == nil {
		conn.Close()
		t.Fatal("failed proxy dial succeeded")
	}
	select {
	case <-contacted:
		t.Fatal("failed proxy silently contacted the direct target")
	default:
	}
	cancelled, stop := context.WithCancel(context.Background())
	stop()
	if _, err := Dial(cancelled, target, p); !errors.Is(err, context.Canceled) {
		t.Fatal("cancelled proxy dial did not preserve context error", err)
	}
	p.Password = ""
	if conn, err := Dial(context.Background(), target, p); err == nil {
		conn.Close()
		t.Fatal("redacted config accepted for dialing")
	}
}
