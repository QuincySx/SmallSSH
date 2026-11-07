package broker

import (
	"context"
	"io"
	"sync/atomic"
	"testing"
	"time"

	"sshclient/internal/sshconn"
	"sshclient/internal/testserver"
)

func TestPoolCheckoutCancellationClosesStalledSSHSession(t *testing.T) {
	for _, operation := range []string{"exec", "sftp"} {
		t.Run(operation, func(t *testing.T) {
			var opens atomic.Int32
			blocked := make(chan struct{})
			fixture := testserver.NewWithSessionGate(t, func() bool {
				if opens.Add(1) == 1 {
					return true
				}
				close(blocked)
				return false
			})
			first, finish := context.WithCancel(context.Background())
			defer finish()
			connection, err := sshconn.Dial(first, fixture.Profile())
			if err != nil {
				t.Fatal(err)
			}
			defer connection.Close()
			if _, err := connection.Exec(first, "warmup"); err != nil {
				t.Fatal(err)
			}
			var pool connPool
			lease := pool.lease(pooledKey(fixture.Profile()))
			pool.put(lease, connection)
			finish()
			second, cancel := context.WithCancel(context.Background())
			defer cancel()
			reused := pool.take(second, lease)
			if reused != connection {
				t.Fatal("fixture did not reuse the authenticated transport")
			}
			done := make(chan error, 1)
			go func() {
				var err error
				if operation == "exec" {
					_, err = reused.Exec(second, "blocked-open")
				} else {
					_, err = reused.List(second, "/")
				}
				done <- err
			}()
			select {
			case <-blocked:
			case <-time.After(2 * time.Second):
				t.Fatal("second operation did not reach the stalled channel-open request")
			}
			cancel()
			select {
			case err := <-done:
				if err == nil {
					t.Fatal("cancelled channel-open unexpectedly succeeded")
				}
			case <-time.After(time.Second):
				t.Fatal("cancelled operation left its pooled SSH transport blocked opening a channel")
			}
			if fixture.Auths.Load() != 1 {
				t.Fatal("test did not exercise a single reused SSH authentication")
			}
		})
	}
}

func TestLatePoolReturnAfterConfigurationChangeIsClosed(t *testing.T) {
	fixture := testserver.New(t)
	profile := fixture.Profile()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	connection, err := sshconn.Dial(ctx, profile)
	if err != nil {
		t.Fatal(err)
	}
	defer connection.Close()
	var pool connPool
	key := pooledKey(profile)
	oldLease := pool.lease(key)
	// Reproduce the review's scheduling point after the old task checked ctx:
	// its transport is detached, configuration clears an empty pool and revokes
	// that context, then the task tries to return its old authenticated socket.
	connection.Detach()
	pool.clear()
	cancel()
	pool.put(oldLease, connection)
	if reused := pool.take(context.Background(), oldLease); reused != nil {
		t.Fatal("old credentials re-entered the pool after invalidation")
	}
	verify, stop := context.WithTimeout(context.Background(), time.Second)
	defer stop()
	if _, err := connection.ExecStream(verify, "must-not-execute", io.Discard, io.Discard); err == nil {
		t.Fatal("rejected stale connection remained authenticated and usable")
	}
	// A newly acquired lease still permits reuse under the current generation.
	fresh, err := sshconn.Dial(verify, profile)
	if err != nil {
		t.Fatal(err)
	}
	defer fresh.Close()
	newLease := pool.lease(key)
	pool.put(newLease, fresh)
	if reused := pool.take(verify, newLease); reused != fresh {
		t.Fatal("current generation could not reuse its connection")
	}
}

func TestPoolDoesNotShareAnSSHIdentityAcrossTargetsOrAccounts(t *testing.T) {
	fixture := testserver.New(t)
	original := fixture.Profile()
	connection, err := sshconn.Dial(context.Background(), original)
	if err != nil {
		t.Fatal(err)
	}
	defer connection.Close()
	var pool connPool
	key := pooledKey(original)
	lease := pool.lease(key)
	pool.put(lease, connection)
	for _, field := range []string{"host", "port", "user", "fingerprint", "auth", "agent"} {
		changed := original
		switch field {
		case "host":
			changed.Host = "different.fixture.invalid"
		case "port":
			changed.Port++
		case "user":
			changed.User = "restricted-new-account"
		case "fingerprint":
			changed.Fingerprint += "changed"
		case "auth":
			changed.Auth = "agent"
		case "agent":
			changed.AgentFingerprint = "different-agent-key"
		}
		if reused := pool.take(context.Background(), pool.lease(pooledKey(changed))); reused != nil {
			t.Fatal("connection crossed an SSH identity boundary", field)
		}
	}
	if reused := pool.take(context.Background(), lease); reused != connection {
		t.Fatal("identity checks consumed the original connection")
	}
}
