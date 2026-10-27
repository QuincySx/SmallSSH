package desktop

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"
	"sshclient/internal/approval"
	"sshclient/internal/systemauth"
	"sshclient/internal/vault"
)

type approvalAuthResult struct {
	owner *vault.Session
	err   error
}

type approvalAuthHarness struct {
	state      *approvalPromptState
	request    approval.Request
	ui         chan func()
	started    chan bool
	results    chan approvalAuthResult
	submitted  chan approval.Request
	submitDone chan struct{}
	current    bool
	done       int
}

func newApprovalAuthHarness(t *testing.T) *approvalAuthHarness {
	t.Helper()
	_, r := reviewFixture("full")
	r.Nonce = "original-generation"
	h := &approvalAuthHarness{request: r, ui: make(chan func(), 8), started: make(chan bool, 8), results: make(chan approvalAuthResult, 8), submitted: make(chan approval.Request, 8), submitDone: make(chan struct{}), current: true}
	h.state = &approvalPromptState{requestKey: approvalRequestKey(r)}
	h.state.flow = &approvalAuthFlow{
		context: context.Background(), post: func(fn func()) { h.ui <- fn }, current: func() bool { return h.current },
		authenticate: func(_ context.Context, _ []byte, system bool) (*vault.Session, error) {
			h.started <- system
			r := <-h.results // Deliberately ignores cancellation, like a late native callback.
			return r.owner, r.err
		},
		submit: func(_ context.Context, reviewed approval.Request, _ bool, _ *vault.Session) error {
			h.submitted <- reviewed
			<-h.submitDone
			return nil
		},
		done: func() { h.done++ },
	}
	t.Cleanup(func() { close(h.submitDone) })
	return h
}

func awaitApprovalValue[T any](t *testing.T, ch <-chan T) T {
	t.Helper()
	select {
	case value := <-ch:
		return value
	case <-time.After(3 * time.Second):
		t.Fatal("approval worker did not reach expected phase")
		var zero T
		return zero
	}
}

func generatedApprovalOwner(t *testing.T) *vault.Session {
	t.Helper()
	owner, err := vault.New([]byte("offline-synthetic-owner-password"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(owner.Close)
	return owner
}

func TestApprovalAuthenticationReturnsToUIGateBeforeSubmitting(t *testing.T) {
	for _, system := range []bool{false, true} {
		h := newApprovalAuthHarness(t)
		h.state.passwordMode, h.state.password = true, "offline-synthetic-password"
		h.state.start(h.request, true, system, false)
		if got := awaitApprovalValue(t, h.started); got != system {
			t.Fatal("authentication used the wrong method")
		}
		h.state.start(h.request, true, system, false)
		select {
		case <-h.started:
			t.Fatal("busy state started duplicate authentication")
		default:
		}
		owner := generatedApprovalOwner(t)
		h.results <- approvalAuthResult{owner: owner}
		gate := awaitApprovalValue(t, h.ui)
		select {
		case <-h.submitted:
			t.Fatal("authentication bypassed the UI gate")
		default:
		}
		gate()
		if !h.state.submitting || !h.state.passwordMode || h.state.password != "" {
			t.Fatal("submission did not clear password or retain expanded mode")
		}
		h.state.expandPassword()
		if !h.state.submitting || !h.state.busy {
			t.Fatal("method switch interrupted an RPC already being submitted")
		}
		h.state.start(h.request, true, system, false)
		got := awaitApprovalValue(t, h.submitted)
		if got.ID != h.request.ID || got.Nonce != h.request.Nonce {
			t.Fatal("decision changed the reviewed request generation")
		}
		select {
		case <-h.started:
			t.Fatal("submitting state started duplicate authentication")
		default:
		}
	}
}

func TestApprovalLateAuthenticationCannotApproveAfterCancellationOrReplacement(t *testing.T) {
	for _, system := range []bool{false, true} {
		for _, change := range []string{"password", "cancel", "close", "request", "nonce", "category", "context"} {
			t.Run(change+map[bool]string{true: "-system", false: "-password"}[system], func(t *testing.T) {
				h := newApprovalAuthHarness(t)
				ctx, cancel := context.WithCancel(context.Background())
				defer cancel()
				h.state.flow.context = ctx
				h.state.password = "offline-synthetic-password"
				h.state.start(h.request, true, system, false)
				awaitApprovalValue(t, h.started)
				switch change {
				case "password":
					h.state.expandPassword()
				case "cancel":
					h.state.stopAttempt()
				case "close":
					h.state.dispose()
				case "request":
					h.current = false
				case "nonce":
					h.state.requestKey = h.request.ID + "\x00new-generation"
				case "category":
					a := &app{inlineApproval: h.state}
					a.selectApprovalCategory(1)
				case "context":
					cancel()
				}
				owner := generatedApprovalOwner(t)
				h.results <- approvalAuthResult{owner: owner}
				awaitApprovalValue(t, h.ui)()
				select {
				case <-h.submitted:
					t.Fatal("obsolete authentication submitted an approval")
				default:
				}
				if _, err := owner.Proof([]byte("synthetic-test")); err == nil {
					t.Fatal("obsolete owner session was not immediately closed")
				}
				if h.done != 0 || h.state.status != "" {
					t.Fatal("late authentication updated replacement state")
				}
			})
		}
	}
}

// approvalReady is a request that may authenticate right now.
func approvalReady() authReadiness {
	return authReadiness{windowReady: true, visible: true, focused: true, active: true, systemReady: true}
}

func TestApprovalAutomaticAttemptRequiresActivationAndDoesNotRetryFailures(t *testing.T) {
	h := newApprovalAuthHarness(t)
	post := func(fn func()) { h.ui <- fn }
	current := func() bool { return h.current }
	for _, readiness := range []authReadiness{
		{windowReady: false, visible: true, focused: true, active: true, systemReady: true},
		{windowReady: true, visible: false, focused: true, active: true, systemReady: true},
		{windowReady: true, visible: true, focused: false, active: true, systemReady: true},
		{windowReady: true, visible: true, focused: true, active: false, systemReady: true},
		{windowReady: true, visible: true, focused: true, active: true, systemReady: false},
	} {
		h.state.queueAutomatic(h.request, readiness, time.Now, post, current)
	}
	select {
	case <-h.ui:
		// A window that is key while the application is not active is the
		// reported start-up failure: the native view cannot evaluate yet.
		t.Fatal("hidden, unfocused, inactive, unready or unavailable authentication was queued")
	default:
	}
	h.state.queueAutomatic(h.request, approvalReady(), time.Now, post, current)
	h.state.queueAutomatic(h.request, approvalReady(), time.Now, post, current)
	awaitApprovalValue(t, h.ui)()
	if !awaitApprovalValue(t, h.started) || !h.state.autoAttempted {
		t.Fatal("visible focused request did not start its single automatic attempt")
	}
	h.results <- approvalAuthResult{err: errors.New("synthetic cancellation")}
	awaitApprovalValue(t, h.ui)()
	if h.state.busy || h.state.status == "" {
		t.Fatal("failure did not settle the request")
	}
	for range 4 {
		h.state.queueAutomatic(h.request, approvalReady(), time.Now, post, current)
	}
	select {
	case <-h.ui:
		t.Fatal("failed authentication automatically retried")
	default:
	}
}

func TestApprovalAutomaticReadinessInterruptionRetriesOnce(t *testing.T) {
	h := newApprovalAuthHarness(t)
	post := func(fn func()) { h.ui <- fn }
	current := func() bool { return h.current }
	notReady := &systemauth.EmbeddedError{Code: "notReady", Message: "窗口内指纹验证区域尚未就绪"}
	h.state.queueAutomatic(h.request, approvalReady(), time.Now, post, current)
	awaitApprovalValue(t, h.ui)()
	awaitApprovalValue(t, h.started)
	h.results <- approvalAuthResult{err: notReady}
	awaitApprovalValue(t, h.ui)()
	if h.state.busy || h.state.status != "" || !h.state.autoRetryUsed || h.state.autoAttempted || h.state.autoRetryAt.IsZero() {
		t.Fatal("a system readiness interruption was reported as a failure instead of being retried")
	}
	h.state.queueAutomatic(h.request, approvalReady(), time.Now, post, current)
	select {
	case <-h.ui:
		t.Fatal("authentication restarted before the readiness window elapsed")
	default:
	}
	retryAt := h.state.autoRetryAt
	h.state.queueAutomatic(h.request, approvalReady(), func() time.Time { return retryAt }, post, current)
	awaitApprovalValue(t, h.ui)()
	awaitApprovalValue(t, h.started)
	h.results <- approvalAuthResult{err: notReady}
	awaitApprovalValue(t, h.ui)()
	if h.state.status == "" {
		t.Fatal("the second readiness interruption was retried or hidden")
	}
	h.state.queueAutomatic(h.request, approvalReady(), func() time.Time { return retryAt.Add(time.Hour) }, post, current)
	select {
	case <-h.ui:
		t.Fatal("a readiness interruption retried more than once")
	default:
	}
}

func TestApprovalAutomaticCancellationStaysSilent(t *testing.T) {
	h := newApprovalAuthHarness(t)
	post := func(fn func()) { h.ui <- fn }
	h.state.queueAutomatic(h.request, approvalReady(), time.Now, post, func() bool { return true })
	awaitApprovalValue(t, h.ui)()
	awaitApprovalValue(t, h.started)
	h.results <- approvalAuthResult{err: context.Canceled}
	awaitApprovalValue(t, h.ui)()
	if h.state.status != "" || h.state.autoRetryUsed || h.state.busy {
		t.Fatal("cancellation was reported as an authority failure or retried")
	}
}

func TestApprovalQueuedAuthenticationRechecksFocusAndPasswordChoice(t *testing.T) {
	for _, change := range []string{"focus", "password", "close"} {
		h := newApprovalAuthHarness(t)
		h.state.queueAutomatic(h.request, approvalReady(), time.Now, func(fn func()) { h.ui <- fn }, func() bool { return h.current })
		switch change {
		case "focus":
			h.current = false
		case "password":
			h.state.expandPassword()
		case "close":
			h.state.dispose()
		}
		awaitApprovalValue(t, h.ui)()
		select {
		case <-h.started:
			t.Fatal("queued authentication ignored lifecycle change", change)
		default:
		}
	}
}

func TestApprovalRendererAndHeadlessCenterNeverAuthenticate(t *testing.T) {
	a, r := reviewFixture("full")
	a.authStatus = systemauth.Status{Enabled: true, Available: true, Name: "Touch ID"}
	h := newApprovalAuthHarness(t)
	var attempts atomic.Int32
	h.state.flow.authenticate = func(context.Context, []byte, bool) (*vault.Session, error) {
		attempts.Add(1)
		return nil, errors.New("must not run")
	}
	tt := ui.NewTester(func(c *ui.Context) { a.approvalPrompt(c, r, h.state, func(bool, bool) {}, func() {}) }, 420, 540)
	for range 4 {
		tt.Frame()
	}
	a.requests = []approval.Request{r}
	a.inlineApproval = h.state
	a.queueInlineApproval()
	center := ui.NewTester(a.approvalView, 560, 760)
	for range 4 {
		center.Frame()
		a.queueInlineApproval()
	}
	if attempts.Load() != 0 || h.state.autoAttempted {
		t.Fatal("pure rendering invoked authentication")
	}
}

func TestApprovalAutomaticClaimIsOncePerRequestGenerationAcrossViews(t *testing.T) {
	a, r := reviewFixture("full")
	a.bindApprovalFlow(r, &approvalPromptState{}, func(func()) {}, func() bool { return true }, func() {})
	first, second, renewal := &approvalPromptState{}, &approvalPromptState{}, &approvalPromptState{}
	a.bindApprovalFlow(r, first, func(func()) {}, func() bool { return true }, func() {})
	a.bindApprovalFlow(r, second, func(func()) {}, func() bool { return true }, func() {})
	if !first.flow.claimAuto() || second.flow.claimAuto() {
		t.Fatal("same generation could automatically authenticate twice")
	}
	r.Nonce = "renewed-generation"
	a.bindApprovalFlow(r, renewal, func(func()) {}, func() bool { return true }, func() {})
	if !renewal.flow.claimAuto() {
		t.Fatal("new generation lost its automatic attempt")
	}
}

func TestApprovalBusyRejectionCancelsAuthenticationInOneAction(t *testing.T) {
	a, r := reviewFixture("full")
	a.authStatus = systemauth.Status{Enabled: true, Available: true, Name: "Touch ID"}
	h := newApprovalAuthHarness(t)
	var denied atomic.Int32
	h.state.flow.denyOwner = func() (*vault.Session, error) { return generatedApprovalOwner(t), nil }
	h.state.flow.submit = func(_ context.Context, reviewed approval.Request, allow bool, _ *vault.Session) error {
		if allow {
			t.Error("cancelled authentication approved a request")
		} else {
			denied.Add(1)
		}
		h.submitted <- reviewed
		return nil
	}
	r = h.request
	tt := ui.NewTester(func(c *ui.Context) {
		a.approvalPrompt(c, r, h.state, func(allow, system bool) { h.state.start(r, allow, system, false) }, func() {})
	}, 380, 540)
	h.state.start(r, true, true, false)
	awaitApprovalValue(t, h.started)
	tt.Frame()
	if err := tt.Click("不允许"); err != nil {
		t.Fatal(err)
	}
	if !h.state.submitting {
		t.Fatal("one-click rejection did not start the denial")
	}
	if err := tt.Click("不允许"); err != nil {
		t.Fatal(err)
	}
	awaitApprovalValue(t, h.submitted)
	awaitApprovalValue(t, h.ui)()
	owner := generatedApprovalOwner(t)
	h.results <- approvalAuthResult{owner: owner}
	awaitApprovalValue(t, h.ui)()
	if denied.Load() != 1 {
		t.Fatal("rejection submitted more than once")
	}
	select {
	case <-h.submitted:
		t.Fatal("late authentication sent a second decision")
	default:
	}
	if _, err := owner.Proof([]byte("late-success")); err == nil {
		t.Fatal("late owner session was retained")
	}
}

func TestApprovalCenterEscapeCancelsVerificationAndKeepsExpandedPassword(t *testing.T) {
	a, r := reviewFixture("full")
	a.authStatus = systemauth.Status{Enabled: true, Available: true, Name: "Touch ID"}
	a.requests = []approval.Request{r}
	tt := ui.NewTester(a.approvalView, 560, 760)
	if err := tt.Click("授权方式：触控 ID"); err != nil {
		t.Fatal(err)
	}
	var cancelled int
	a.inlineApproval.busy, a.inlineApproval.cancel = true, func() { cancelled++ }
	tt.Frame()
	tt.Key(0, ui.KeyEscape)
	if cancelled != 1 || a.inlineApproval.busy || !a.inlineApproval.passwordMode {
		t.Fatal("inline Escape did not cancel while retaining password mode")
	}
	if _, ok := tt.Find("保险库主密码"); !ok {
		t.Fatal("inline cancellation hid the password field")
	}
}

func TestApprovalAutomaticWaitsForEmbeddedAnchorWithoutSpendingAttempt(t *testing.T) {
	h := newApprovalAuthHarness(t)
	h.state.presentation = systemauth.NewEmbeddedPresentation(func(fn func()) { fn() })
	post := func(fn func()) { h.ui <- fn }
	current := func() bool { return true }
	h.state.queueAutomatic(h.request, approvalReady(), time.Now, post, current)
	if h.state.autoQueued || h.state.autoAttempted {
		t.Fatal("an unlaid-out native anchor spent the automatic attempt")
	}
	select {
	case <-h.ui:
		t.Fatal("empty anchor queued authentication")
	default:
	}
	h.state.presentation.SetFrame(1, systemauth.EmbeddedRect{X: 8, Y: 12, Width: 24, Height: 24})
	h.state.queueAutomatic(h.request, approvalReady(), time.Now, post, current)
	callback := awaitApprovalValue(t, h.ui)
	// A page can disappear after queueing but before the callback executes.
	h.state.presentation.SetFrame(0, systemauth.EmbeddedRect{})
	callback()
	if h.state.autoAttempted || h.state.busy {
		t.Fatal("detached native anchor began authentication")
	}
	h.state.presentation.SetFrame(1, systemauth.EmbeddedRect{X: 8, Y: 12, Width: 24, Height: 24})
	h.state.queueAutomatic(h.request, approvalReady(), time.Now, post, current)
	awaitApprovalValue(t, h.ui)()
	awaitApprovalValue(t, h.started)
	if !h.state.autoAttempted || !h.state.systemAttempt {
		t.Fatal("ready native anchor did not start exactly one system attempt")
	}
	h.state.dispose()
	owner := generatedApprovalOwner(t)
	h.results <- approvalAuthResult{owner: owner}
	awaitApprovalValue(t, h.ui)()
	if h.state.presentation.Ready() {
		t.Fatal("disposed approval retained its native host")
	}
	if _, err := owner.Proof([]byte("discarded-native-result")); err == nil {
		t.Fatal("late native result retained an owner session")
	}
}

func TestApprovalTypingPasswordCancelsEmbeddedTouchIDInPlace(t *testing.T) {
	a, r := reviewFixture("read")
	a.authStatus = systemauth.Status{Enabled: true, Available: true, Name: "Touch ID"}
	cancelled, passwordApprovals := 0, 0
	state := &approvalPromptState{
		passwordMode: true, busy: true, systemAttempt: true,
		presentation: systemauth.NewEmbeddedPresentation(func(fn func()) { fn() }),
		cancel:       func() { cancelled++ },
	}
	tt := ui.NewTester(func(c *ui.Context) {
		a.approvalPrompt(c, r, state, func(allow, system bool) {
			if allow && !system {
				passwordApprovals++
			}
		}, func() {})
	}, 420, 540)
	if err := tt.Click("保险库主密码"); err != nil {
		t.Fatal(err)
	}
	tt.Type("isolated-password-fallback")
	if cancelled != 1 || state.busy || !state.passwordMode || state.password != "isolated-password-fallback" {
		t.Fatal("typing required a separate biometric cancellation or lost the password")
	}
	tt.Key(0, ui.KeyEnter)
	if passwordApprovals != 1 {
		t.Fatal("password did not submit after an in-place biometric cancellation")
	}
}
