package broker

import (
	"errors"
	"fmt"
	"slices"

	"sshclient/internal/accesspolicy"
	"sshclient/internal/approval"
	"sshclient/internal/vault"
)

// Policy refusals never create a pending request. A caller label, a parent
// grant, or the public ServerStatus flag cannot acquire owner authority.
func (s *Server) checkAutomation(p vault.Profile, op approval.Operation, parent string) error {
	if err := p.Policy.Check(op); err != nil {
		return s.recordRefusal(op, parent, accesspolicy.Reason(err), p.Name, err)
	}
	return nil
}

func (s *Server) recordRefusal(op approval.Operation, parent, reason, hostName string, cause error) error {
	if err := s.audit.reject(op, parent, reason, hostName); err != nil {
		// No authority or remote effect is allowed when its refusal cannot be
		// recorded. Existing grants are also invalidated on audit failure.
		s.Approvals.Lock()
		return fmt.Errorf("审计记录失败，操作已阻止: %w", err)
	}
	return cause
}

func (s *Server) recordAuthorityFailure(op approval.Operation, parent, reason string, cause error) error {
	name := ""
	if p, err := s.Vault.Profile(op.Host); err == nil {
		name = p.Name
	}
	return s.recordRefusal(op, parent, reason, name, cause)
}

func (s *Server) auditRequest(event string, r *approval.Request) error {
	name := ""
	if r != nil {
		if p, err := s.Vault.Profile(r.Operation.Host); err == nil {
			name = p.Name
		}
	}
	return s.audit.writeHost(event, r, name)
}

func (s *Server) auditExpiries() {
	for _, r := range s.Approvals.DrainExpired() {
		if err := s.auditRequest("expired", &r); err != nil {
			s.Approvals.Lock()
			return
		}
	}
}

func (s *Server) auditPolicyChange(old accesspolicy.Policy, existed bool, p vault.Profile) error {
	if existed && samePolicy(old, p.Policy) {
		return nil
	}
	r := approval.Request{OwnerIssued: true, Operation: approval.Operation{Host: p.ID, Kind: "policy", Client: "所有者", PathProtection: !p.Policy.Unrestricted}}
	if err := s.auditRequest("policy_changed", &r); err != nil {
		s.Approvals.Lock()
		return err
	}
	return nil
}

func samePolicy(a, b accesspolicy.Policy) bool {
	return a.Disabled == b.Disabled && a.Unrestricted == b.Unrestricted && slices.Equal(a.Paths(), b.Paths())
}

func (s *Server) invalidateProfile(old vault.Profile, existed bool, p vault.Profile) {
	if existed && !samePolicy(old.Policy, p.Policy) && old.Host == p.Host && old.Port == p.Port && old.User == p.User &&
		old.Fingerprint == p.Fingerprint && old.Auth == p.Auth && old.AgentFingerprint == p.AgentFingerprint &&
		old.Password == p.Password && old.PrivateKey == p.PrivateKey && old.KeyPassphrase == p.KeyPassphrase {
		s.Approvals.InvalidateAutomationHost(p.ID)
		return
	}
	s.Approvals.InvalidateHost(p.ID)
}

func policyDenial(err error) bool {
	return errors.Is(err, accesspolicy.ErrDisabled) || errors.Is(err, accesspolicy.ErrProtected) ||
		errors.Is(err, accesspolicy.ErrUncontrolled) || errors.Is(err, accesspolicy.ErrInvalidPolicy)
}

func managementAttempt(q Message) approval.Operation {
	op := approval.Operation{Kind: q.Action, Client: q.Operation.Client, Host: q.Target}
	if q.Profile != nil {
		op.Host = q.Profile.ID
	}
	if q.Details != nil {
		op.Host = q.Details.ID
	}
	if q.ProfileChange != nil {
		op.Host = q.ProfileChange.Profile.ID
	}
	if q.Action == "ownerRequest" {
		op.Host = q.Operation.Host
	}
	return op
}
