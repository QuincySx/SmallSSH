package broker

import (
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"time"

	"sshclient/internal/approval"
	"sshclient/internal/sshconn"
	"sshclient/internal/vault"
)

// controlMessage changes authorization state and returns a response value.
// It has no transport: every deferred unlock completes before the caller writes.
func (s *Server) controlMessage(q Message, channel *callerChannel) (Message, error) {
	bucket, peer := channel.bucket, channel.peer
	recordAuthorityFailure := func(op approval.Operation, parent, reason string, cause error) error {
		return s.recordAuthorityFailure(op, parent, reason, cause, peer)
	}
	if s.auditFault.Load() {
		switch q.Action {
		case "request", "ownerRequest", "decide", "delegate", "renewSession", "profileChange", "profilePut", "profileDetails", "profileDelete", "workspaceWrite":
			return Message{}, errAuditUnavailable
		}
	}
	switch q.Action {
	case "openOperation":
		s.ownerMu.Lock()
		defer s.ownerMu.Unlock()
		if len(channel.requests) != 0 || channel.operation || channel.identity == nil || channel.identity.Verify() != nil {
			return Message{}, approval.ErrDenied
		}
		waiting := 0
		for _, other := range s.channels {
			if other.operation && len(other.requests) == 0 && other.peer.Instance == channel.peer.Instance {
				waiting++
			}
		}
		if waiting >= 8 {
			return Message{}, approval.ErrDenied
		}
		channel.operation = true
		return Message{Action: "channel", Channel: channel.id}, nil
	case "renewalChallenge", "renewSession":
		s.ownerMu.Lock()
		defer s.ownerMu.Unlock()
		parent := q.ID
		if q.Action == "renewSession" {
			_, parent, _ = s.Approvals.RenewalOperation(q.ID)
		}
		if !s.ownsSession(channel, parent) {
			return Message{}, recordAuthorityFailure(managementAttempt(q), parent, "invalid_authority", approval.ErrDenied)
		}
		result, e := s.renewSession(q, peer)
		if e != nil {
			return Message{}, e
		}
		return result, nil
	case "sessionStatus":
		r, e := s.queryRequest(q, channel)
		if e != nil {
			return Message{}, e
		}
		return Message{Request: &r}, nil
	case "delegationChallenge":
		s.ownerMu.Lock()
		defer s.ownerMu.Unlock()
		destination := s.channels[q.Channel]
		if !s.ownsSession(channel, q.ID) || destination == nil || !destination.operation || len(destination.requests) != 0 || destination.ctx.Err() != nil || !channel.identity.Same(destination.identity) {
			return Message{}, recordAuthorityFailure(q.Operation, q.ID, "invalid_authority", approval.ErrDenied)
		}
		p, e := s.Vault.Profile(q.Operation.Host)
		if e != nil {
			return Message{}, recordAuthorityFailure(q.Operation, q.ID, "invalid_authority", e)
		}
		q.Operation.Endpoint = fmt.Sprintf("%s@%s", p.User, net.JoinHostPort(p.Host, fmt.Sprint(p.Port)))
		q.Operation.HostFingerprint = p.Fingerprint
		if e = s.checkAutomation(p, q.Operation, q.ID, peer); e != nil {
			return Message{}, e
		}
		if _, e := s.Approvals.GetForTicket(q.ID, q.Ticket); e != nil {
			return Message{}, recordAuthorityFailure(q.Operation, q.ID, "invalid_authority", e)
		}
		challenge, e := s.Approvals.DelegationChallengeForChannel(q.ID, q.Operation, time.Duration(q.TTL)*time.Second, destination.id)
		if e != nil {
			if !errors.Is(e, approval.ErrSessionExpired) && !errors.Is(e, approval.ErrSessionRenewing) {
				e = s.recordRefusal(q.Operation, q.ID, "scope_denied", p.Name, e)
			}
			return Message{}, e
		}
		return Message{Delegation: &challenge}, nil
	case "delegate":
		s.ownerMu.Lock()
		defer s.ownerMu.Unlock()
		op, parent, known := s.Approvals.DelegationOperation(q.ID)
		destination := s.channels[s.Approvals.DelegationChannel(q.ID)]
		if !known || !s.ownsSession(channel, parent) || destination == nil || !destination.operation || len(destination.requests) != 0 || destination.ctx.Err() != nil || !channel.identity.Same(destination.identity) {
			return Message{}, recordAuthorityFailure(op, parent, "invalid_authority", approval.ErrDenied)
		}
		r, t, e := s.Approvals.MintDelegated(q.ID, q.Proof)
		if e != nil {
			if errors.Is(e, approval.ErrReviewPending) {
				return Message{}, e
			}
			if !known {
				op = approval.Operation{Kind: "delegate"}
			}
			return Message{}, recordAuthorityFailure(op, parent, "invalid_authority", e)
		}
		s.bindRequest(destination, r.ID)
		destination.parent = channel
		p, e := s.Vault.Profile(r.Operation.Host)
		if e == nil {
			e = s.checkAutomation(p, r.Operation, r.ParentID, peer)
		}
		if e != nil {
			s.Approvals.InvalidateAutomationHost(r.Operation.Host)
			return Message{}, e
		}
		event := "delegated"
		if r.Status == "pending" {
			event = "requested"
		}
		if e = s.auditRequest(event, &r, peer); e != nil {
			s.Approvals.Lock()
			return Message{}, e
		}
		return Message{Request: &r, Ticket: t, Channel: destination.id}, nil
	case "agentIdentities":
		if !s.ownerQuery(q, channel) {
			return Message{}, recordAuthorityFailure(managementAttempt(q), "", "owner_required", approval.ErrDenied)
		}
		keys, e := sshconn.AgentIdentities(channel.ctx)
		if e != nil {
			return Message{}, e
		}
		return Message{Identities: keys}, nil
	case "profileChange":
		_, ok := s.verifyOwnerChallenge(channel, q.ID, func(prove func([]byte) ([]byte, error)) ([]byte, error) {
			if q.ProfileChange == nil {
				return nil, errors.New("配置变更载荷缺失")
			}
			return ProfileChangeProof(q.ID, *q.ProfileChange, prove)
		}, q.Proof)
		if !ok {
			return Message{}, recordAuthorityFailure(managementAttempt(q), "", "owner_required", approval.ErrDenied)
		}
		s.ownerMu.Lock()
		defer s.ownerMu.Unlock()
		e := error(nil)
		profile, e := s.Vault.ResolveChange(*q.ProfileChange)
		if e == nil && profile.Auth == "key" {
			_, e = sshconn.InspectPrivateKey(profile.PrivateKey, profile.KeyPassphrase)
		}
		if e != nil {
			return Message{}, e
		}
		previous, previousErr := s.Vault.Profile(profile.ID)
		if e = s.Vault.SaveProfile(VaultPath(s.dir), profile); e != nil {
			return Message{}, e
		}
		s.invalidateProfile(previous, previousErr == nil, profile)
		if e = s.auditPolicyChange(previous.Policy, previousErr == nil, profile); e != nil {
			return Message{}, e
		}
		return Message{Action: "ok"}, nil
	case "ownerRequest":
		s.ownerMu.Lock()
		if len(channel.requests) != 0 || channel.operation {
			s.ownerMu.Unlock()
			return Message{}, approval.ErrDenied
		}
		s.ownerMu.Unlock()
		sessionID, ok := s.verifyOwnerChallenge(channel, q.ID, func(prove func([]byte) ([]byte, error)) ([]byte, error) {
			return OwnerRequestProof(q.ID, q.Operation, prove)
		}, q.Proof)
		if !ok {
			return Message{}, recordAuthorityFailure(managementAttempt(q), "", "owner_required", approval.ErrDenied)
		}
		if sessionID != "" && q.Operation.Kind == "grant" {
			return Message{}, recordAuthorityFailure(q.Operation, "", "invalid_operation", approval.ErrDenied)
		}
		s.ownerMu.Lock()
		defer s.ownerMu.Unlock()
		if len(channel.requests) != 0 || channel.operation || channel.ctx.Err() != nil || s.channels[channel.id] != channel {
			return Message{}, approval.ErrDenied
		}
		if sessionID != "" && s.ownerLeases[sessionID] == nil {
			return Message{}, approval.ErrDenied
		}
		profile, e := s.Vault.Profile(q.Operation.Host)
		if e != nil {
			return Message{}, e
		}
		endpoint := fmt.Sprintf("%s@%s", profile.User, net.JoinHostPort(profile.Host, fmt.Sprint(profile.Port)))
		if (q.Operation.Endpoint != "" && q.Operation.Endpoint != endpoint) || (q.Operation.HostFingerprint != "" && q.Operation.HostFingerprint != profile.Fingerprint) {
			return Message{}, errors.New("连接目标已变化，请重新连接后操作")
		}
		q.Operation.Endpoint = endpoint
		q.Operation.HostFingerprint = profile.Fingerprint
		r, t, e := s.Approvals.CreateOwnerFromPeer(q.Operation, peer)
		if e != nil {
			return Message{}, e
		}
		s.bindRequest(channel, r.ID)
		if sessionID != "" {
			s.requestOwners[r.ID] = sessionID
		}
		return Message{Request: &r, Ticket: t, Channel: channel.id}, nil

	case "withdraw":
		s.ownerMu.Lock()
		defer s.ownerMu.Unlock()
		if !s.ownsRequest(channel, q.ID) {
			return Message{}, recordAuthorityFailure(managementAttempt(q), q.ID, "invalid_authority", approval.ErrDenied)
		}
		if e := s.Approvals.Withdraw(q.ID, q.Ticket); e != nil {
			attempt := managementAttempt(q)
			if r, err := s.Approvals.Get(q.ID); err == nil {
				attempt.Host = r.Operation.Host
			}
			return Message{}, recordAuthorityFailure(attempt, q.ID, "invalid_authority", e)
		}
		if r, e := s.Approvals.Get(q.ID); e == nil {
			if e = s.auditRequest("withdrawn", &r, peer); e != nil {
				return Message{}, e
			}
		}
		return Message{Action: "ok"}, nil
	case "profileChallenge":
		s.ownerMu.Lock()
		defer s.ownerMu.Unlock()
		for k, v := range s.challenges {
			if time.Now().After(v) {
				delete(s.challenges, k)
			}
		}
		if len(s.challenges) >= 128 {
			return Message{}, errors.New("管理请求过多")
		}
		id := vault.ShortID()
		s.challenges[id] = time.Now().Add(time.Minute)
		return Message{ID: id}, nil
	case "auditHistory":
		denied := approval.Operation{Kind: "audit-history", Client: q.Operation.Client}
		_, ok := s.verifyOwnerChallenge(channel, q.ID, func(prove func([]byte) ([]byte, error)) ([]byte, error) {
			if q.AuditQuery == nil {
				return nil, errors.New("审计查询缺失")
			}
			return AuditHistoryProof(q.ID, *q.AuditQuery, prove)
		}, q.Proof)
		if !ok {
			return Message{}, recordAuthorityFailure(denied, "", "owner_required", approval.ErrDenied)
		}
		page, e := s.audit.history(*q.AuditQuery)
		if e != nil {
			return Message{}, e
		}
		return Message{AuditPage: &page}, nil
	case "profilePut":
		_, ok := s.verifyOwnerChallenge(channel, q.ID, func(prove func([]byte) ([]byte, error)) ([]byte, error) {
			if q.Profile == nil {
				return nil, errors.New("连接配置载荷缺失")
			}
			return ProfileProof(q.ID, *q.Profile, prove)
		}, q.Proof)
		if !ok {
			return Message{}, recordAuthorityFailure(managementAttempt(q), "", "owner_required", approval.ErrDenied)
		}
		s.ownerMu.Lock()
		defer s.ownerMu.Unlock()
		previous, previousErr := s.Vault.Profile(q.Profile.ID)
		e := error(nil)
		if e = s.Vault.SaveProfile(VaultPath(s.dir), *q.Profile); e != nil {
			return Message{}, e
		}
		s.invalidateProfile(previous, previousErr == nil, *q.Profile)
		if e = s.auditPolicyChange(previous.Policy, previousErr == nil, *q.Profile); e != nil {
			return Message{}, e
		}
		return Message{Action: "ok"}, nil
	case "profileDetails":
		_, ok := s.verifyOwnerChallenge(channel, q.ID, func(prove func([]byte) ([]byte, error)) ([]byte, error) {
			if q.Details == nil {
				return nil, errors.New("连接详情载荷缺失")
			}
			b, err := json.Marshal(q.Details)
			if err != nil {
				return nil, err
			}
			return prove(append([]byte("ssh-client/profile-details/v1\x00"+q.ID+"\x00"), b...))
		}, q.Proof)
		if !ok {
			return Message{}, recordAuthorityFailure(managementAttempt(q), "", "owner_required", approval.ErrDenied)
		}
		s.ownerMu.Lock()
		defer s.ownerMu.Unlock()
		previous, previousErr := s.Vault.Profile(q.Details.ID)
		e := error(nil)
		if e = s.Vault.SaveDetails(VaultPath(s.dir), *q.Details); e != nil {
			return Message{}, e
		}
		updated, e := s.Vault.Profile(q.Details.ID)
		if e != nil {
			return Message{}, e
		}
		s.invalidateProfile(previous, previousErr == nil, updated)
		if e = s.auditPolicyChange(previous.Policy, previousErr == nil, updated); e != nil {
			return Message{}, e
		}
		return Message{Action: "ok"}, nil
	case "profileDelete":
		_, ok := s.verifyOwnerChallenge(channel, q.ID, func(prove func([]byte) ([]byte, error)) ([]byte, error) {
			if q.Target == "" || len(q.Target) > 128 {
				return nil, errors.New("连接目标无效")
			}
			return prove([]byte("ssh-client/profile-delete/v1\x00" + q.ID + "\x00" + q.Target))
		}, q.Proof)
		if !ok {
			return Message{}, recordAuthorityFailure(managementAttempt(q), "", "owner_required", approval.ErrDenied)
		}
		s.ownerMu.Lock()
		defer s.ownerMu.Unlock()
		e := error(nil)
		if e = s.Vault.DeleteProfile(VaultPath(s.dir), q.Target); e != nil {
			return Message{}, e
		}
		s.roots.clear()
		s.conns.clear()
		s.Approvals.InvalidateHost(q.Target)
		s.invalidateRelayDependents(q.Target)
		return Message{Action: "ok"}, nil
	case "hosts":
		// Without owner proof a caller gets what it needs to ask for access and
		// nothing about where the machine is or how to log in to it.
		if q.Challenge == "" && len(q.Proof) == 0 {
			list := redactedSummaries(s.Vault.Summaries())
			return Message{Connections: list, Redacted: true}, nil
		}
		owner := s.ownerQuery(q, channel)
		list := s.Vault.Summaries()
		if !owner {
			return Message{}, recordAuthorityFailure(managementAttempt(q), "", "owner_required", approval.ErrDenied)
		}
		return Message{Hosts: s.Vault.Names(), Connections: list, Routes: s.Vault.Routes()}, nil
	case "pending":
		page, e := s.pendingPage(q, channel)
		if e != nil {
			return Message{}, recordAuthorityFailure(managementAttempt(q), "", "owner_required", e)
		}
		return page, nil
	case "workspaceRead":
		return s.workspaceRead(q, channel)
	case "workspaceWrite":
		return s.workspaceWrite(q, channel)
	case "request":
		s.ownerMu.Lock()
		defer s.ownerMu.Unlock()
		if len(channel.requests) != 0 || channel.operation || channel.identity == nil || channel.identity.Verify() != nil {
			return Message{}, recordAuthorityFailure(q.Operation, "", "invalid_authority", approval.ErrDenied)
		}
		p, e := s.Vault.Profile(q.Operation.Host)
		if e != nil {
			return Message{}, recordAuthorityFailure(q.Operation, "", "invalid_authority", e)
		}
		q.Operation.Endpoint = fmt.Sprintf("%s@%s", p.User, net.JoinHostPort(p.Host, fmt.Sprint(p.Port)))
		q.Operation.HostFingerprint = p.Fingerprint
		q.Operation.PathProtection = q.Operation.Kind == "grant" && !p.Policy.Unrestricted
		if e = s.checkAutomation(p, q.Operation, "", peer); e != nil {
			return Message{}, e
		}
		r, t, e := s.Approvals.CreateFromPeer(q.Operation, time.Duration(q.TTL)*time.Second, bucket, peer)
		if e != nil {
			return Message{}, s.recordRefusal(q.Operation, "", "invalid_operation", p.Name, e)
		}
		s.bindRequest(channel, r.ID)
		if r.Operation.Kind == "grant" {
			channel.session = r.ID
		}
		if e = s.auditRequest("requested", &r, peer); e != nil {
			s.Approvals.Lock()
			return Message{}, e
		}
		return Message{Request: &r, Ticket: t, Channel: channel.id}, nil
	case "status":
		r, e := s.queryRequest(q, channel)
		if e != nil {
			return Message{}, e
		}
		return Message{Request: &r}, nil
	case "decide":
		reviewed, getErr := s.Approvals.Get(q.ID)
		proof := q.Proof
		managementSession := ""
		if getErr == nil && reviewed.Operation.Kind != "grant" {
			sessionID, payload, ok := s.verifyDecisionProof(channel, reviewed, q.Allow, q.Proof)
			if !ok {
				attempt := managementAttempt(q)
				attempt.Host = reviewed.Operation.Host
				return Message{}, recordAuthorityFailure(attempt, q.ID, "invalid_authority", approval.ErrDenied)
			}
			if sessionID != "" {
				managementSession = sessionID
				if !s.ownerSessionMayDecide(reviewed, sessionID) {
					attempt := managementAttempt(q)
					attempt.Host = reviewed.Operation.Host
					return Message{}, recordAuthorityFailure(attempt, q.ID, "invalid_authority", approval.ErrDenied)
				}
				var e error
				proof, e = s.Vault.Proof(payload)
				if e != nil {
					return Message{}, recordAuthorityFailure(reviewed.Operation, q.ID, "invalid_authority", approval.ErrDenied)
				}
			}
		}
		s.ownerMu.Lock()
		defer s.ownerMu.Unlock()
		if managementSession != "" && (s.ownerLeases[managementSession] == nil || channel.ctx.Err() != nil || s.channels[channel.id] != channel) {
			return Message{}, recordAuthorityFailure(reviewed.Operation, q.ID, "invalid_authority", approval.ErrDenied)
		}
		if e := s.Approvals.DecideWithTerms(q.ID, q.Allow, q.TTL, q.Idle, q.SkipMediumReview, proof); e != nil {
			attempt := managementAttempt(q)
			if r, err := s.Approvals.Get(q.ID); err == nil {
				attempt.Host = r.Operation.Host
			}
			return Message{}, recordAuthorityFailure(attempt, q.ID, "invalid_authority", e)
		}
		r, e := s.Approvals.Get(q.ID)
		if e != nil {
			return Message{}, e
		}
		if e = s.auditRequest(r.Status, &r, peer); e != nil {
			s.Approvals.Lock()
			return Message{}, e
		}
		return Message{Action: "ok"}, nil
	case "revoke":
		proof := q.Proof
		reviewed, getErr := s.Approvals.Get(q.ID)
		managementSession := ""
		if getErr == nil && reviewed.Operation.Kind != "grant" {
			sessionID, payload, ok := s.verifyRevokeProof(channel, reviewed, q.Proof)
			if !ok {
				attempt := managementAttempt(q)
				attempt.Host = reviewed.Operation.Host
				return Message{}, recordAuthorityFailure(attempt, q.ID, "invalid_authority", approval.ErrDenied)
			}
			if sessionID != "" {
				managementSession = sessionID
				if !s.ownerSessionMayDecide(reviewed, sessionID) {
					attempt := managementAttempt(q)
					attempt.Host = reviewed.Operation.Host
					return Message{}, recordAuthorityFailure(attempt, q.ID, "invalid_authority", approval.ErrDenied)
				}
				var e error
				proof, e = s.Vault.Proof(payload)
				if e != nil {
					return Message{}, recordAuthorityFailure(reviewed.Operation, q.ID, "invalid_authority", approval.ErrDenied)
				}
			}
		}
		s.ownerMu.Lock()
		if managementSession != "" && (s.ownerLeases[managementSession] == nil || channel.ctx.Err() != nil || s.channels[channel.id] != channel) {
			s.ownerMu.Unlock()
			return Message{}, recordAuthorityFailure(reviewed.Operation, q.ID, "invalid_authority", approval.ErrDenied)
		}
		revokeErr := s.Approvals.Revoke(q.ID, proof)
		s.ownerMu.Unlock()
		if revokeErr != nil {
			attempt := managementAttempt(q)
			if r, err := s.Approvals.Get(q.ID); err == nil {
				attempt.Host = r.Operation.Host
			}
			return Message{}, recordAuthorityFailure(attempt, q.ID, "invalid_authority", revokeErr)
		}
		r, e := s.Approvals.Get(q.ID)
		if e != nil {
			r, e = s.Approvals.SessionStatus(q.ID)
		}
		if e == nil {
			if e = s.auditRequest("revoked", &r, peer); e != nil {
				return Message{}, e
			}
		}
		return Message{Action: "ok"}, nil
	default:
		return Message{}, errors.New("不支持的本地操作")
	}
}

func errorResponse(err error) Message {
	code := ""
	if errors.Is(err, approval.ErrSessionExpired) {
		code = "session_expired"
	}
	if errors.Is(err, approval.ErrSessionRenewing) {
		code = "session_renewing"
	}
	if errors.Is(err, approval.ErrReviewPending) {
		code = "operation_review_pending"
	}
	if policyDenial(err) {
		code = "automation_policy"
	}
	return Message{Action: "error", Error: err.Error(), ErrorCode: code}
}
