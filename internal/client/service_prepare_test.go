package client

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"sync/atomic"
	"testing"
	"time"

	"sshclient/internal/broker"
	"sshclient/internal/localipc"
	"sshclient/internal/protocol"
	"sshclient/internal/service"
	"sshclient/internal/vault"
)

type prepareSDKFixture struct {
	dir    string
	host   *service.Host
	source *vault.Session
	sdk    ServiceClient
}

func newPrepareSDKFixture(t *testing.T) prepareSDKFixture {
	t.Helper()
	dir := b03bClientTestDir(t)
	password := []byte("b04d synthetic prepare master password")
	source, err := vault.New(password)
	clear(password)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(source.Close)
	if err := vault.Save(broker.VaultPath(dir), source, true); err != nil {
		t.Fatal(err)
	}
	host, err := service.Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = host.Close() })
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	return prepareSDKFixture{
		dir:    dir,
		host:   host,
		source: source,
		sdk:    ServiceClient{Dir: dir, Executable: executable},
	}
}

func requirePrepareSDKLocked(t *testing.T, fixture prepareSDKFixture) {
	t.Helper()
	if state := fixture.host.Status().State; state != protocol.ServiceStateLocked {
		t.Fatalf("host state = %q, want %q", state, protocol.ServiceStateLocked)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	status, err := fixture.sdk.Status(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if status.State != protocol.ServiceStateLocked {
		t.Fatalf("SDK status state = %q, want %q", status.State, protocol.ServiceStateLocked)
	}
}

func requireNoPrepareBrokerListener(t *testing.T, dir string) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 250*time.Millisecond)
	defer cancel()
	connection, err := localipc.Dial(ctx, dir)
	if err == nil {
		_ = connection.Close()
		t.Fatal("broker listener unexpectedly accepted a connection")
	}
}

func waitPreparedClosed(t *testing.T, prepared *PreparedOwner) {
	t.Helper()
	select {
	case <-prepared.Done():
	case <-time.After(2 * time.Second):
		t.Fatal("PreparedOwner.Done did not close within 2 seconds")
	}
}

func requirePreparedWireClosed(t *testing.T, wire *localipc.Wire) {
	t.Helper()
	if wire == nil {
		t.Fatal("prepared owner has no wire connection")
	}
	err := wire.Send(protocol.ServiceMessage{
		Action: "status",
		Major:  protocol.ServiceProtocolMajor,
		Minor:  protocol.ServiceProtocolMinor,
	})
	if err == nil {
		t.Fatal("prepared wire accepted a send after close")
	}
}

func requirePrepareRejected(t *testing.T, prepared *PreparedOwner, err error, message string) {
	t.Helper()
	if prepared != nil {
		prepared.Close()
	}
	if err == nil {
		t.Fatal(message)
	}
}

func TestServicePrepareStaysLockedAndCloseIsIdempotent(t *testing.T) {
	fixture := newPrepareSDKFixture(t)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	prepared, err := fixture.sdk.PrepareOwner(ctx, fixture.source, 0)
	if err != nil {
		t.Fatal(err)
	}
	if prepared == nil {
		t.Fatal("PrepareOwner succeeded without a prepared owner")
	}
	t.Cleanup(prepared.Close)
	prepared.mu.Lock()
	wire := prepared.wire
	prepared.mu.Unlock()
	if wire == nil {
		t.Fatal("prepared owner has no wire connection")
	}
	select {
	case <-prepared.Done():
		t.Fatal("PreparedOwner.Done closed before Close")
	default:
	}
	requirePrepareSDKLocked(t, fixture)
	requireNoPrepareBrokerListener(t, fixture.dir)

	fixture.source.Close()
	select {
	case <-prepared.Done():
		t.Fatal("closing the unlock source closed the prepared owner")
	default:
	}
	prepared.Close()
	prepared.Close()
	waitPreparedClosed(t, prepared)
	requirePreparedWireClosed(t, wire)
	requirePrepareSDKLocked(t, fixture)

	var nilPrepared *PreparedOwner
	nilPrepared.Close()
}

func TestServicePrepareAuthContextCancellationClosesPending(t *testing.T) {
	fixture := newPrepareSDKFixture(t)
	authCtx, cancelAuth := context.WithCancel(context.Background())
	defer cancelAuth()

	prepared, err := fixture.sdk.PrepareOwner(authCtx, fixture.source, 0)
	if err != nil {
		t.Fatal(err)
	}
	if prepared == nil {
		t.Fatal("PrepareOwner succeeded without a prepared owner")
	}
	t.Cleanup(prepared.Close)
	prepared.mu.Lock()
	wire := prepared.wire
	prepared.mu.Unlock()
	if wire == nil {
		t.Fatal("prepared owner has no wire connection")
	}

	cancelAuth()
	waitPreparedClosed(t, prepared)
	requirePreparedWireClosed(t, wire)
	requirePrepareSDKLocked(t, fixture)

	proof, err := fixture.source.Proof([]byte("b04d synthetic proof after auth cancellation"))
	if err != nil {
		t.Fatal("SDK closed the caller's unlock source")
	}
	if len(proof) == 0 {
		t.Fatal("unlock source produced an empty proof")
	}
	clear(proof)

	freshCtx, freshCancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer freshCancel()
	fresh, err := fixture.sdk.PrepareOwner(freshCtx, fixture.source, 0)
	if err != nil {
		t.Fatalf("service was unusable after auth cancellation: %v", err)
	}
	if fresh == nil {
		t.Fatal("PrepareOwner succeeded without a prepared owner")
	}
	fresh.Close()
	waitPreparedClosed(t, fresh)
	requirePrepareSDKLocked(t, fixture)
}

type prepareSourceSpy struct {
	session     *vault.Session
	unlockCalls atomic.Int32
	proofCalls  atomic.Int32
}

var _ UnlockSource = (*prepareSourceSpy)(nil)

func (s *prepareSourceSpy) UnlockKey() ([]byte, error) {
	s.unlockCalls.Add(1)
	return s.session.UnlockKey()
}

func (s *prepareSourceSpy) Proof(message []byte) ([]byte, error) {
	s.proofCalls.Add(1)
	return s.session.Proof(message)
}

func (s *prepareSourceSpy) calls() (int32, int32) {
	return s.unlockCalls.Load(), s.proofCalls.Load()
}

func requirePrepareSourceUnused(t *testing.T, source *prepareSourceSpy) {
	t.Helper()
	unlockCalls, proofCalls := source.calls()
	if unlockCalls != 0 || proofCalls != 0 {
		t.Fatalf("source methods called: UnlockKey=%d Proof=%d", unlockCalls, proofCalls)
	}
}

func TestServicePrepareRejectsInvalidInputsBeforeKeyRelease(t *testing.T) {
	fixture := newPrepareSDKFixture(t)
	requirePrepareSDKLocked(t, fixture)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	t.Run("nil source", func(t *testing.T) {
		prepared, err := fixture.sdk.PrepareOwner(ctx, nil, 0)
		requirePrepareRejected(t, prepared, err, "accepted a nil unlock source")
	})

	t.Run("typed nil source", func(t *testing.T) {
		var source *vault.Session
		prepared, err := fixture.sdk.PrepareOwner(ctx, source, 0)
		requirePrepareRejected(t, prepared, err, "accepted a typed-nil unlock source")
	})

	t.Run("nil context", func(t *testing.T) {
		source := &prepareSourceSpy{session: fixture.source}
		prepared, err := fixture.sdk.PrepareOwner(nil, source, 0)
		requirePrepareRejected(t, prepared, err, "accepted a nil context")
		requirePrepareSourceUnused(t, source)
	})

	t.Run("canceled context", func(t *testing.T) {
		canceled, stop := context.WithCancel(context.Background())
		stop()
		source := &prepareSourceSpy{session: fixture.source}
		prepared, err := fixture.sdk.PrepareOwner(canceled, source, 0)
		if prepared != nil {
			prepared.Close()
		}
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("canceled context error = %v", err)
		}
		requirePrepareSourceUnused(t, source)
	})

	for _, test := range []struct {
		name     string
		lifetime time.Duration
	}{
		{name: "below minimum", lifetime: 59 * time.Second},
		{name: "above maximum", lifetime: 8*time.Hour + time.Second},
		{name: "fractional seconds", lifetime: 90*time.Second + 500*time.Millisecond},
	} {
		test := test
		t.Run(test.name, func(t *testing.T) {
			source := &prepareSourceSpy{session: fixture.source}
			prepared, err := fixture.sdk.PrepareOwner(ctx, source, test.lifetime)
			requirePrepareRejected(t, prepared, err, "accepted an invalid owner lifetime")
			requirePrepareSourceUnused(t, source)
		})
	}

	t.Run("missing executable", func(t *testing.T) {
		source := &prepareSourceSpy{session: fixture.source}
		sdk := fixture.sdk
		sdk.Executable = filepath.Join(fixture.dir, "missing-prepare-executable")
		prepared, err := sdk.PrepareOwner(ctx, source, 0)
		requirePrepareRejected(t, prepared, err, "accepted a missing trusted executable")
		requirePrepareSourceUnused(t, source)
	})

	t.Run("different executable", func(t *testing.T) {
		self, err := localipc.ExecutableCodeID(fixture.sdk.Executable)
		if err != nil {
			t.Fatal(err)
		}
		var candidates []string
		switch runtime.GOOS {
		case "darwin":
			candidates = []string{"/usr/bin/true", "/bin/true", filepath.Join(runtime.GOROOT(), "bin", "go")}
		case "linux":
			candidates = []string{"/bin/true", "/usr/bin/true"}
		case "windows":
			candidates = []string{filepath.Join(runtime.GOROOT(), "bin", "go.exe")}
		default:
			t.Skip("no second signed executable available")
		}
		different := ""
		for _, candidate := range candidates {
			code, err := localipc.ExecutableCodeID(candidate)
			if err == nil && code != self {
				different = candidate
				break
			}
		}
		if different == "" {
			t.Skip("no different trusted executable available")
		}
		source := &prepareSourceSpy{session: fixture.source}
		sdk := fixture.sdk
		sdk.Executable = different
		prepared, err := sdk.PrepareOwner(ctx, source, 0)
		requirePrepareRejected(t, prepared, err, "accepted a different executable as the service host")
		requirePrepareSourceUnused(t, source)
	})

	requirePrepareSDKLocked(t, fixture)
}
