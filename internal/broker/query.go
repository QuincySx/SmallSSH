package broker

import (
	"crypto/hmac"
	"encoding/json"
	"sort"
	"time"

	"sshclient/internal/approval"
)

// Owner queries are bound to a one-use challenge and the complete query,
// including pagination. A caller's label never selects this authority.
func OwnerQueryAction(action string) bool {
	switch action {
	case "hosts", "pending", "status", "sessionStatus", "probeHost", "agentIdentities":
		return true
	}
	return false
}

func OwnerQueryProof(challenge string, q Message, prove func([]byte) ([]byte, error)) ([]byte, error) {
	b, err := json.Marshal(struct {
		Action, ID, Target, After string
		Port, Limit               int
	}{q.Action, q.ID, q.Target, q.After, q.Port, q.Limit})
	if err != nil {
		return nil, err
	}
	return prove(append([]byte("ssh-client/owner-query/v1\x00"+challenge+"\x00"), b...))
}

func (s *Server) ownerQuery(q Message) bool {
	if q.Challenge == "" || !OwnerQueryAction(q.Action) {
		return false
	}
	s.ownerMu.Lock()
	defer s.ownerMu.Unlock()
	expires, ok := s.challenges[q.Challenge]
	delete(s.challenges, q.Challenge)
	if !ok || !time.Now().Before(expires) {
		return false
	}
	expected, err := OwnerQueryProof(q.Challenge, q, s.Vault.Proof)
	return err == nil && hmac.Equal(expected, q.Proof)
}

func (s *Server) queryRequest(q Message) (approval.Request, error) {
	if s.ownerQuery(q) {
		if q.Action == "sessionStatus" {
			return s.Approvals.SessionStatus(q.ID)
		}
		r, err := s.Approvals.Get(q.ID)
		if err != nil {
			return s.Approvals.SessionStatus(q.ID)
		}
		return r, nil
	}
	r, err := s.Approvals.GetForTicket(q.ID, q.Ticket)
	if err != nil || q.Action == "sessionStatus" && r.Operation.Kind != "grant" {
		return approval.Request{}, approval.ErrDenied
	}
	return r, nil
}

func (s *Server) pendingPage(q Message) (Message, error) {
	if !s.ownerQuery(q) {
		return Message{}, approval.ErrDenied
	}
	limit := q.Limit
	if limit == 0 {
		limit = 8
	}
	if limit < 1 || limit > 8 || len(q.After) > 64 {
		return Message{}, approval.ErrDenied
	}
	rows := s.Approvals.List()
	sort.Slice(rows, func(i, j int) bool { return rows[i].ID < rows[j].ID })
	page := Message{Requests: []approval.Request{}}
	for _, r := range rows {
		if r.ID <= q.After {
			continue
		}
		if len(page.Requests) == limit {
			page.Next = page.Requests[len(page.Requests)-1].ID
			break
		}
		page.Requests = append(page.Requests, r)
	}
	return page, nil
}
