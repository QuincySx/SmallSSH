package localipc

import (
	"context"
	"crypto/tls"
	"net"
	"os"
	"testing"
	"time"
)

func TestPinnedTransportAndKernelProcessInstance(t *testing.T) {
	dir, err := os.MkdirTemp("", "ipc-auth-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(dir) })
	listener, err := Listen(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	configuration, pin, err := ServerTLS()
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	type accepted struct {
		identity *ProcessIdentity
		binding  string
		err      error
	}
	results := make(chan accepted, 3)
	go func() {
		for range 3 {
			raw, err := listener.Accept()
			if err != nil {
				results <- accepted{err: err}
				return
			}
			identity, err := Identify(raw)
			if err != nil {
				raw.Close()
				results <- accepted{err: err}
				continue
			}
			connection := tls.Server(raw, configuration)
			connection.SetDeadline(time.Now().Add(5 * time.Second))
			if err := connection.HandshakeContext(ctx); err != nil {
				raw.Close()
				identity.Close()
				results <- accepted{err: err}
				continue
			}
			binding, err := ChannelBinding(connection)
			results <- accepted{identity: identity, binding: binding, err: err}
			connection.Close()
		}
	}()
	var previous *ProcessIdentity
	for i, key := range []string{pin, "", "sha256:0000000000000000000000000000000000000000000000000000000000000000"} {
		wire, err := ConnectVerified(ctx, dir, key)
		if i == 2 {
			if err == nil {
				wire.Conn.Close()
				t.Fatal("accepted an unpinned broker key")
			}
			<-results
			continue
		}
		if err != nil {
			t.Fatal(err)
		}
		binding, err := ChannelBinding(wire.Conn)
		wire.Conn.Close()
		got := <-results
		if got.err != nil {
			t.Fatal(got.err)
		}
		defer got.identity.Close()
		if err != nil || binding != got.binding || got.identity.PID != os.Getpid() || got.identity.Verify() != nil {
			t.Fatal("transport or process identity was not verified", err, got)
		}
		if previous != nil && !previous.Same(got.identity) {
			t.Fatal("same live process changed identity across connections")
		}
		previous = got.identity
	}
}

func TestUnverifiableTransportIdentityFailsClosed(t *testing.T) {
	a, b := net.Pipe()
	defer a.Close()
	defer b.Close()
	if identity, err := Identify(a); err == nil || identity != nil {
		t.Fatal("accepted a transport without kernel identity")
	}
	if _, err := ChannelBinding(a); err == nil {
		t.Fatal("accepted a plaintext channel binding")
	}
}
