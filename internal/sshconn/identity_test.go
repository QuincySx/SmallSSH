package sshconn_test

import (
	"bufio"
	"context"
	"encoding/pem"
	"errors"
	"io"
	"net"
	"strings"
	"sync/atomic"
	"syscall"
	"testing"
	"time"

	"golang.org/x/crypto/ssh"
	"sshclient/internal/sshconn"
	"sshclient/internal/testserver"
)

func TestHostIdentityProbeNeverOffersCredentials(t *testing.T) {
	f := testserver.New(t)
	p := f.Profile()
	for _, host := range []string{p.Host, "localhost"} {
		t.Run(host, func(t *testing.T) {
			identity, err := sshconn.ProbeHost(context.Background(), host, p.Port)
			if err != nil || identity.Host != host || identity.Fingerprint != p.Fingerprint || identity.Algorithm == "" {
				t.Fatal(identity, err)
			}
		})
	}
	if f.Auths.Load() != 0 {
		t.Fatal("probe offered an authentication identity")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	start := time.Now()
	if _, err := sshconn.ProbeHost(ctx, p.Host, p.Port); !errors.Is(err, context.Canceled) || time.Since(start) > time.Second {
		t.Fatal("cancelled probe proceeded", err)
	}
}

func TestHostIdentityProbePreservesDialError(t *testing.T) {
	listener, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	addr := listener.Addr().(*net.TCPAddr)
	if err := listener.Close(); err != nil {
		t.Fatal(err)
	}
	identity, err := sshconn.ProbeHost(context.Background(), addr.IP.String(), addr.Port)
	var opErr *net.OpError
	if !errors.Is(err, syscall.ECONNREFUSED) || !errors.As(err, &opErr) || opErr.Op != "dial" {
		t.Fatalf("dial error chain lost: %v", err)
	}
	if !strings.Contains(err.Error(), addr.String()) || !strings.Contains(err.Error(), "TCP 连接") {
		t.Fatalf("dial error has no target or phase: %v", err)
	}
	if identity != (sshconn.HostIdentity{}) {
		t.Fatalf("failed probe returned an identity: %+v", identity)
	}
}

func TestHostIdentityProbePreservesHandshakeError(t *testing.T) {
	for _, tc := range []struct {
		name    string
		payload string
		want    string
		cause   error
	}{
		{name: "interrupted", want: "EOF", cause: io.EOF},
		{name: "non-SSH", payload: strings.Repeat("H", 256), want: "overflow reading version string"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			listener, err := net.Listen("tcp4", "127.0.0.1:0")
			if err != nil {
				t.Fatal(err)
			}
			serveProbeOnce(t, listener, func(raw net.Conn) error {
				if _, err := bufio.NewReader(raw).ReadString('\n'); err != nil {
					return err
				}
				_, err := io.WriteString(raw, tc.payload)
				return err
			})
			addr := listener.Addr().(*net.TCPAddr)
			identity, err := sshconn.ProbeHost(context.Background(), addr.IP.String(), addr.Port)
			if err == nil || !strings.Contains(err.Error(), tc.want) || (tc.cause != nil && !errors.Is(err, tc.cause)) {
				t.Fatalf("handshake error cause lost: %v", err)
			}
			if !strings.Contains(err.Error(), addr.String()) || !strings.Contains(err.Error(), "SSH 握手") || errors.Unwrap(err) == nil {
				t.Fatalf("handshake error has no target, phase, or error chain: %v", err)
			}
			if identity != (sshconn.HostIdentity{}) {
				t.Fatalf("failed probe returned an identity: %+v", identity)
			}
		})
	}
}

func TestHostIdentityProbeCancelsStalledHandshake(t *testing.T) {
	listener, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	accepted := make(chan struct{})
	serveProbeOnce(t, listener, func(raw net.Conn) error {
		if _, err := bufio.NewReader(raw).ReadString('\n'); err != nil {
			return err
		}
		close(accepted)
		_, err := io.Copy(io.Discard, raw)
		return err
	})
	addr := listener.Addr().(*net.TCPAddr)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	result := make(chan error, 1)
	go func() {
		_, err := sshconn.ProbeHost(ctx, addr.IP.String(), addr.Port)
		result <- err
	}()
	select {
	case <-accepted:
	case <-time.After(time.Second):
		t.Fatal("probe did not connect to the isolated listener")
	}
	cancel()
	select {
	case err := <-result:
		if !errors.Is(err, context.Canceled) || !strings.Contains(err.Error(), addr.String()) || !strings.Contains(err.Error(), "SSH 握手") {
			t.Fatalf("cancelled handshake error lost context or target: %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("cancelled probe remained in the handshake")
	}
}

func TestHostIdentityProbeAllowsIPv6Loopback(t *testing.T) {
	listener, err := net.Listen("tcp6", "[::1]:0")
	if err != nil {
		t.Skipf("IPv6 loopback unavailable: %v", err)
	}
	f := testserver.New(t)
	var auths atomic.Int32
	config := &ssh.ServerConfig{NoClientAuth: true, NoClientAuthCallback: func(ssh.ConnMetadata) (*ssh.Permissions, error) {
		auths.Add(1)
		return nil, nil
	}}
	config.AddHostKey(f.Host)
	serveProbeOnce(t, listener, func(raw net.Conn) error {
		_, _, _, _ = ssh.NewServerConn(raw, config)
		return nil
	})
	addr := listener.Addr().(*net.TCPAddr)
	identity, err := sshconn.ProbeHost(context.Background(), addr.IP.String(), addr.Port)
	if err != nil || identity.Host != "::1" || identity.Fingerprint != f.Profile().Fingerprint {
		t.Fatal(identity, err)
	}
	if auths.Load() != 0 {
		t.Fatal("probe proceeded to authentication")
	}
}

func serveProbeOnce(t *testing.T, listener net.Listener, serve func(net.Conn) error) {
	t.Helper()
	done := make(chan error, 1)
	go func() {
		raw, err := listener.Accept()
		if err != nil {
			done <- err
			return
		}
		defer raw.Close()
		raw.SetDeadline(time.Now().Add(3 * time.Second))
		done <- serve(raw)
	}()
	t.Cleanup(func() {
		listener.Close()
		if err := <-done; err != nil && !errors.Is(err, net.ErrClosed) {
			t.Errorf("isolated probe listener failed: %v", err)
		}
	})
}

func TestPrivateKeyImportChecksEncryptionAndRejectsPublicKey(t *testing.T) {
	f := testserver.New(t)
	info, err := sshconn.InspectPrivateKey(f.PrivateKey, "")
	if err != nil || info.Fingerprint != ssh.FingerprintSHA256(f.Key.PublicKey()) {
		t.Fatal(info, err)
	}
	raw, err := ssh.ParseRawPrivateKey([]byte(f.PrivateKey))
	if err != nil {
		t.Fatal(err)
	}
	block, err := ssh.MarshalPrivateKeyWithPassphrase(raw, "isolated test", []byte("key-only-test-pass"))
	if err != nil {
		t.Fatal(err)
	}
	encrypted := string(pem.EncodeToMemory(block))
	if _, err = sshconn.InspectPrivateKey(encrypted, ""); err == nil || !strings.Contains(err.Error(), "口令") {
		t.Fatal(err)
	}
	if _, err = sshconn.InspectPrivateKey(encrypted, "wrong"); err == nil {
		t.Fatal("wrong passphrase accepted")
	}
	if _, err = sshconn.InspectPrivateKey(encrypted, "key-only-test-pass"); err != nil {
		t.Fatal(err)
	}
	if _, err = sshconn.InspectPrivateKey(string(ssh.MarshalAuthorizedKey(f.Key.PublicKey())), ""); err == nil || !strings.Contains(err.Error(), "公钥") {
		t.Fatal(err)
	}
}
