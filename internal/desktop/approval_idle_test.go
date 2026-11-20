package desktop

import (
	"testing"

	"github.com/egoist/mygo/ui"
	"sshclient/internal/approval"
)

func TestApprovalIdleChoicesPersistAcrossViewsAndNonce(t *testing.T) {
	a, r := reviewFixture("full")
	r.Nonce = "first"
	r.Operation.Grant.IdleSeconds = 20 * 60
	state := &approvalPromptState{}
	tt := ui.NewTester(func(c *ui.Context) {
		a.initializeApprovalGrant(r, state)
		a.approvalGrantChoices(c, r, state)
	}, 420, 350)
	if state.chosenIdle(r) != 15*60 || !tt.HasText("15 分钟") {
		t.Fatal("CLI suggestion replaced owner default", tt.Texts())
	}
	if err := tt.Click("会话闲置时长"); err != nil {
		t.Fatal(err)
	}
	if err := tt.Click("5 分钟"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if state.chosenIdle(r) != 5*60 {
		t.Fatal("selection was not retained")
	}
	if err := tt.Click("会话闲置时长"); err != nil {
		t.Fatal(err)
	}
	if err := tt.Click("20 分钟"); err != nil {
		t.Fatal(err)
	}
	if state.chosenIdle(r) != 20*60 {
		t.Fatal("20 minute selection not applied")
	}
	if err := tt.Click("会话闲置自动断开开关"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if !state.noIdle || state.chosenIdle(r) != state.chosenLifetime(r) || !tt.HasText("闲置不断开") {
		t.Fatal("disabled idle terms disagree")
	}
	a.requests = []approval.Request{r}
	center := ui.NewTester(a.approvalView, 1000, 760)
	other := a.inlineApproval
	if other == nil || !other.noIdle || other.idleSeconds != 20*60 || !center.HasText("闲置不断开") {
		t.Fatal("center view lost choices")
	}
	r.Nonce = "fresh"
	a.requests = []approval.Request{r}
	center.Frame()
	other = a.inlineApproval
	if other == nil || other.noIdle || other.chosenIdle(r) != 15*60 {
		t.Fatal("fresh nonce inherited choices")
	}
}

func TestApprovalIdleOffPreferenceAndReview(t *testing.T) {
	a, r := reviewFixture("read")
	r.Operation.Grant.IdleSeconds = 20 * 60
	no := false
	a.behavior.SessionIdleEnabled = &no
	state := &approvalPromptState{}
	a.initializeApprovalGrant(r, state)
	if !state.noIdle || state.chosenIdle(r) != state.chosenLifetime(r) {
		t.Fatal("disabled default not reflected in terms")
	}
	tt := ui.NewTester(func(c *ui.Context) { requestReviewWithTerms(c, r, state.chosenLifetime(r), state.chosenIdle(r), true) }, 650, 800)
	if !tt.HasText("最长 2 小时 · 闲置不断开") {
		t.Fatal("review did not show disabled idle", tt.Texts())
	}
}

func TestRenewalUsesPreviouslyApprovedTermsInsteadOfNewSessionDefault(t *testing.T) {
	a, r := reviewFixture("full")
	r.Renewal = true
	r.Nonce = "renewal"
	for _, idle := range []int{20 * 60, 2 * 60 * 60} {
		r.Operation.Grant.IdleSeconds = idle
		state := &approvalPromptState{}
		a.initializeApprovalGrant(r, state)
		if state.chosenLifetime(r) != 2*60*60 || state.chosenIdle(r) != idle || state.noIdle != (idle == 2*60*60) {
			t.Fatalf("renewal did not retain approved terms: %+v", state)
		}
	}
}
