package broker

import (
	"fmt"
	"net"

	"sshclient/internal/approval"
)

func (s *Server) checkRenewalTarget(op approval.Operation) error {
	p, err := s.Vault.Profile(op.Host)
	if err != nil {
		return s.recordAuthorityFailure(op, "", "invalid_authority", err)
	}
	endpoint := fmt.Sprintf("%s@%s", p.User, net.JoinHostPort(p.Host, fmt.Sprint(p.Port)))
	if endpoint != op.Endpoint || p.Fingerprint != op.HostFingerprint {
		return s.recordAuthorityFailure(op, "", "invalid_authority", approval.ErrDenied)
	}
	return s.checkAutomation(p, op, "")
}

// The caller can prove possession of the expired client key; it cannot supply
// a different endpoint, permission, key or duration for a renewal.
func (s *Server) renewSession(q Message) (Message, error) {
	if q.Action == "renewalChallenge" {
		if _, err := s.Approvals.GetForTicket(q.ID, q.Ticket); err != nil {
			return Message{}, s.recordAuthorityFailure(approval.Operation{Kind: q.Action}, "", "invalid_authority", err)
		}
		attempt := approval.Operation{Kind: "renewalChallenge", Client: q.Operation.Client}
		if previous, err := s.Approvals.SessionStatus(q.ID); err == nil {
			attempt.Host = previous.Operation.Host
		}
		r, c, err := s.Approvals.RenewalChallenge(q.ID)
		if err != nil {
			return Message{}, s.recordAuthorityFailure(attempt, q.ID, "invalid_authority", err)
		}
		op := r.Operation
		if c != nil {
			op = c.Operation
		}
		if err = s.checkRenewalTarget(op); err != nil {
			if policyDenial(err) {
				s.Approvals.InvalidateAutomationHost(op.Host)
			} else {
				s.Approvals.InvalidateHost(op.Host)
			}
			return Message{}, err
		}
		if c != nil {
			return Message{Renewal: c}, nil
		}
		return Message{Request: &r}, nil
	}
	op, parent, known := s.Approvals.RenewalOperation(q.ID)
	if !known {
		op = approval.Operation{Kind: "renewSession", Client: q.Operation.Client}
	}
	r, created, err := s.Approvals.Renew(q.ID, q.Proof)
	if err != nil {
		return Message{}, s.recordAuthorityFailure(op, parent, "invalid_authority", err)
	}
	if err = s.checkRenewalTarget(r.Operation); err != nil {
		if policyDenial(err) {
			s.Approvals.InvalidateAutomationHost(r.Operation.Host)
		} else {
			s.Approvals.InvalidateHost(r.Operation.Host)
		}
		return Message{}, err
	}
	if created {
		if err = s.auditRequest("renewalRequested", &r); err != nil {
			s.Approvals.Lock()
			return Message{}, err
		}
	}
	return Message{Request: &r}, nil
}
