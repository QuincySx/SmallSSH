package client

import (
	"bytes"
	"context"
	"crypto/tls"
	"errors"
	"net"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"sshclient/internal/localipc"
	"sshclient/internal/protocol"
)

const syntheticPrepareTimeout = 5 * time.Second

type syntheticPrepareSource struct {
	unlockCalls atomic.Int32
	proofCalls  atomic.Int32
	unlockErr   error
	proofErr    error
	mu          sync.Mutex
	unlock      []byte
	proof       []byte
}

var _ UnlockSource = (*syntheticPrepareSource)(nil)

func (s *syntheticPrepareSource) UnlockKey() ([]byte, error) {
	s.unlockCalls.Add(1)
	key := make([]byte, 32)
	copy(key, bytes.Repeat([]byte{0x5a}, 32))
	s.mu.Lock()
	s.unlock = key
	s.mu.Unlock()
	return key, s.unlockErr
}

func (s *syntheticPrepareSource) Proof([]byte) ([]byte, error) {
	s.proofCalls.Add(1)
	proof := syntheticPreparePublicBytes()
	s.mu.Lock()
	s.proof = proof
	s.mu.Unlock()
	return proof, s.proofErr
}

func (s *syntheticPrepareSource) calls() (int32, int32) {
	return s.unlockCalls.Load(), s.proofCalls.Load()
}

func (s *syntheticPrepareSource) buffersCleared() (bool, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return syntheticPrepareBytesCleared(s.unlock), syntheticPrepareBytesCleared(s.proof)
}

type syntheticPrepareEndpoint struct {
	sdk           ServiceClient
	listener      net.Listener
	configuration *tls.Config
	release       func()
	result        chan error
	done          chan struct{}
	cleanupOnce   sync.Once
	mu            sync.Mutex
	raw           net.Conn
}

func newSyntheticPrepareEndpoint(t *testing.T, handler func(*localipc.Wire) error) *syntheticPrepareEndpoint {
	t.Helper()
	dir := b03bClientTestDir(t)
	serviceDir := filepath.Join(dir, "service")
	release, err := localipc.Acquire(serviceDir)
	if err != nil {
		t.Fatal(err)
	}
	listener, err := localipc.Listen(serviceDir)
	if err != nil {
		release()
		t.Fatal(err)
	}
	configuration, _, err := localipc.ServerTLS()
	if err != nil {
		_ = listener.Close()
		release()
		t.Fatal(err)
	}
	executable, err := os.Executable()
	if err != nil {
		localipc.DestroyServerTLS(configuration)
		_ = listener.Close()
		release()
		t.Fatal(err)
	}
	endpoint := &syntheticPrepareEndpoint{
		sdk:           ServiceClient{Dir: dir, Executable: executable},
		listener:      listener,
		configuration: configuration,
		release:       release,
		result:        make(chan error, 1),
		done:          make(chan struct{}),
	}
	t.Cleanup(endpoint.abort)
	go endpoint.serve(handler)
	return endpoint
}

func (e *syntheticPrepareEndpoint) serve(handler func(*localipc.Wire) error) {
	e.result <- e.handle(handler)
	close(e.done)
}

func (e *syntheticPrepareEndpoint) handle(handler func(*localipc.Wire) error) error {
	acceptTimer := time.AfterFunc(syntheticPrepareTimeout, func() { _ = e.listener.Close() })
	defer acceptTimer.Stop()
	raw, err := e.listener.Accept()
	if err != nil {
		return err
	}
	e.mu.Lock()
	e.raw = raw
	e.mu.Unlock()
	defer func() {
		e.mu.Lock()
		if e.raw == raw {
			e.raw = nil
		}
		e.mu.Unlock()
	}()
	defer raw.Close()
	identity, err := localipc.Identify(raw)
	if err != nil {
		return err
	}
	defer identity.Close()
	if err := identity.Verify(); err != nil {
		return err
	}
	if err := raw.SetDeadline(time.Now().Add(syntheticPrepareTimeout)); err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), syntheticPrepareTimeout)
	defer cancel()
	connection := tls.Server(raw, e.configuration)
	defer connection.Close()
	if err := connection.HandshakeContext(ctx); err != nil {
		return err
	}
	wire := &localipc.Wire{Conn: connection, WriteTimeout: syntheticPrepareTimeout}
	return handler(wire)
}

func (e *syntheticPrepareEndpoint) wait(t *testing.T) {
	t.Helper()
	select {
	case <-e.done:
	case <-time.After(syntheticPrepareTimeout):
		t.Fatal("synthetic IPC endpoint did not finish within 5 seconds")
	}
	if err := <-e.result; err != nil {
		t.Fatalf("synthetic IPC endpoint failed: %v", err)
	}
}

func (e *syntheticPrepareEndpoint) abort() {
	e.cleanupOnce.Do(func() {
		_ = e.listener.Close()
		e.mu.Lock()
		raw := e.raw
		e.mu.Unlock()
		if raw != nil {
			_ = raw.Close()
		}
		select {
		case <-e.done:
		case <-time.After(syntheticPrepareTimeout):
		}
		localipc.DestroyServerTLS(e.configuration)
		e.release()
	})
}

func syntheticPrepareContext(t *testing.T) context.Context {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), syntheticPrepareTimeout)
	t.Cleanup(cancel)
	return ctx
}

func syntheticPrepareSend(wire *localipc.Wire, message any) error {
	if err := wire.Conn.SetDeadline(time.Now().Add(syntheticPrepareTimeout)); err != nil {
		return err
	}
	return wire.Send(message)
}

func syntheticPrepareReceive(wire *localipc.Wire, message any) error {
	if err := wire.Conn.SetDeadline(time.Now().Add(syntheticPrepareTimeout)); err != nil {
		return err
	}
	return wire.Receive(message)
}

func syntheticPrepareHello() protocol.ServiceMessage {
	return protocol.ServiceMessage{
		Action:     "hello",
		Major:      protocol.ServiceProtocolMajor,
		Minor:      protocol.ServiceProtocolMinor,
		Epoch:      "0123456789abcdef0123456789abcdef",
		Challenge:  "fedcba9876543210fedcba9876543210",
		Generation: 1,
	}
}

func syntheticPreparePrepared(hello protocol.ServiceMessage, id string) protocol.ServiceMessage {
	return protocol.ServiceMessage{
		Action:     "ownerPrepared",
		Major:      protocol.ServiceProtocolMajor,
		Minor:      hello.Minor,
		Epoch:      hello.Epoch,
		Generation: hello.Generation,
		ID:         id,
	}
}

func syntheticPreparePublicBytes() []byte {
	return append([]byte(nil), []byte("public synthetic protocol placeholder")...)
}

func syntheticPrepareBytesCleared(value []byte) bool {
	if value == nil {
		return false
	}
	for _, character := range value {
		if character != 0 {
			return false
		}
	}
	return true
}

func TestServicePrepareClearsSyntheticPartialResults(t *testing.T) {
	t.Run("unlock key error", func(t *testing.T) {
		endpoint := newSyntheticPrepareEndpoint(t, func(wire *localipc.Wire) error {
			var hello protocol.ServiceMessage
			if err := syntheticPrepareReceive(wire, &hello); err != nil {
				return err
			}
			if hello.Action != "hello" {
				return errors.New("synthetic endpoint did not receive hello")
			}
			return syntheticPrepareSend(wire, syntheticPrepareHello())
		})
		source := &syntheticPrepareSource{unlockErr: errors.New("synthetic placeholder failure")}

		prepared, err := endpoint.sdk.PrepareOwner(syntheticPrepareContext(t), source, 0)
		if prepared != nil {
			prepared.Close()
			t.Fatal("PrepareOwner returned a prepared owner after an unlock source error")
		}
		if err == nil {
			t.Fatal("PrepareOwner accepted an unlock source error")
		}
		endpoint.wait(t)
		unlockCalls, proofCalls := source.calls()
		if unlockCalls != 1 || proofCalls != 0 {
			t.Fatalf("source methods called: UnlockKey=%d Proof=%d", unlockCalls, proofCalls)
		}
		unlockCleared, _ := source.buffersCleared()
		if !unlockCleared {
			t.Fatal("synthetic unlock buffer was not cleared")
		}
	})

	t.Run("proof error", func(t *testing.T) {
		endpoint := newSyntheticPrepareEndpoint(t, func(wire *localipc.Wire) error {
			var hello protocol.ServiceMessage
			if err := syntheticPrepareReceive(wire, &hello); err != nil {
				return err
			}
			if hello.Action != "hello" {
				return errors.New("synthetic endpoint did not receive hello")
			}
			return syntheticPrepareSend(wire, syntheticPrepareHello())
		})
		source := &syntheticPrepareSource{proofErr: errors.New("synthetic placeholder failure")}

		prepared, err := endpoint.sdk.PrepareOwner(syntheticPrepareContext(t), source, 0)
		if prepared != nil {
			prepared.Close()
			t.Fatal("PrepareOwner returned a prepared owner after a proof source error")
		}
		if err == nil {
			t.Fatal("PrepareOwner accepted a proof source error")
		}
		endpoint.wait(t)
		unlockCalls, proofCalls := source.calls()
		if unlockCalls != 1 || proofCalls != 1 {
			t.Fatalf("source methods called: UnlockKey=%d Proof=%d", unlockCalls, proofCalls)
		}
		unlockCleared, proofCleared := source.buffersCleared()
		if !unlockCleared || !proofCleared {
			t.Fatal("synthetic key or proof buffer was not cleared")
		}
	})
}

func TestServicePrepareRejectsInjectedHelloFields(t *testing.T) {
	for _, test := range []struct {
		name   string
		inject func(*protocol.ServiceMessage)
	}{
		{
			name: "unlock key",
			inject: func(hello *protocol.ServiceMessage) {
				hello.UnlockKey = syntheticPreparePublicBytes()
			},
		},
		{
			name: "proof",
			inject: func(hello *protocol.ServiceMessage) {
				hello.Proof = syntheticPreparePublicBytes()
			},
		},
	} {
		test := test
		t.Run(test.name, func(t *testing.T) {
			var helloReceived atomic.Bool
			endpoint := newSyntheticPrepareEndpoint(t, func(wire *localipc.Wire) error {
				var request protocol.ServiceMessage
				if err := syntheticPrepareReceive(wire, &request); err != nil {
					return err
				}
				if request.Action != "hello" {
					return errors.New("synthetic endpoint did not receive hello")
				}
				helloReceived.Store(true)
				hello := syntheticPrepareHello()
				test.inject(&hello)
				return syntheticPrepareSend(wire, hello)
			})
			source := &syntheticPrepareSource{}

			prepared, err := endpoint.sdk.PrepareOwner(syntheticPrepareContext(t), source, 0)
			if prepared != nil {
				prepared.Close()
				t.Fatal("PrepareOwner returned a prepared owner for an injected hello")
			}
			if err == nil {
				t.Fatal("PrepareOwner accepted an injected hello")
			}
			endpoint.wait(t)
			if !helloReceived.Load() {
				t.Fatal("synthetic endpoint did not complete hello exchange")
			}
			unlockCalls, proofCalls := source.calls()
			if unlockCalls != 0 || proofCalls != 0 {
				t.Fatalf("source methods called: UnlockKey=%d Proof=%d", unlockCalls, proofCalls)
			}
		})
	}

	t.Run("wrong field type", func(t *testing.T) {
		var helloReceived atomic.Bool
		endpoint := newSyntheticPrepareEndpoint(t, func(wire *localipc.Wire) error {
			var request protocol.ServiceMessage
			if err := syntheticPrepareReceive(wire, &request); err != nil {
				return err
			}
			if request.Action != "hello" {
				return errors.New("synthetic endpoint did not receive hello")
			}
			helloReceived.Store(true)
			return syntheticPrepareSend(wire, map[string]any{
				"action":     "hello",
				"major":      "invalid",
				"minor":      protocol.ServiceProtocolMinor,
				"epoch":      "0123456789abcdef0123456789abcdef",
				"challenge":  "fedcba9876543210fedcba9876543210",
				"generation": uint64(1),
			})
		})
		source := &syntheticPrepareSource{}

		prepared, err := endpoint.sdk.PrepareOwner(syntheticPrepareContext(t), source, 0)
		if prepared != nil {
			prepared.Close()
			t.Fatal("PrepareOwner returned a prepared owner for malformed hello JSON")
		}
		if err == nil {
			t.Fatal("PrepareOwner accepted malformed hello JSON")
		}
		endpoint.wait(t)
		if !helloReceived.Load() {
			t.Fatal("synthetic endpoint did not complete hello exchange")
		}
		unlockCalls, proofCalls := source.calls()
		if unlockCalls != 0 || proofCalls != 0 {
			t.Fatalf("source methods called: UnlockKey=%d Proof=%d", unlockCalls, proofCalls)
		}
	})
}

func TestServicePrepareRejectsInjectedPreparedFields(t *testing.T) {
	const preparedID = "00112233445566778899aabbccddeeff"
	for _, test := range []struct {
		name   string
		inject func(*protocol.ServiceMessage)
	}{
		{
			name: "unlock key",
			inject: func(prepared *protocol.ServiceMessage) {
				prepared.UnlockKey = syntheticPreparePublicBytes()
			},
		},
		{
			name: "proof",
			inject: func(prepared *protocol.ServiceMessage) {
				prepared.Proof = syntheticPreparePublicBytes()
			},
		},
	} {
		test := test
		t.Run(test.name, func(t *testing.T) {
			var prepareReceived atomic.Bool
			hello := syntheticPrepareHello()
			endpoint := newSyntheticPrepareEndpoint(t, func(wire *localipc.Wire) error {
				var request protocol.ServiceMessage
				if err := syntheticPrepareReceive(wire, &request); err != nil {
					return err
				}
				if request.Action != "hello" {
					return errors.New("synthetic endpoint did not receive hello")
				}
				if err := syntheticPrepareSend(wire, hello); err != nil {
					return err
				}
				var prepare protocol.ServiceMessage
				if err := syntheticPrepareReceive(wire, &prepare); err != nil {
					return err
				}
				if prepare.Action != "prepareOwner" ||
					prepare.Major != protocol.ServiceProtocolMajor ||
					prepare.Minor != hello.Minor ||
					prepare.Epoch != hello.Epoch ||
					prepare.Challenge != hello.Challenge ||
					prepare.Generation != hello.Generation ||
					prepare.OwnerPrepare == nil ||
					len(prepare.UnlockKey) == 0 ||
					len(prepare.Proof) == 0 {
					return errors.New("synthetic endpoint did not receive a complete prepare frame")
				}
				prepareReceived.Store(true)
				clear(prepare.UnlockKey)
				prepare.UnlockKey = nil
				clear(prepare.Proof)
				prepare.Proof = nil
				response := syntheticPreparePrepared(hello, preparedID)
				test.inject(&response)
				return syntheticPrepareSend(wire, response)
			})
			source := &syntheticPrepareSource{}

			prepared, err := endpoint.sdk.PrepareOwner(syntheticPrepareContext(t), source, 0)
			if prepared != nil {
				prepared.Close()
				t.Fatal("PrepareOwner returned a live prepared owner for injected prepared fields")
			}
			if err == nil {
				t.Fatal("PrepareOwner accepted injected prepared fields")
			}
			endpoint.wait(t)
			if !prepareReceived.Load() {
				t.Fatal("synthetic endpoint did not complete prepare frame exchange")
			}
			unlockCalls, proofCalls := source.calls()
			if unlockCalls != 1 || proofCalls != 1 {
				t.Fatalf("source methods called: UnlockKey=%d Proof=%d", unlockCalls, proofCalls)
			}
		})
	}
}
