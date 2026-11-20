package localipc

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/tls"
	"errors"
	"os"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestPinnedTransportRenewsAfterThirtyDaysWithoutChangingIdentity(t *testing.T) {
	var clock atomic.Int64
	clock.Store(time.Now().Add(-31 * 24 * time.Hour).UnixNano())
	configuration, pin, err := serverTLS(func() time.Time { return time.Unix(0, clock.Load()) })
	if err != nil {
		t.Fatal(err)
	}
	defer DestroyServerTLS(configuration)
	original := configuration.Certificates[0].Leaf
	if !original.NotAfter.Before(time.Now()) {
		t.Fatal("fixture certificate has not expired")
	}
	clock.Store(time.Now().UnixNano())
	dir, results := serveRenewalTestConnections(t, configuration, 2)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	wire, err := ConnectVerified(ctx, dir, pin)
	if err != nil {
		t.Fatal("a daemon older than 30 days refused a fresh pinned connection", err)
	}
	renewed := wire.Conn.(*tls.Conn).ConnectionState().PeerCertificates[0]
	if renewed.SerialNumber.Cmp(original.SerialNumber) == 0 || publicKeyPin(renewed) != pin {
		t.Fatal("renewal did not replace the certificate while preserving its identity")
	}
	if err := <-results; err != nil {
		t.Fatal(err)
	}
	wire.Close()
	wrong := "sha256:0000000000000000000000000000000000000000000000000000000000000000"
	if untrusted, err := ConnectVerified(ctx, dir, wrong); err == nil {
		untrusted.Close()
		t.Fatal("renewal accepted an untrusted broker pin")
	}
	<-results
}

func TestExpiredPinnedTransportStillFailsClosed(t *testing.T) {
	configuration, pin, err := serverTLS(func() time.Time { return time.Now().Add(-31 * 24 * time.Hour) })
	if err != nil {
		t.Fatal(err)
	}
	defer DestroyServerTLS(configuration)
	// Simulate an old implementation that cannot renew. Clients must continue
	// checking validity rather than treating a matching pin as an expiry bypass.
	configuration.GetConfigForClient = nil
	dir, results := serveRenewalTestConnections(t, configuration, 1)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if wire, err := ConnectVerified(ctx, dir, pin); !errors.Is(err, ErrBrokerIdentity) {
		if wire != nil {
			wire.Close()
		}
		t.Fatal("expired pinned certificate was not rejected", err)
	}
	<-results
}

func TestConcurrentCertificateRenewalKeepsOwnedKeyAndFailsClosed(t *testing.T) {
	_, private, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	originalKey := append([]byte(nil), private...)
	var clock atomic.Int64
	clock.Store(time.Now().Add(-31 * 24 * time.Hour).UnixNano())
	configuration, pin, err := serverTLS(func() time.Time { return time.Unix(0, clock.Load()) }, private)
	if err != nil {
		t.Fatal(err)
	}
	clock.Store(time.Now().UnixNano())
	var wg sync.WaitGroup
	serials := make(chan string, 24)
	for range 24 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			connection, err := configuration.GetConfigForClient(&tls.ClientHelloInfo{})
			if err != nil {
				t.Error(err)
				return
			}
			leaf := connection.Certificates[0].Leaf
			if publicKeyPin(leaf) != pin {
				t.Error("concurrent renewal changed the identity")
			}
			serials <- leaf.SerialNumber.String()
		}()
	}
	wg.Wait()
	close(serials)
	serial := ""
	for next := range serials {
		if serial != "" && serial != next {
			t.Fatal("concurrent handshakes issued redundant certificates")
		}
		serial = next
	}
	DestroyServerTLS(configuration)
	if !bytes.Equal(private, originalKey) {
		t.Fatal("destroying the server cleared its caller's private key")
	}
	owned := configuration.Certificates[0].PrivateKey.(ed25519.PrivateKey)
	if !bytes.Equal(owned, make([]byte, ed25519.PrivateKeySize)) {
		t.Fatal("destroy did not clear the owned signing key")
	}
}

func TestCertificateRenewalFailureDoesNotFallBackToExpiredCertificate(t *testing.T) {
	current := time.Now()
	configuration, _, err := serverTLS(func() time.Time { return current })
	if err != nil {
		t.Fatal(err)
	}
	defer DestroyServerTLS(configuration)
	// x509 refuses certificate validity extending beyond year 9999.
	current = time.Date(9999, 12, 31, 0, 0, 0, 0, time.UTC)
	if connection, err := configuration.GetConfigForClient(&tls.ClientHelloInfo{}); err == nil || connection != nil {
		t.Fatal("renewal failure served a fallback certificate", err)
	}
}

func serveRenewalTestConnections(t *testing.T, configuration *tls.Config, count int) (string, <-chan error) {
	t.Helper()
	dir, err := os.MkdirTemp("", "tls-renew-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(dir) })
	listener, err := Listen(dir)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { listener.Close() })
	results := make(chan error, count)
	go func() {
		for range count {
			raw, err := listener.Accept()
			if err != nil {
				results <- err
				return
			}
			connection := tls.Server(raw, configuration)
			connection.SetDeadline(time.Now().Add(5 * time.Second))
			results <- connection.HandshakeContext(context.Background())
			raw.Close()
		}
	}()
	return dir, results
}
