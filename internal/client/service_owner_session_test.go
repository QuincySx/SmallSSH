package client

import (
	"context"
	"testing"
	"time"

	"sshclient/internal/localipc"
	"sshclient/internal/protocol"
)

const ownerSessionTestTimeout = 10 * time.Second

func ownerSessionTestPrepare(t *testing.T, fixture prepareSDKFixture, authCtx context.Context) *PreparedOwner {
	t.Helper()
	prepared, err := fixture.sdk.PrepareOwner(authCtx, fixture.source, 0)
	if err != nil {
		t.Fatal(err)
	}
	if prepared == nil {
		t.Fatal("PrepareOwner succeeded without a prepared owner")
	}
	return prepared
}

func ownerSessionTestPrepareLive(t *testing.T, fixture prepareSDKFixture) (*PreparedOwner, context.CancelFunc) {
	t.Helper()
	authCtx, cancelAuth := context.WithCancel(context.Background())
	t.Cleanup(cancelAuth)
	return ownerSessionTestPrepare(t, fixture, authCtx), cancelAuth
}

func ownerSessionTestCommit(t *testing.T, prepared *PreparedOwner) *OwnerSession {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), ownerSessionTestTimeout)
	defer cancel()
	session, err := prepared.Commit(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if session == nil {
		t.Fatal("Commit succeeded without an owner session")
	}
	return session
}

func ownerSessionTestPrepareAndCommit(t *testing.T, fixture prepareSDKFixture) (*PreparedOwner, *OwnerSession) {
	t.Helper()
	prepared, cancelAuth := ownerSessionTestPrepareLive(t, fixture)
	session := ownerSessionTestCommit(t, prepared)
	cancelAuth()
	return prepared, session
}

func requireOwnerSessionHostState(t *testing.T, fixture prepareSDKFixture, want string) {
	t.Helper()
	if state := fixture.host.Status().State; state != want {
		t.Fatalf("host state = %q, want %q", state, want)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	status, err := fixture.sdk.Status(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if status.State != want {
		t.Fatalf("SDK status state = %q, want %q", status.State, want)
	}
}

func requireOwnerSessionListSnippets(t *testing.T, fixture prepareSDKFixture, session *OwnerSession) {
	t.Helper()
	pin, err := session.BrokerPublicKeyPin()
	if err != nil {
		t.Fatal(err)
	}
	data := Client{Dir: fixture.dir, BrokerKey: pin}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if _, err := data.ListSnippets(ctx, session); err != nil {
		t.Fatalf("ListSnippets with managed owner session failed: %v", err)
	}
}

func requireOwnerSessionListSnippetsFails(t *testing.T, fixture prepareSDKFixture, session *OwnerSession) {
	t.Helper()
	pin, err := session.BrokerPublicKeyPin()
	if err != nil {
		t.Fatal(err)
	}
	data := Client{Dir: fixture.dir, BrokerKey: pin}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if _, err := data.ListSnippets(ctx, session); err == nil {
		t.Fatal("ListSnippets succeeded with a closed owner session")
	}
}

func requireOwnerSessionProof(t *testing.T, session *OwnerSession) {
	t.Helper()
	proof, err := session.Proof([]byte("b05 owner session synthetic proof"))
	if err != nil {
		t.Fatal(err)
	}
	if len(proof) == 0 {
		t.Fatal("owner session produced an empty proof")
	}
	clear(proof)
}

func requireOwnerSessionProofFails(t *testing.T, session *OwnerSession) {
	t.Helper()
	proof, err := session.Proof([]byte("b05 owner session proof after close"))
	clear(proof)
	if err == nil {
		t.Fatal("owner session proof succeeded after close")
	}
}

func validOwnerSessionTestPin(pin string) bool {
	if len(pin) != 71 || pin[:7] != "sha256:" {
		return false
	}
	for index := 7; index < len(pin); index++ {
		character := pin[index]
		if !(character >= '0' && character <= '9' || character >= 'a' && character <= 'f') {
			return false
		}
	}
	return true
}

func requireOwnerSessionPublicMetadata(t *testing.T, session *OwnerSession) {
	t.Helper()
	info := session.Info()
	if !isLowerHex32(info.ID) {
		t.Fatalf("owner session ID = %q, want 32 lowercase hex characters", info.ID)
	}
	if !isLowerHex32(info.Epoch) {
		t.Fatalf("owner session epoch = %q, want 32 lowercase hex characters", info.Epoch)
	}
	if !info.Expires.After(time.Now()) {
		t.Fatalf("owner session expiry = %v, want a future time", info.Expires)
	}
	pin, err := session.BrokerPublicKeyPin()
	if err != nil {
		t.Fatal(err)
	}
	if !validOwnerSessionTestPin(pin) {
		t.Fatalf("owner session broker pin = %q, want sha256: plus 64 lowercase hex characters", pin)
	}
	requireOwnerSessionProof(t, session)
}

func waitOwnerSessionClosed(t *testing.T, session *OwnerSession, timeout time.Duration) {
	t.Helper()
	select {
	case <-session.Done():
	case <-time.After(timeout):
		t.Fatalf("owner session Done did not close within %v", timeout)
	}
}

func closeOwnerSessionBounded(t *testing.T, session *OwnerSession) {
	t.Helper()
	closed := make(chan struct{})
	go func() {
		session.Close()
		session.Close()
		close(closed)
	}()
	select {
	case <-closed:
	case <-time.After(2 * time.Second):
		t.Fatal("owner session Close did not return within 2 seconds")
	}
}

func requireOwnerSessionWireClosedBounded(t *testing.T, wire *localipc.Wire) {
	t.Helper()
	if wire == nil {
		t.Fatal("prepared owner has no wire connection")
	}
	_ = wire.Conn.SetDeadline(time.Now().Add(time.Second))
	result := make(chan error, 1)
	go func() {
		result <- wire.Send(protocol.ServiceMessage{
			Action: "status",
			Major:  protocol.ServiceProtocolMajor,
			Minor:  protocol.ServiceProtocolMinor,
		})
	}()
	select {
	case err := <-result:
		if err == nil {
			t.Fatal("prepared wire accepted a send after close")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("prepared wire send did not finish within 2 seconds")
	}
}

func TestServiceOwnerSessionCommitDetachesFromAuthAndCommitContexts(t *testing.T) {
	fixture := newPrepareSDKFixture(t)
	authCtx, cancelAuth := context.WithCancel(context.Background())
	defer cancelAuth()
	prepared := ownerSessionTestPrepare(t, fixture, authCtx)
	t.Cleanup(prepared.Close)

	fixture.source.Close()
	commitCtx, cancelCommit := context.WithTimeout(context.Background(), ownerSessionTestTimeout)
	session, err := prepared.Commit(commitCtx)
	cancelCommit()
	if err != nil {
		t.Fatal(err)
	}
	if session == nil {
		t.Fatal("Commit succeeded without an owner session")
	}
	t.Cleanup(session.Close)
	waitPreparedClosed(t, prepared)
	cancelAuth()

	requireOwnerSessionPublicMetadata(t, session)
	requireOwnerSessionListSnippets(t, fixture, session)
	requireOwnerSessionHostState(t, fixture, protocol.ServiceStateUnlocked)

	prepared.Close()
	requireOwnerSessionListSnippets(t, fixture, session)
	requireOwnerSessionHostState(t, fixture, protocol.ServiceStateUnlocked)
}

func TestServiceOwnerSessionCloseIsolationAndRootCleanup(t *testing.T) {
	fixture := newPrepareSDKFixture(t)
	preparedA, sessionA := ownerSessionTestPrepareAndCommit(t, fixture)
	preparedB, sessionB := ownerSessionTestPrepareAndCommit(t, fixture)
	t.Cleanup(preparedA.Close)
	t.Cleanup(preparedB.Close)
	t.Cleanup(sessionA.Close)
	t.Cleanup(sessionB.Close)
	waitPreparedClosed(t, preparedA)
	waitPreparedClosed(t, preparedB)

	requireOwnerSessionListSnippets(t, fixture, sessionA)
	requireOwnerSessionListSnippets(t, fixture, sessionB)
	requireOwnerSessionHostState(t, fixture, protocol.ServiceStateUnlocked)

	sessionA.Close()
	waitOwnerSessionClosed(t, sessionA, 2*time.Second)
	requireOwnerSessionProofFails(t, sessionA)
	requireOwnerSessionListSnippetsFails(t, fixture, sessionA)
	requireOwnerSessionListSnippets(t, fixture, sessionB)
	requireOwnerSessionHostState(t, fixture, protocol.ServiceStateUnlocked)

	sessionB.Close()
	waitOwnerSessionClosed(t, sessionB, 2*time.Second)
	requireOwnerSessionHostState(t, fixture, protocol.ServiceStateUnlocked)
	proof, err := fixture.source.Proof([]byte("b05 root source remains usable"))
	if err != nil {
		t.Fatalf("root source proof after SDK close failed: %v", err)
	}
	if len(proof) == 0 {
		t.Fatal("root source produced an empty proof after SDK close")
	}
	clear(proof)
	unlockKey, err := fixture.source.UnlockKey()
	if err != nil {
		t.Fatalf("root source unlock key after SDK close failed: %v", err)
	}
	if len(unlockKey) != 32 {
		clear(unlockKey)
		t.Fatalf("root source unlock key length = %d, want 32", len(unlockKey))
	}
	clear(unlockKey)

	fixture.host.Lock()
	requireOwnerSessionHostState(t, fixture, protocol.ServiceStateLocked)
}

func TestServiceOwnerSessionControlLossClosesWithoutHeartbeat(t *testing.T) {
	t.Run("host lock", func(t *testing.T) {
		fixture := newPrepareSDKFixture(t)
		prepared, session := ownerSessionTestPrepareAndCommit(t, fixture)
		t.Cleanup(prepared.Close)
		t.Cleanup(session.Close)

		fixture.host.Lock()
		waitOwnerSessionClosed(t, session, 2*time.Second)
		requireOwnerSessionProofFails(t, session)
		closeOwnerSessionBounded(t, session)
		requireOwnerSessionHostState(t, fixture, protocol.ServiceStateLocked)

		freshPrepared, freshSession := ownerSessionTestPrepareAndCommit(t, fixture)
		t.Cleanup(freshPrepared.Close)
		t.Cleanup(freshSession.Close)
		requireOwnerSessionListSnippets(t, fixture, freshSession)
		requireOwnerSessionHostState(t, fixture, protocol.ServiceStateUnlocked)
		requireOwnerSessionProofFails(t, session)
	})

	t.Run("host close", func(t *testing.T) {
		fixture := newPrepareSDKFixture(t)
		prepared, session := ownerSessionTestPrepareAndCommit(t, fixture)
		t.Cleanup(prepared.Close)
		t.Cleanup(session.Close)

		if err := fixture.host.Close(); err != nil {
			t.Fatal(err)
		}
		waitOwnerSessionClosed(t, session, 2*time.Second)
		requireOwnerSessionProofFails(t, session)
		closeOwnerSessionBounded(t, session)
	})
}

type ownerSessionCommitResult struct {
	session *OwnerSession
	err     error
}

func receiveOwnerSessionCommitResult(t *testing.T, results <-chan ownerSessionCommitResult) ownerSessionCommitResult {
	t.Helper()
	select {
	case result := <-results:
		return result
	case <-time.After(ownerSessionTestTimeout):
		t.Fatal("Commit did not return within the test deadline")
		return ownerSessionCommitResult{}
	}
}

func TestServiceOwnerSessionCommitStatesAndRaces(t *testing.T) {
	t.Run("invalid states", func(t *testing.T) {
		var nilPrepared *PreparedOwner
		if session, err := nilPrepared.Commit(context.Background()); err == nil || session != nil {
			t.Fatal("nil PreparedOwner.Commit was not rejected")
		}

		fixture := newPrepareSDKFixture(t)
		prepared := ownerSessionTestPrepare(t, fixture, context.Background())
		prepared.Close()
		waitPreparedClosed(t, prepared)
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		session, err := prepared.Commit(ctx)
		cancel()
		if err == nil || session != nil {
			t.Fatal("Commit succeeded after PreparedOwner.Close")
		}

		prepared = ownerSessionTestPrepare(t, fixture, context.Background())
		canceledCtx, cancelCanceled := context.WithCancel(context.Background())
		cancelCanceled()
		session, err = prepared.Commit(canceledCtx)
		if err == nil || session != nil {
			prepared.Close()
			t.Fatal("Commit succeeded with a canceled context")
		}
		prepared.Close()
	})

	t.Run("repeat commit", func(t *testing.T) {
		fixture := newPrepareSDKFixture(t)
		prepared, session := ownerSessionTestPrepareAndCommit(t, fixture)
		t.Cleanup(prepared.Close)
		t.Cleanup(session.Close)
		waitPreparedClosed(t, prepared)

		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		repeated, err := prepared.Commit(ctx)
		cancel()
		if err == nil || repeated != nil {
			t.Fatal("repeated Commit was not rejected")
		}
		requireOwnerSessionListSnippets(t, fixture, session)
	})

	t.Run("concurrent duplicate commit", func(t *testing.T) {
		fixture := newPrepareSDKFixture(t)
		for iteration := 0; iteration < 3; iteration++ {
			prepared, cancelAuth := ownerSessionTestPrepareLive(t, fixture)
			ctx, cancelCommit := context.WithTimeout(context.Background(), ownerSessionTestTimeout)
			start := make(chan struct{})
			results := make(chan ownerSessionCommitResult, 2)
			for worker := 0; worker < 2; worker++ {
				go func() {
					<-start
					session, err := prepared.Commit(ctx)
					results <- ownerSessionCommitResult{session: session, err: err}
				}()
			}
			close(start)
			first := receiveOwnerSessionCommitResult(t, results)
			second := receiveOwnerSessionCommitResult(t, results)
			cancelCommit()
			cancelAuth()

			var winner *OwnerSession
			var loser ownerSessionCommitResult
			switch {
			case first.session != nil && first.err == nil && second.session == nil && second.err != nil:
				winner = first.session
				loser = second
			case second.session != nil && second.err == nil && first.session == nil && first.err != nil:
				winner = second.session
				loser = first
			default:
				if first.session != nil {
					first.session.Close()
				}
				if second.session != nil {
					second.session.Close()
				}
				t.Fatalf("concurrent Commit results = (%v, %v), want exactly one winner", first.err, second.err)
			}
			waitPreparedClosed(t, prepared)
			requireOwnerSessionListSnippets(t, fixture, winner)
			if loser.err == nil {
				winner.Close()
				t.Fatal("losing concurrent Commit returned no error")
			}
			closeOwnerSessionBounded(t, winner)
			prepared.Close()
		}
	})

	t.Run("concurrent commit and close", func(t *testing.T) {
		fixture := newPrepareSDKFixture(t)
		for iteration := 0; iteration < 3; iteration++ {
			prepared, cancelAuth := ownerSessionTestPrepareLive(t, fixture)
			prepared.mu.Lock()
			wire := prepared.wire
			prepared.mu.Unlock()

			ctx, cancelCommit := context.WithTimeout(context.Background(), ownerSessionTestTimeout)
			start := make(chan struct{})
			result := make(chan ownerSessionCommitResult, 1)
			closed := make(chan struct{})
			go func() {
				<-start
				session, err := prepared.Commit(ctx)
				result <- ownerSessionCommitResult{session: session, err: err}
			}()
			go func() {
				<-start
				prepared.Close()
				close(closed)
			}()
			close(start)
			commitResult := receiveOwnerSessionCommitResult(t, result)
			select {
			case <-closed:
			case <-time.After(ownerSessionTestTimeout):
				t.Fatal("PreparedOwner.Close did not return within the test deadline")
			}
			cancelCommit()
			cancelAuth()

			if commitResult.session != nil {
				if commitResult.err != nil {
					commitResult.session.Close()
					t.Fatal("Commit returned both a session and an error")
				}
				closeOwnerSessionBounded(t, commitResult.session)
			} else if commitResult.err == nil {
				t.Fatal("Commit returned neither a session nor an error")
			}
			waitPreparedClosed(t, prepared)
			requireOwnerSessionWireClosedBounded(t, wire)
		}
	})
}
