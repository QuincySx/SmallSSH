package desktop

import (
	"testing"
	"time"

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

func TestOnlyNewClientSessionsCanAutomaticallyPrompt(t *testing.T) {
	for _, kind := range []string{"exec", "shell", "list", "get", "put", "monitor", "docker-logs", "docker-terminal", "compose-update"} {
		a, r, now := popupFixture()
		r.Operation.Kind = kind
		r.Operation.Grant = approval.GrantScope{}
		a.requests = []approval.Request{r}
		if _, ok := a.nextApprovalPopup(now); ok || r.Status != "pending" {
			t.Fatal("routine request opened a dialog or was approved", kind)
		}
	}
	for _, condition := range []string{"owner", "child", "approved", "expired", "invalid-key"} {
		a, r, now := popupFixture()
		switch condition {
		case "owner":
			r.OwnerIssued = true
		case "child":
			r.ParentID = "existing-session"
		case "approved":
			r.Status = "approved"
		case "expired":
			r.Expires = now
		case "invalid-key":
			r.Operation.Grant.PublicKey = "caller-name-is-not-a-key"
		}
		a.requests = []approval.Request{r}
		if _, ok := a.nextApprovalPopup(now); ok {
			t.Fatal("non-new session prompted", condition)
		}
	}
	a, r, now := popupFixture()
	a.requests = []approval.Request{r}
	if got, ok := a.nextApprovalPopup(now); !ok || got.ID != r.ID {
		t.Fatal("new session request was lost")
	}
	if _, ok := a.nextApprovalPopup(now.Add(2 * time.Minute)); ok {
		t.Fatal("same request prompted twice")
	}
}

func TestApprovalRequestsAreGroupedWithoutDelayedPopupChains(t *testing.T) {
	a, first, now := popupFixture()
	second, third := first, first
	second.ID, third.ID = "second", "third"
	a.requests = []approval.Request{first, second, third}
	if r, ok := a.nextApprovalPopup(now); !ok || r.ID != first.ID {
		t.Fatal("first batch did not notify")
	}
	if _, ok := a.nextApprovalPopup(now.Add(2 * time.Minute)); ok {
		t.Fatal("closing dialog exposed another request from the same batch")
	}
	for _, r := range a.requests {
		if r.Status != "pending" {
			t.Fatal("grouping silently decided a request")
		}
	}
	// A fresh request during cooldown remains available in the center, but
	// will not pop up later while the owner is doing something else.
	cooling := first
	cooling.ID = "cooling"
	a.requests = append(a.requests, cooling)
	if _, ok := a.nextApprovalPopup(now.Add(time.Second)); ok {
		t.Fatal("cooldown did not suppress repeated prompt")
	}
	if _, ok := a.nextApprovalPopup(now.Add(2 * time.Minute)); ok {
		t.Fatal("cooldown merely delayed the popup")
	}
	busy := first
	busy.ID = "while-another-dialog-is-open"
	a.requests = append(a.requests, busy)
	a.approvalWin = &mygo.Window{}
	if _, ok := a.nextApprovalPopup(now.Add(3 * time.Minute)); ok {
		t.Fatal("stacked native dialogs")
	}
	a.approvalWin = nil
	if _, ok := a.nextApprovalPopup(now.Add(4 * time.Minute)); ok {
		t.Fatal("queued a delayed native dialog")
	}
	newRequest := first
	newRequest.ID = "a-new-session-later"
	a.requests = append(a.requests, newRequest)
	if r, ok := a.nextApprovalPopup(now.Add(5 * time.Minute)); !ok || r.ID != newRequest.ID {
		t.Fatal("later explicit session request could not notify")
	}
}

func TestHiddenAndBusyDevelopmentDoesNotConsumeOrOpenRequests(t *testing.T) {
	for _, blocked := range []string{"hidden", "locked", "saving", "connecting"} {
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
		}
		if _, ok := a.nextApprovalPopup(now); ok || a.approvalWin != nil || a.notified[r.ID] {
			t.Fatal("blocked frontend touched notification", blocked)
		}
		a.online, a.startup.revealed, a.connection.phase = true, true, ""
		if _, ok := a.nextApprovalPopup(now); !ok {
			t.Fatal("blocked request was silently consumed", blocked)
		}
	}
}

func TestDelegatedOperationsStayQuietWithoutAuthorizingNamesakes(t *testing.T) {
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
	if _, ok := h.a.nextApprovalPopup(time.Now()); ok {
		t.Fatal("child or single operation opened another approval dialog")
	}
	if stream, err := h.a.client.Start(h.ctx, stranger.ID, ticket, 0, 0); err == nil {
		stream.Close()
		t.Fatal("quiet request was treated as approved")
	}
}
