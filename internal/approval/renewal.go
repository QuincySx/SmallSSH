package approval

import (
	"crypto/ed25519"
	"encoding/base64"
	"encoding/json"
	"errors"
	"time"
)

var (
	ErrSessionExpired  = errors.New("客户端会话已自然过期，需要所有者批准续期")
	ErrSessionRenewing = errors.New("客户端会话正在等待所有者批准续期")
)

type RenewalChallenge struct {
	ID            string    `json:"id"`
	SessionID     string    `json:"sessionId"`
	PreviousNonce string    `json:"previousNonce"`
	Operation     Operation `json:"operation"`
	Expires       time.Time `json:"expires"`
}

func RenewalProofMessage(c RenewalChallenge) []byte {
	b, _ := json.Marshal(c)
	return append([]byte("ssh-client/session-renewal/v1\x00"), b...)
}

// RenewalOperation exposes no credential or proof; the broker uses the exact
// attempted operation when recording a rejected one-use renewal signature.
func (m *Manager) RenewalOperation(id string) (Operation, string, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	c, ok := m.renewals[id]
	return c.Operation, c.SessionID, ok
}

// Expired-session metadata is not authority: it can only start a fresh owner
// decision for the same client key, target and scope. It is bounded and cleared
// on lock, restart or connection changes. Unknown sessions never auto-renew.
func (m *Manager) pruneRenewals() {
	for len(m.dormant) > 256 {
		var oldest string
		var at time.Time
		for id, e := range m.dormant {
			if oldest == "" || e.dormantAt.Before(at) {
				oldest, at = id, e.dormantAt
			}
		}
		delete(m.dormant, oldest)
	}
	for id, c := range m.renewals {
		if !m.now().Before(c.Expires) {
			delete(m.renewals, id)
		}
	}
}

func (m *Manager) SessionStatus(id string) (Request, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.prune()
	if e := m.pending[id]; e != nil && e.request.Operation.Kind == "grant" {
		return e.request, nil
	}
	if e := m.dormant[id]; e != nil {
		return e.request, nil
	}
	return Request{}, ErrDenied
}

// Already queued/approved renewals are returned without creating another
// request. Merely calling this method can never grant or extend access.
func (m *Manager) RenewalChallenge(id string) (Request, *RenewalChallenge, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.prune()
	if e := m.pending[id]; e != nil {
		if e.request.Operation.Kind == "grant" && (e.request.Status == "approved" || e.request.Status == "pending" && e.request.Renewal) {
			return e.request, nil, nil
		}
		return Request{}, nil, ErrDenied
	}
	e := m.dormant[id]
	if e == nil || e.request.Status != "expired" {
		return Request{}, nil, ErrDenied
	}
	if m.renewals == nil {
		m.renewals = make(map[string]RenewalChallenge)
	}
	if len(m.renewals) >= 128 {
		return Request{}, nil, errors.New("待签名续期请求过多")
	}
	c := RenewalChallenge{ID: token(), SessionID: id, PreviousNonce: e.request.Nonce, Operation: e.request.Operation, Expires: m.now().Add(30 * time.Second).UTC()}
	m.renewals[c.ID] = c
	return Request{}, &c, nil
}

// Renew consumes a client-key proof and changes an expired grant into a new
// pending generation. The fresh nonce invalidates every previous owner proof.
func (m *Manager) Renew(id string, signature []byte) (Request, bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.prune()
	c, ok := m.renewals[id]
	delete(m.renewals, id)
	if !ok || !m.now().Before(c.Expires) {
		return Request{}, false, ErrDenied
	}
	key, err := base64.RawURLEncoding.DecodeString(c.Operation.Grant.PublicKey)
	if err != nil || len(key) != ed25519.PublicKeySize || !ed25519.Verify(key, RenewalProofMessage(c), signature) {
		return Request{}, false, ErrDenied
	}
	if e := m.pending[c.SessionID]; e != nil {
		if e.renewedFrom == c.PreviousNonce && e.request.Operation == c.Operation && (e.request.Status == "approved" || e.request.Status == "pending") {
			return e.request, false, nil
		}
		return Request{}, false, ErrDenied
	}
	e := m.dormant[c.SessionID]
	if e == nil || e.request.Status != "expired" || e.request.Nonce != c.PreviousNonce || e.request.Operation != c.Operation {
		return Request{}, false, ErrDenied
	}
	if len(m.pending) >= 128 {
		return Request{}, false, errors.New("待处理授权过多，请稍后重试")
	}
	delete(m.dormant, c.SessionID)
	e.renewedFrom = c.PreviousNonce
	e.request.Nonce = token()
	e.request.Renewal = true
	e.request.Renewals++
	e.request.Status = "pending"
	e.request.Expires = m.now().Add(5 * time.Minute).UTC()
	e.lastUsed = time.Time{}
	e.cancel = nil
	m.pending[c.SessionID] = e
	return e.request, true, nil
}
