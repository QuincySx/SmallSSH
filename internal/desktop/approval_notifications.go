package desktop

import (
	"time"

	"sshclient/internal/approval"
)

// Notification grouping never grants authority. Unshown requests remain
// pending in the authorization center until an owner explicitly decides.
const approvalPopupCooldown = time.Minute

func approvalNoticeKey(r approval.Request) string {
	if r.Nonce == "" {
		return r.ID
	}
	return r.ID + ":" + r.Nonce
}

func (a *app) nextApprovalPopup(now time.Time) (approval.Request, bool) {
	if !a.online || !a.startup.mayPopup() || a.connection.phase == "save" || a.connection.phase == "connect" {
		return approval.Request{}, false
	}
	if a.notified == nil {
		a.notified = make(map[string]bool)
	}
	var first approval.Request
	for _, r := range a.requests {
		// Routine one-shot requests and delegated children stay quiet. Only a
		// new client session is eligible for an automatic owner prompt.
		if r.Status != "pending" || r.OwnerIssued || r.ParentID != "" || r.Operation.Kind != "grant" || a.notified[approvalNoticeKey(r)] || !now.Before(r.Expires) || r.Operation.Grant.Validate() != nil {
			continue
		}
		a.notified[approvalNoticeKey(r)] = true
		if first.ID == "" {
			first = r
		}
	}
	// Consume the batch even while a prompt is open or cooling down. Closing
	// one dialog must not reveal a chain of delayed popups on the next ticks.
	if first.ID == "" || a.approvalWin != nil || !a.lastApprovalPopup.IsZero() && now.Before(a.lastApprovalPopup.Add(approvalPopupCooldown)) {
		return approval.Request{}, false
	}
	a.lastApprovalPopup = now
	return first, true
}
