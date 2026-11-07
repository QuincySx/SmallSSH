package netroute

import (
	"bufio"
	"bytes"
	"context"
	"io"
	"log"
	"net"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"sshclient/internal/connectcfg"
)

func proxyFixture(t *testing.T, serve func(net.Conn)) string {
	t.Helper()
	listener, e := net.Listen("tcp", "127.0.0.1:0")
	if e != nil {
		t.Fatal(e)
	}
	t.Cleanup(func() { listener.Close() })
	go func() {
		connection, e := listener.Accept()
		if e == nil {
			defer connection.Close()
			connection.SetDeadline(time.Now().Add(3 * time.Second))
			serve(connection)
		}
	}()
	return listener.Addr().String()
}
func TestHTTPProxyCONNECTPreservesBufferedSSHBytesAndAuthenticates(t *testing.T) {
	checked := make(chan bool, 1)
	address := proxyFixture(t, func(c net.Conn) {
		r, e := http.ReadRequest(bufio.NewReader(c))
		if e != nil {
			checked <- false
			return
		}
		checked <- r.Method == "CONNECT" && r.Host == "remote.invalid:22" && r.Header.Get("Proxy-Authorization") == "Basic Zml4dHVyZTpwYXNzd29yZA=="
		io.WriteString(c, "HTTP/1.1 200 Connection established\r\n\r\nSSH-2.0-fixture\r\n")
	})
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	connection, e := Dial(ctx, "remote.invalid:22", connectcfg.Proxy{Mode: "http", Address: address, User: "fixture", Password: "password"})
	if e != nil {
		t.Fatal(e)
	}
	defer connection.Close()
	if !<-checked {
		t.Fatal("CONNECT target or explicit proxy authentication changed")
	}
	data, e := io.ReadAll(connection)
	if e != nil || string(data) != "SSH-2.0-fixture\r\n" {
		t.Fatal(string(data), e)
	}
}
func TestSOCKS5ProxyKeepsDNSRemoteAndUsesSelectedAuthentication(t *testing.T) {
	checked := make(chan bool, 1)
	address := proxyFixture(t, func(c net.Conn) {
		var hello [3]byte
		if _, e := io.ReadFull(c, hello[:]); e != nil {
			return
		}
		if hello != [3]byte{5, 1, 2} {
			return
		}
		c.Write([]byte{5, 2})
		login := make([]byte, 1+1+7+1+8)
		if _, e := io.ReadFull(c, login); e != nil {
			return
		}
		c.Write([]byte{1, 0})
		var command [3]byte
		io.ReadFull(c, command[:])
		target, e := ReadAddress(c)
		checked <- e == nil && target == "remote.invalid:22" && bytes.Equal(login, append(append([]byte{1, 7}, []byte("fixture")...), append([]byte{8}, []byte("password")...)...))
		c.Write([]byte{5, 0, 0, 1, 0, 0, 0, 0, 0, 0})
		io.WriteString(c, "proxied")
	})
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	connection, e := Dial(ctx, "remote.invalid:22", connectcfg.Proxy{Mode: "socks5", Address: address, User: "fixture", Password: "password"})
	if e != nil {
		t.Fatal(e)
	}
	defer connection.Close()
	if !<-checked {
		t.Fatal("SOCKS5 resolved locally or lost its configured credentials")
	}
	data, e := io.ReadAll(connection)
	if e != nil || string(data) != "proxied" {
		t.Fatal(string(data), e)
	}
}
func TestProxyCancellationAndExplicitDirectIgnoreEnvironment(t *testing.T) {
	address := proxyFixture(t, func(c net.Conn) { io.Copy(io.Discard, c) })
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	began := time.Now()
	if c, e := Dial(ctx, "remote.invalid:22", connectcfg.Proxy{Mode: "http", Address: address}); e == nil {
		c.Close()
		t.Fatal("stalled CONNECT succeeded")
	}
	if time.Since(began) > time.Second {
		t.Fatal("CONNECT ignored context cancellation")
	}
	t.Setenv("ALL_PROXY", "socks5://127.0.0.1:1")
	t.Setenv("HTTP_PROXY", "http://127.0.0.1:1")
	echo := proxyFixture(t, func(c net.Conn) { io.WriteString(c, "direct") })
	c, e := Dial(context.Background(), echo, connectcfg.Proxy{Mode: "none"})
	if e != nil {
		t.Fatal("explicit direct used an environment proxy", e)
	}
	defer c.Close()
	b, e := io.ReadAll(c)
	if e != nil || string(b) != "direct" {
		t.Fatal(e)
	}
}
func TestSOCKSAddressDomainLengthsAreNotMistakenForIP(t *testing.T) {
	for _, host := range []string{"abcd", "abcdefghijklmnop", "127.0.0.1", "::1"} {
		original := net.JoinHostPort(host, "22")
		encoded, e := EncodeAddress(original)
		if e != nil {
			t.Fatal(e)
		}
		actual, e := ReadAddress(bytes.NewReader(encoded))
		if e != nil || actual != original {
			t.Fatal(original, actual, e)
		}
	}
}

func TestHTTPSProxyRejectsUntrustedCertificatesBeforeCONNECT(t *testing.T) {
	var contacted atomic.Bool
	proxy := httptest.NewUnstartedServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { contacted.Store(true) }))
	proxy.Config.ErrorLog = log.New(io.Discard, "", 0)
	proxy.StartTLS()
	defer proxy.Close()
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	c, e := Dial(ctx, "remote.invalid:22", connectcfg.Proxy{Mode: "https", Address: proxy.Listener.Addr().String(), User: "fixture", Password: "synthetic-secret"})
	if e == nil {
		c.Close()
		t.Fatal("untrusted HTTPS proxy certificate accepted")
	}
	if contacted.Load() {
		t.Fatal("CONNECT or proxy credentials sent before TLS identity validation")
	}
}
