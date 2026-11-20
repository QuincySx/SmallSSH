package broker

import (
	"errors"
	"time"

	"sshclient/internal/auditlog"
	"sshclient/internal/identityagent"
	"sshclient/internal/identitymodel"
)

func (s *Server) auditSignature(e *identityagent.SignEvent) error {
	if s.auditFault.Load() {
		return errAuditUnavailable
	}
	if e.Event == "ssh_requested" {
		e.Executable = executablePath(e.PeerPID)
	}
	v := auditlog.Event{Time: time.Now().UTC(), Event: e.Event, Kind: "ssh-key-sign", RequestID: e.ID, PeerPID: e.PeerPID, ProcessInstance: e.ProcessInstance, ExecutableID: e.ExecutableID, Executable: auditText(e.Executable, 1024), Bundle: auditText(e.Bundle, 256), Object: auditText(e.Key, 128), KeyFingerprint: e.Fingerprint, OperationHash: e.DataHash, Destination: auditText(e.Target, 512), Authorization: e.Authorization, Outcome: e.Outcome, Reason: e.Reason}
	if err := s.audit.store.Append(v); err != nil {
		return s.failAudit(err)
	}
	return nil
}

func (s *Server) auditIdentityAuthorization(e identitymodel.AuditEvent) error {
	if s.auditFault.Load() {
		return errAuditUnavailable
	}
	switch e.Event {
	case "ssh_authorized", "ssh_denied", "ssh_revoked", "ssh_expired", "ssh_policy_changed", "ssh_key_created", "ssh_key_deleted":
	default:
		return errors.New("无效的密钥授权事件")
	}
	v := auditlog.Event{Time: time.UnixMilli(e.Time).UTC(), Event: e.Event, Kind: "ssh-key-authorization", RequestID: auditText(e.ID, 128), ParentID: auditText(e.LeaseID, 128), Object: auditText(e.Key, 128), KeyFingerprint: auditText(e.Fingerprint, 128), KeyName: auditText(e.Name, 128), Authorization: auditText(e.Authorization, 64), Reason: auditText(e.Reason, 64), IdleSeconds: e.IdleSeconds, PreviousIdle: e.PreviousIdle, OwnerIssued: e.PeerPID == 0, PeerPID: e.PeerPID, Executable: auditText(e.Executable, 1024), ProcessInstance: auditText(e.ProcessInstance, 128), ExecutableID: auditText(e.ExecutableID, 128)}
	if e.Expires > 0 {
		v.Expires = time.UnixMilli(e.Expires).UTC()
	}
	if err := s.audit.store.Append(v); err != nil {
		return s.failAudit(err)
	}
	return nil
}
