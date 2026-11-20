package broker

import (
	"sort"

	"sshclient/internal/approval"
	"sshclient/internal/protocol"
	"sshclient/internal/vault"
)

// Owner queries are bound to a one-use challenge and the complete query,
// including pagination. A caller's label never selects this authority.
func OwnerQueryAction(action string) bool { return protocol.OwnerQueryAction(action) }

func OwnerQueryProof(challenge string, q Message, prove func([]byte) ([]byte, error)) ([]byte, error) {
	return protocol.OwnerQueryProof(challenge, q, prove)
}

func (s *Server) ownerQuery(q Message, channel *callerChannel) bool {
	if q.Challenge == "" || !OwnerQueryAction(q.Action) {
		return false
	}
	_, ok := s.verifyOwnerChallenge(channel, q.Challenge, func(prove func([]byte) ([]byte, error)) ([]byte, error) {
		return OwnerQueryProof(q.Challenge, q, prove)
	}, q.Proof)
	return ok
}

func (s *Server) queryRequest(q Message, channel *callerChannel) (approval.Request, error) {
	if s.ownerQuery(q, channel) {
		if q.Action == "sessionStatus" {
			return s.Approvals.SessionStatus(q.ID)
		}
		r, err := s.Approvals.Get(q.ID)
		if err != nil {
			return s.Approvals.SessionStatus(q.ID)
		}
		return r, nil
	}
	s.ownerMu.Lock()
	defer s.ownerMu.Unlock()
	if !s.ownsRequest(channel, q.ID) {
		return approval.Request{}, approval.ErrDenied
	}
	r, err := s.Approvals.GetForTicket(q.ID, q.Ticket)
	if err != nil || q.Action == "sessionStatus" && r.Operation.Kind != "grant" {
		return approval.Request{}, approval.ErrDenied
	}
	return r, nil
}

func (s *Server) pendingPage(q Message, channel *callerChannel) (Message, error) {
	if !s.ownerQuery(q, channel) {
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

// redactedSummaries keeps only what a caller needs to ask for access: the ID it
// must name, and the name and group a human uses to recognize it. Addresses,
// accounts, credentials and automation policy stay with the owner.
func redactedSummaries(list []vault.Summary) []vault.Summary {
	out := make([]vault.Summary, len(list))
	for i, v := range list {
		out[i] = vault.Summary{ID: v.ID, Name: v.Name, Group: v.Group}
	}
	return out
}
