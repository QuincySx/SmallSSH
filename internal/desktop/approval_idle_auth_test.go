package desktop

import (
	"testing"
	"time"

	"github.com/egoist/mygo/ui"
)

func TestApprovalIdleChangeCancelsBusyAuthentication(t *testing.T) {
	h := newApprovalAuthHarness(t)
	a, _ := reviewFixture("full")
	r := h.request
	a.initializeApprovalGrant(r, h.state)
	tt := ui.NewTester(func(c *ui.Context) { a.approvalGrantChoices(c, r, h.state) }, 420, 350)
	h.state.start(r, true, true, false)
	awaitApprovalValue(t, h.started)
	seq := h.state.sequence
	if err := tt.Click("会话闲置自动断开开关"); err != nil {
		t.Fatal(err)
	}
	if h.state.busy || !h.state.noIdle || h.state.sequence <= seq || !h.state.autoAttempted || !a.approvalAutoAttempted[approvalRequestKey(r)] {
		t.Fatal("change did not cancel and suppress automatic verification")
	}
	h.results <- approvalAuthResult{owner: generatedApprovalOwner(t)}
	awaitApprovalValue(t, h.ui)()
	select {
	case <-h.submitted:
		t.Fatal("late authentication submitted")
	default:
	}
	h.state.submitting = true
	tt.Frame()
	_ = tt.Click("会话闲置自动断开开关")
	if !h.state.noIdle {
		t.Fatal("submitting allowed terms change")
	}
}

func TestApprovalQueuedAutomaticSuppressedAcrossViewSwitch(t *testing.T) {
	h := newApprovalAuthHarness(t)
	a, _ := reviewFixture("full")
	r := h.request
	a.initializeApprovalGrant(r, h.state)
	h.state.queueAutomatic(r, approvalReady(), time.Now, func(fn func()) { h.ui <- fn }, func() bool { return true })
	queued := awaitApprovalValue(t, h.ui)
	tt := ui.NewTester(func(c *ui.Context) { a.approvalGrantChoices(c, r, h.state) }, 420, 350)
	if err := tt.Click("会话闲置自动断开开关"); err != nil {
		t.Fatal(err)
	}
	queued()
	select {
	case <-h.started:
		t.Fatal("queued attempt started after terms change")
	default:
	}
	other := &approvalPromptState{}
	a.initializeApprovalGrant(r, other)
	if !other.noIdle {
		t.Fatal("view did not recover modified terms")
	}
	a.bindApprovalFlow(r, other, func(fn func()) { h.ui <- fn }, func() bool { return true }, func() {})
	other.queueAutomatic(r, approvalReady(), time.Now, func(fn func()) { h.ui <- fn }, func() bool { return true })
	awaitApprovalValue(t, h.ui)()
	if other.busy {
		t.Fatal("replacement view restarted automatic verification")
	}
	// Explicit confirmation on the replacement view uses a synthetic authenticator.
	h.state.dispose()
	other.flow.authenticate = h.state.flow.authenticate
	other.flow.submit = h.state.flow.submit
	other.start(r, true, true, false)
	awaitApprovalValue(t, h.started)
	h.results <- approvalAuthResult{owner: generatedApprovalOwner(t)}
	awaitApprovalValue(t, h.ui)()
	awaitApprovalValue(t, h.submitted)
}
