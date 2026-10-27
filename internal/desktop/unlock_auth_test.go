package desktop

import (
	"context"
	"os"
	"sync/atomic"
	"testing"
	"time"

	"sshclient/internal/broker"
	"sshclient/internal/client"
	"sshclient/internal/localipc"
	"sshclient/internal/systemauth"
	"sshclient/internal/vault"
)

type vaultUnlockHarness struct {
	a       *app
	state   *unlockState
	ui      chan func()
	started chan bool
	results chan approvalAuthResult
	opened  chan *vault.Session
	count   atomic.Int32
	cancel  context.CancelFunc
}

func newVaultUnlockHarness(t *testing.T) *vaultUnlockHarness {
	t.Helper()
	dir, err := os.MkdirTemp("/private/tmp", "sshu-")
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	h := &vaultUnlockHarness{ui: make(chan func(), 32), started: make(chan bool, 8), results: make(chan approvalAuthResult, 8), opened: make(chan *vault.Session, 8), cancel: cancel}
	h.a = &app{ctx: ctx, cancel: cancel, dir: dir, client: client.Client{Dir: dir}, dispatch: func(fn func()) { h.ui <- fn }, authStatus: systemauth.Status{Enabled: true, Available: true, Name: "Touch ID"}}
	h.state = h.a.ensureUnlockState()
	h.state.authenticate = func(_ context.Context, _ []byte, system bool) (*vault.Session, error) {
		h.started <- system
		result := <-h.results // Deliberately ignores cancellation.
		return result.owner, result.err
	}
	h.state.openBroker = func(dir string, owner *vault.Session) (*broker.Server, error) {
		h.count.Add(1)
		h.opened <- owner
		return broker.OpenSession(dir, owner)
	}
	// Successful unlock may initialize the local browser; keep every read in
	// the generated fixture directory, including its directory caches.
	h.a.ensurePanes()
	for _, p := range []*filePane{h.a.localFilePane, h.a.primaryFilePane} {
		p.browser().dir, p.browser().address = dir, dir
	}
	t.Cleanup(func() {
		h.a.disposeUnlockState()
		cancel()
		h.a.cancelDirectoryLoads()
		if h.a.server != nil {
			h.a.server.Vault.Close()
			h.a.server.Close()
		}
		if h.a.unlockCleanup != nil {
			select {
			case <-h.a.unlockCleanup:
			case <-time.After(3 * time.Second):
				t.Error("unlock cleanup did not release its broker")
			}
		}
		os.RemoveAll(dir)
	})
	return h
}

func (h *vaultUnlockHarness) authenticate(t *testing.T) *vault.Session {
	t.Helper()
	h.a.systemUnlock()
	if !awaitApprovalValue(t, h.started) {
		t.Fatal("system verification used the password route")
	}
	owner := generatedApprovalOwner(t)
	h.results <- approvalAuthResult{owner: owner}
	return owner
}

func requireUnlockOwnerClosed(t *testing.T, owner *vault.Session) {
	t.Helper()
	if _, err := owner.Proof([]byte("isolated-unlock-cancellation")); err == nil {
		t.Fatal("cancelled unlock retained a usable vault")
	}
}

func TestVaultUnlockAuthenticatesBeforeBrokerAndUsesTwoUIGates(t *testing.T) {
	h := newVaultUnlockHarness(t)
	owner := h.authenticate(t)
	gate := awaitApprovalValue(t, h.ui)
	if h.count.Load() != 0 || h.a.online {
		t.Fatal("authentication opened the broker before UI review")
	}
	gate()
	awaitApprovalValue(t, h.opened)
	commit := awaitApprovalValue(t, h.ui)
	if h.a.online || h.a.server != nil {
		t.Fatal("broker became the application session before the final gate")
	}
	commit()
	if !h.a.online || h.a.server == nil || h.a.busy || h.a.unlockState != nil {
		t.Fatal("successful unlock did not transfer ownership exactly once")
	}
	if _, err := owner.Proof([]byte("accepted-owner")); err != nil {
		t.Fatal("accepted owner was closed by attempt cleanup", err)
	}
	awaitApprovalValue(t, h.a.unlockCleanup)
}

func TestVaultUnlockLateAuthenticationCannotOpenBroker(t *testing.T) {
	for _, action := range []string{"cancel", "password", "dispose", "lock", "parent", "generation"} {
		t.Run(action, func(t *testing.T) {
			h := newVaultUnlockHarness(t)
			owner := h.authenticate(t)
			gate := awaitApprovalValue(t, h.ui)
			switch action {
			case "cancel":
				h.a.cancelUnlockAttempt()
			case "password":
				h.a.expandUnlockPassword()
			case "dispose":
				h.a.disposeUnlockState()
			case "lock":
				h.a.lock()
			case "parent":
				h.cancel()
			case "generation":
				h.a.generation++
			}
			gate()
			if h.a.online || h.count.Load() != 0 {
				t.Fatal("stale verification started a broker")
			}
			requireUnlockOwnerClosed(t, owner)
		})
	}
}

func TestVaultUnlockCancelledWaiterPreservesBrokerCleanupChain(t *testing.T) {
	h := newVaultUnlockHarness(t)
	release := make(chan struct{})
	defer close(release)
	h.state.openBroker = func(dir string, owner *vault.Session) (*broker.Server, error) {
		count := h.count.Add(1)
		h.opened <- owner
		if count == 1 {
			<-release
		}
		return broker.OpenSession(dir, owner)
	}
	first := h.authenticate(t)
	awaitApprovalValue(t, h.ui)()
	awaitApprovalValue(t, h.opened)
	firstCleanup := h.a.unlockCleanup
	h.a.cancelUnlockAttempt()
	requireUnlockOwnerClosed(t, first)

	second := h.authenticate(t)
	awaitApprovalValue(t, h.ui)()
	secondCleanup := h.a.unlockCleanup
	h.a.cancelUnlockAttempt()
	requireUnlockOwnerClosed(t, second)
	third := h.authenticate(t)
	awaitApprovalValue(t, h.ui)()
	if h.count.Load() != 1 {
		t.Fatal("retry opened while the first broker still owned startup")
	}
	select {
	case <-secondCleanup:
		t.Fatal("cancelled middle waiter released the startup barrier too early")
	default:
	}
	// Release the first startup once and then disable the deferred close.
	release <- struct{}{}
	awaitApprovalValue(t, firstCleanup)
	awaitApprovalValue(t, secondCleanup)
	if opened := awaitApprovalValue(t, h.opened); opened != third || h.count.Load() != 2 {
		t.Fatal("cancelled middle attempt created a broker or lost the last retry")
	}
	awaitApprovalValue(t, h.ui)()
	if !h.a.online || h.a.server == nil {
		t.Fatal("retry did not unlock after old cleanup")
	}
}

func TestVaultUnlockCancellationDuringBrokerStartupNeedsNoUICallback(t *testing.T) {
	h := newVaultUnlockHarness(t)
	release := make(chan struct{})
	h.state.openBroker = func(dir string, owner *vault.Session) (*broker.Server, error) {
		h.opened <- owner
		<-release
		return broker.OpenSession(dir, owner)
	}
	owner := h.authenticate(t)
	awaitApprovalValue(t, h.ui)()
	awaitApprovalValue(t, h.opened)
	cleanup := h.a.unlockCleanup
	h.a.disposeUnlockState()
	h.cancel()
	requireUnlockOwnerClosed(t, owner)
	close(release)
	awaitApprovalValue(t, cleanup)
	if h.a.online || h.a.server != nil {
		t.Fatal("cancelled startup unlocked after the UI was gone")
	}
	releaseLock, err := localipc.Acquire(h.a.dir)
	if err != nil {
		t.Fatal("cancelled broker kept its directory lock", err)
	}
	releaseLock()
}

func TestVaultUnlockAutomaticWaitsForActiveAppAndRechecksQueuedReadiness(t *testing.T) {
	s := &unlockState{}
	now := time.Now()
	clock := func() time.Time { return now }
	ready := authReadiness{windowReady: true, visible: true, focused: true, active: true, systemReady: true}
	var queued []func()
	starts := 0
	post := func(fn func()) { queued = append(queued, fn) }
	current := func() (authReadiness, bool) { return ready, true }
	start := func() { starts++ }
	for _, missing := range []string{"window", "visible", "focus", "active", "system"} {
		r := ready
		switch missing {
		case "window":
			r.windowReady = false
		case "visible":
			r.visible = false
		case "focus":
			r.focused = false
		case "active":
			r.active = false
		case "system":
			r.systemReady = false
		}
		s.queueAutomaticUnlock(r, clock, post, current, start)
		if len(queued) != 0 || s.autoAttempted {
			t.Fatal("incomplete native readiness spent the automatic attempt", missing)
		}
	}
	s.queueAutomaticUnlock(ready, clock, post, current, start)
	s.queueAutomaticUnlock(ready, clock, post, current, start)
	if len(queued) != 1 {
		t.Fatal("automatic request was queued twice")
	}
	ready.active = false
	queued[0]()
	if starts != 0 || s.autoAttempted || s.autoQueued {
		t.Fatal("queued authentication ignored later app deactivation")
	}
	queued = nil
	ready.active = true
	s.queueAutomaticUnlock(ready, clock, post, current, start)
	queued[0]()
	s.queueAutomaticUnlock(ready, clock, post, current, start)
	if starts != 1 || !s.autoAttempted || len(queued) != 1 {
		t.Fatal("activation did not begin exactly one authentication")
	}
}

func TestVaultUnlockTransientAutomaticInterruptionRetriesOnceWithoutError(t *testing.T) {
	h := newVaultUnlockHarness(t)
	h.state.autoAttempted = true
	h.a.beginVaultUnlockAttempt(true, true)
	awaitApprovalValue(t, h.started)
	err := &systemauth.EmbeddedError{Code: "notReady", Message: "Touch ID 暂未就绪，请重试"}
	h.results <- approvalAuthResult{err: err}
	awaitApprovalValue(t, h.ui)()
	if h.a.unlockError != "" || h.state.passwordMode || h.a.busy || h.state.autoAttempted || !h.state.autoRetryUsed {
		t.Fatal("temporary startup interruption forced password mode or showed a false failure")
	}
	ready := authReadiness{windowReady: true, visible: true, focused: true, active: true, systemReady: true}
	clockNow := h.state.autoRetryAt.Add(-time.Nanosecond)
	clock := func() time.Time { return clockNow }
	post := func(fn func()) { h.ui <- fn }
	current := func() (authReadiness, bool) { return ready, true }
	start := func() { h.a.beginVaultUnlockAttempt(true, true) }
	h.state.queueAutomaticUnlock(ready, clock, post, current, start)
	if h.state.autoQueued {
		t.Fatal("temporary failure retried without a settling interval")
	}
	clockNow = h.state.autoRetryAt
	h.state.queueAutomaticUnlock(ready, clock, post, current, start)
	awaitApprovalValue(t, h.ui)()
	awaitApprovalValue(t, h.started)
	h.results <- approvalAuthResult{err: err}
	awaitApprovalValue(t, h.ui)()
	if h.a.busy || h.state.passwordMode || h.a.unlockError == "" || !h.state.autoAttempted {
		t.Fatal("second interruption did not stop with a retryable diagnostic")
	}
	h.state.queueAutomaticUnlock(ready, clock, post, current, start)
	if h.state.autoQueued || h.count.Load() != 0 {
		t.Fatal("temporary failure created an authentication loop or started the broker")
	}
}

func TestVaultUnlockCancelAndRejectedFingerprintNeverAutoRetry(t *testing.T) {
	for _, code := range []string{"cancelled", "authenticationFailed", "biometryUnavailable", "biometryLocked", "unrecognized"} {
		t.Run(code, func(t *testing.T) {
			h := newVaultUnlockHarness(t)
			h.state.autoAttempted = true
			h.a.beginVaultUnlockAttempt(true, true)
			awaitApprovalValue(t, h.started)
			h.results <- approvalAuthResult{err: &systemauth.EmbeddedError{Code: code, Message: "isolated verification outcome"}}
			awaitApprovalValue(t, h.ui)()
			if h.state.autoRetryUsed || !h.state.autoAttempted || h.a.busy || h.count.Load() != 0 {
				t.Fatal("a cancelled or rejected verification retried automatically")
			}
			if code == "cancelled" {
				if h.state.passwordMode || h.a.unlockError != "" {
					t.Fatal("user cancellation became a hardware failure or forced password mode")
				}
			} else if !h.state.passwordMode || h.a.unlockError == "" {
				t.Fatal("a real failure lost its password fallback or diagnostic")
			}
		})
	}
}
