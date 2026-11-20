package desktop

import (
	"fmt"
	"time"

	"sshclient/internal/approval"
)

// Notification grouping never grants authority. Unshown requests remain
// pending in the authorization center until an owner explicitly decides.
func approvalNoticeKey(r approval.Request) string {
	if r.Nonce == "" {
		return r.ID
	}
	return r.ID + ":" + r.Nonce
}

func (a *app) markApprovalHandled(r approval.Request) {
	if a.notified == nil {
		a.notified = make(map[string]bool)
	}
	a.notified[approvalNoticeKey(r)] = true
	a.wakePoll()
}

// nextApprovalPopup returns the next pending request that deserves the owner's
// attention. Every request that needs a decision qualifies: a one-shot exec,
// a file operation, a new session, or a pending child. One window is shown at
// a time; the rest stay queued and are surfaced after it closes. Nothing is
// approved here, and a request marked as shown is not repeated if the owner
// dismisses it (it remains visible in the authorization center).
func (a *app) nextApprovalPopup(now time.Time) (approval.Request, bool) {
	if !a.online || !a.startup.mayPopup() || a.presence.backgrounded || a.connection.phase != "" || a.prompt.open || a.inlineApproval != nil || a.approvalWin != nil {
		return approval.Request{}, false
	}
	if a.notified == nil {
		a.notified = make(map[string]bool)
	}
	for _, r := range a.requests {
		if r.Status != "pending" || r.OwnerIssued || a.notified[approvalNoticeKey(r)] || !now.Before(r.Expires) {
			continue
		}
		if r.Operation.Kind == "grant" && r.Operation.Grant.Validate() != nil {
			continue
		}
		a.notified[approvalNoticeKey(r)] = true
		return r, true
	}
	return approval.Request{}, false
}

// approvalWindowTitle keeps the queue visible without taking room from the
// request itself: the title says how many more prompts will follow.
func (a *app) approvalWindowTitle(r approval.Request) string {
	if remaining := a.otherPendingApprovals(r.ID); remaining > 0 {
		return fmt.Sprintf(tr(nil, "服务器操作授权 · 还有 %d 条待处理"), remaining)
	}
	return tr(nil, "服务器操作授权")
}

// otherPendingApprovals counts requests that will still get their own popup.
// A request the owner dismissed stays pending in the center but is not counted
// again here.
func (a *app) otherPendingApprovals(id string) int {
	pending := 0
	for _, r := range a.requests {
		if r.ID != id && r.Status == "pending" && !r.OwnerIssued && !a.notified[approvalNoticeKey(r)] {
			pending++
		}
	}
	return pending
}
