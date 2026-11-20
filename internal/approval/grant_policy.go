package approval

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"time"

	"sshclient/internal/operationrisk"
)

// Activity is display metadata. It is deliberately excluded from decision and
// revoke proofs: using a grant must never invalidate an already displayed proof.
type GrantActivity struct {
	ApprovedAt   time.Time `json:"approvedAt"`
	LastUsed     time.Time `json:"lastUsed"`
	IdleSeconds  int       `json:"idleSeconds"`
	MediumReview *bool     `json:"mediumReview,omitempty"`
}

type GrantPolicyChange struct {
	ID           string `json:"id"`
	Nonce        string `json:"nonce"`
	IdleSeconds  int    `json:"idleSeconds"`            // zero disables early idle expiry
	MediumReview *bool  `json:"mediumReview,omitempty"` // when present, only edit file confirmation
}

func (r Request) Generation() string {
	if r.Nonce == "" {
		return ""
	}
	digest := sha256.Sum256([]byte(r.Nonce))
	return hex.EncodeToString(digest[:])
}

func (r Request) ConfirmsMediumReview() bool {
	if r.Activity != nil && r.Activity.MediumReview != nil {
		return *r.Activity.MediumReview
	}
	return !r.Operation.Grant.SkipMediumReview
}

func (e *entry) confirmsMediumReview() bool {
	if e.mediumReviewOverride != nil {
		return *e.mediumReviewOverride
	}
	return !e.request.Operation.Grant.SkipMediumReview
}

func (e *entry) idleSeconds() int {
	if e.idleOverride != nil {
		return *e.idleOverride
	}
	if e.request.Operation.Grant.IdleSeconds == e.request.Operation.Grant.LifetimeSeconds {
		return 0
	}
	return e.request.Operation.Grant.IdleSeconds
}

func (e *entry) snapshot() Request {
	r := e.request
	if r.Operation.Kind == "grant" && !e.approvedAt.IsZero() {
		r.Activity = &GrantActivity{ApprovedAt: e.approvedAt, LastUsed: e.lastUsed, IdleSeconds: e.idleSeconds()}
		if r.Operation.Grant.Permission == "guarded" {
			review := e.confirmsMediumReview()
			r.Activity.MediumReview = &review
		}
	}
	return r
}

// SetGrantPolicy cannot extend the absolute deadline or revive an expired grant.
// Tightening the idle rule uses the actual last activity, not the edit time.
func (m *Manager) SetGrantPolicy(c GrantPolicyChange) (Request, Request, error) {
	if c.MediumReview != nil && c.IdleSeconds != 0 {
		return Request{}, Request{}, ErrDenied
	}
	if c.IdleSeconds != 0 && (c.IdleSeconds < 60 || c.IdleSeconds > 1800) {
		return Request{}, Request{}, errors.New("闲置期限需为1至30分钟，或关闭")
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	m.prune()
	e := m.pending[c.ID]
	if e == nil || e.request.Nonce != c.Nonce || e.request.Status != "approved" || e.request.Operation.Kind != "grant" {
		return Request{}, Request{}, ErrDenied
	}
	before := e.snapshot()
	if c.MediumReview == nil {
		e.idleOverride = &c.IdleSeconds
	} else {
		if e.request.Operation.Grant.Permission != "guarded" {
			return Request{}, Request{}, ErrDenied
		}
		review := *c.MediumReview
		e.mediumReviewOverride = &review
		if review && !before.ConfirmsMediumReview() {
			for _, child := range m.pending {
				r := &child.request
				op := r.Operation
				if r.ParentID == c.ID && r.Status == "approved" && r.ParentGeneration != "" && !r.MediumReview &&
					operationrisk.ReviewOptionalMutation(op.Kind, op.Path, op.Destination) {
					r.Status = "revoked"
				}
			}
		}
	}
	after := e.snapshot()
	m.prune()
	if m.pending[c.ID] == nil {
		after.Status = "expired"
	}
	return before, after, nil
}
