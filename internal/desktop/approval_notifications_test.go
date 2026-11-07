package desktop

import (
	"testing"
	"time"

	"strings"

	"github.com/egoist/mygo"
	"sshclient/internal/approval"
	"sshclient/internal/broker"
	"sshclient/internal/client"
	"sshclient/internal/vault"
)

func popupFixture() (*app, approval.Request, time.Time) {
	a, r := reviewFixture("full")
	now := time.Now()
	r.Expires = now.Add(10 * time.Minute)
	a.online = true
	a.startup = newStartup(false)
	return a, r, now
}

func TestEveryPendingRequestCanPrompt(t *testing.T) {
	for _, kind := range []string{"exec", "shell", "list", "get", "put", "monitor", "docker-logs", "docker-terminal", "compose-update", "grant"} {
		a, r, now := popupFixture()
		r.Operation.Kind = kind
		if kind != "grant" {
			r.Operation.Grant = approval.GrantScope{}
		}
		a.requests = []approval.Request{r}
		if got, ok := a.nextApprovalPopup(now); !ok || got.ID != r.ID {
			t.Fatal("pending request did not prompt", kind)
		}
		if _, ok := a.nextApprovalPopup(now.Add(time.Second)); ok {
			t.Fatal("the same request prompted twice", kind)
		}
	}
}

func TestNonActionableRequestsStayQuiet(t *testing.T) {
	for _, condition := range []string{"owner", "approved", "expired", "invalid-key"} {
		a, r, now := popupFixture()
		switch condition {
		case "owner":
			r.OwnerIssued = true
		case "approved":
			r.Status = "approved"
		case "expired":
			r.Expires = now
		case "invalid-key":
			r.Operation.Grant.PublicKey = "caller-name-is-not-a-key"
		}
		a.requests = []approval.Request{r}
		if _, ok := a.nextApprovalPopup(now); ok {
			t.Fatal("non-actionable request prompted", condition)
		}
	}
	a, r, now := popupFixture()
	r.ParentID = "existing-session"
	a.requests = []approval.Request{r}
	if got, ok := a.nextApprovalPopup(now); !ok || got.ID != r.ID {
		t.Fatal("a pending child that needs a decision did not prompt")
	}
}

func TestPopupQueueShowsOneAtATimeAndKeepsTheRest(t *testing.T) {
	a, first, now := popupFixture()
	second, third := first, first
	second.ID, third.ID = "second", "third"
	a.requests = []approval.Request{first, second, third}

	if got, ok := a.nextApprovalPopup(now); !ok || got.ID != first.ID {
		t.Fatal("first request did not prompt")
	}
	// Another window is open: the rest must stay queued, not be consumed.
	a.approvalWin = &mygo.Window{}
	if _, ok := a.nextApprovalPopup(now.Add(time.Second)); ok {
		t.Fatal("a second dialog was stacked")
	}
	if a.notified[approvalNoticeKey(second)] || a.notified[approvalNoticeKey(third)] {
		t.Fatal("queued requests were marked as shown")
	}
	a.approvalWin = nil
	if got, ok := a.nextApprovalPopup(now.Add(2 * time.Second)); !ok || got.ID != second.ID {
		t.Fatal("second request did not follow the first")
	}
	a.approvalWin = &mygo.Window{}
	if _, ok := a.nextApprovalPopup(now.Add(3 * time.Second)); ok {
		t.Fatal("a second dialog was stacked")
	}
	a.approvalWin = nil
	if got, ok := a.nextApprovalPopup(now.Add(4 * time.Second)); !ok || got.ID != third.ID {
		t.Fatal("third request did not follow the second")
	}
	if _, ok := a.nextApprovalPopup(now.Add(5 * time.Second)); ok {
		t.Fatal("a decided, dismissed, or already shown request repeated")
	}
}

func TestDismissedRequestDoesNotRepeat(t *testing.T) {
	a, r, now := popupFixture()
	r.Operation.Kind = "exec"
	r.Operation.Grant = approval.GrantScope{}
	a.requests = []approval.Request{r}
	if _, ok := a.nextApprovalPopup(now); !ok {
		t.Fatal("request did not prompt")
	}
	// Dismissing the window without deciding leaves the request pending and in
	// the authorization center; it must not open the same popup on every tick.
	if _, ok := a.nextApprovalPopup(now.Add(10 * time.Minute)); ok {
		t.Fatal("a dismissed request prompted again")
	}
}

func TestHiddenAndBusyFrontendsDoNotConsumeRequests(t *testing.T) {
	for _, blocked := range []string{"hidden", "locked", "saving", "connecting", "picker", "prompt", "inline"} {
		a, r, now := popupFixture()
		a.requests = []approval.Request{r}
		switch blocked {
		case "hidden":
			a.startup = newStartup(true)
		case "locked":
			a.online = false
		case "saving":
			a.connection.phase = "save"
		case "connecting":
			a.connection.phase = "connect"
		case "picker":
			a.connection.phase = "picker"
		case "prompt":
			a.prompt.open = true
		case "inline":
			a.inlineApproval = &approvalPromptState{}
		}
		if _, ok := a.nextApprovalPopup(now); ok || a.approvalWin != nil || a.notified[r.ID] {
			t.Fatal("blocked frontend touched notification", blocked)
		}
		a.online, a.startup.revealed, a.connection.phase = true, true, ""
		a.prompt.open, a.inlineApproval = false, nil
		if _, ok := a.nextApprovalPopup(now); !ok {
			t.Fatal("blocked request was silently consumed", blocked)
		}
	}
}

func TestApprovalWindowTitleShowsRemainingRequests(t *testing.T) {
	a, r := reviewFixture("full")
	r.Status = "pending"
	other := r
	other.ID = "second-pending"
	a.requests = []approval.Request{r, other}
	if title := a.approvalWindowTitle(r); !strings.Contains(title, "1") {
		t.Fatal("the title hid the rest of the queue", title)
	}
}

func TestAutoApprovedDelegatedOperationsStayQuietButPendingOnesPrompt(t *testing.T) {
	h := newConnectionFlowHarness(t)
	if err := h.a.client.SaveConnection(h.ctx, vault.ProfileChange{Profile: h.fixture.Profile()}, h.server.Vault); err != nil {
		t.Fatal(err)
	}
	parent, delegation, err := h.a.client.RequestSession(h.ctx, "fixture", "same AI label", "full", time.Hour, 15*time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	defer delegation.Close()
	if err = h.a.client.DecideWithOwner(h.ctx, parent.ID, true, false, h.server.Vault); err != nil {
		t.Fatal(err)
	}
	delegated := client.Client{Dir: h.dir, Delegation: delegation}
	for i := 0; i < 5; i++ {
		r, _, err := delegated.Request(h.ctx, approval.Operation{Host: "fixture", Client: "same AI label", Kind: "list", Path: "/"}, time.Minute)
		if err != nil || r.Status != "approved" || r.ParentID != parent.ID {
			t.Fatal("ordinary delegated operation asked again", err, r.Status)
		}
	}
	stranger, ticket, err := h.a.client.Request(h.ctx, approval.Operation{Host: "fixture", Client: "same AI label", Kind: "exec", Command: "not-authorized"}, time.Minute)
	if err != nil || stranger.Status != "pending" {
		t.Fatal("another caller inherited the session", err)
	}
	pending, err := h.a.client.Call(h.ctx, broker.Message{Action: "pending"})
	if err != nil {
		t.Fatal(err)
	}
	h.a.requests = pending.Requests
	h.a.startup = newStartup(false)
	if got, ok := h.a.nextApprovalPopup(time.Now()); !ok || got.ID != stranger.ID {
		t.Fatal("a pending one-shot did not prompt", got.ID, ok)
	}
	if stream, err := h.a.client.Start(h.ctx, stranger.ID, ticket, 0, 0); err == nil {
		stream.Close()
		t.Fatal("prompting was treated as approval")
	}
}
