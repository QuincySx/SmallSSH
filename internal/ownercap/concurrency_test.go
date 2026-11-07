package ownercap

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestRegistryConcurrentVerificationRevocationAndClose(t *testing.T) {
	pair := newIdentityPair(t)
	callbacks := make(chan string, maxSessions)
	var callbackCount atomic.Int64
	var callbackDone sync.WaitGroup
	var registry *Registry
	registry, err := NewRegistry(testEpoch, func(id string) {
		callbackCount.Add(1)
		callbackDone.Add(1)
		defer callbackDone.Done()
		registry.RevokeAll()
		registry.Sweep()
		callbacks <- id
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(registry.Close)
	type registered struct {
		id      string
		proof   []byte
		payload []byte
		cancel  context.CancelFunc
	}
	var sessions []registered
	registeredIDs := make(map[string]bool)
	for range 4 {
		control, cancel := context.WithCancel(pair.ctx)
		signer, err := NewSigner()
		if err != nil {
			cancel()
			t.Fatal(err)
		}
		t.Cleanup(signer.Close)
		info, err := registry.Register(control, pair.caller, signer.PublicKey(), time.Hour)
		if err != nil {
			cancel()
			t.Fatal(err)
		}
		if err := signer.Bind(info); err != nil {
			cancel()
			t.Fatal(err)
		}
		payload := []byte("payload-" + info.ID)
		proof, err := signer.Proof(payload)
		if err != nil {
			cancel()
			t.Fatal(err)
		}
		sessions = append(sessions, registered{id: info.ID, proof: proof, payload: payload, cancel: cancel})
		registeredIDs[info.ID] = true
	}
	start := make(chan struct{})
	var workers sync.WaitGroup
	run := func(work func()) {
		workers.Add(1)
		go func() {
			defer workers.Done()
			<-start
			work()
		}()
	}
	for _, session := range sessions {
		session := session
		run(session.cancel)
		run(func() { registry.Revoke(session.id) })
		run(func() {
			for range 20 {
				_, _ = registry.Verify(pair.caller, session.payload, session.proof)
			}
		})
	}
	run(registry.RevokeAll)
	run(registry.Sweep)
	run(registry.Close)
	close(start)
	finished := make(chan struct{})
	go func() {
		workers.Wait()
		close(finished)
	}()
	select {
	case <-finished:
	case <-time.After(5 * time.Second):
		t.Fatal("concurrent registry operations deadlocked")
	}
	revoked := make(map[string]bool)
	for range len(sessions) {
		revoked[receiveWithin(t, callbacks, "concurrent revoke callback")] = true
	}
	callbackDone.Wait()
	if got := callbackCount.Load(); got != int64(len(sessions)) {
		t.Fatalf("revoke callback count = %d, want %d", got, len(sessions))
	}
	if len(revoked) != len(registeredIDs) {
		t.Fatalf("revoke callbacks = %#v, registered = %#v", revoked, registeredIDs)
	}
	for id := range registeredIDs {
		if !revoked[id] {
			t.Fatalf("missing revoke callback for %q", id)
		}
	}
	registry.RevokeAll()
	registry.Sweep()
	registry.Close()
	select {
	case <-callbacks:
		t.Fatal("registry emitted a duplicate revoke callback")
	default:
	}
	for _, session := range sessions {
		if _, err := registry.Verify(pair.caller, session.payload, session.proof); err == nil {
			t.Fatal("closed registry accepted a revoked proof")
		}
	}
}
