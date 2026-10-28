package broker

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"path/filepath"
	"sshclient/internal/approval"
	"sshclient/internal/auditlog"
	"time"
	"unicode/utf8"
)

// Audit intentionally omits commands, paths, credentials, output and tickets.
// It is a local operational log, not a tamper-proof forensic ledger.
type audit struct {
	store *auditlog.Store
}

type auditEvent = auditlog.Event

func openAudit(dir string) (*audit, error) {
	s, err := auditlog.Open(filepath.Join(dir, "audit.jsonl"))
	if err != nil {
		return nil, err
	}
	return &audit{store: s}, nil
}

func (a *audit) close() error { return a.store.Close() }

func (a *audit) write(event string, r *approval.Request) error {
	return a.writeHost(event, r, "")
}

func (a *audit) writeHost(event string, r *approval.Request, hostName string) error {
	return a.writeOutcome(event, r, hostName, "", nil)
}

func (a *audit) writeOutcome(event string, r *approval.Request, hostName, outcome string, code *int) error {
	v := auditEvent{Time: time.Now().UTC(), Event: event}
	v.Outcome, v.ExitCode = outcome, code
	if r != nil {
		v.ParentID = auditText(r.ParentID, 128)
		v.RequestID = auditText(r.ID, 128)
		if r.Nonce != "" {
			generation := sha256.Sum256([]byte(r.Nonce))
			v.Generation = hex.EncodeToString(generation[:])
		}
		v.OwnerIssued = r.OwnerIssued
		v.OwnerSession = r.OwnerSession
		v.Host = auditText(r.Operation.Host, 128)
		v.Kind = auditText(r.Operation.Kind, 64)
		v.Client = auditText(r.Operation.Client, 128)
		v.HostName = auditText(hostName, 256)
		b, _ := json.Marshal(r.Operation)
		h := sha256.Sum256(b)
		v.OperationHash = hex.EncodeToString(h[:])
	}
	return a.store.Append(v)
}

// reject records a policy rejection without creating runnable authority or
// persisting the operation body. Reasons are codes, never raw error messages.
func (a *audit) reject(op approval.Operation, parentID, reason, hostName string) error {
	switch reason {
	case "automation_disabled", "server_disabled", "protected_path", "uncontrolled_operation", "invalid_policy", "scope_denied", "invalid_authority", "invalid_operation", "owner_required", "path_resolution_failed":
	default:
		return errors.New("无效历史拒绝原因")
	}
	var id [16]byte
	if _, err := rand.Read(id[:]); err != nil {
		return err
	}
	b, _ := json.Marshal(op)
	h := sha256.Sum256(b)
	return a.store.Append(auditEvent{
		Time: time.Now().UTC(), Event: "denied", RequestID: hex.EncodeToString(id[:]),
		ParentID: auditText(parentID, 128), Host: auditText(op.Host, 128), Kind: auditText(op.Kind, 64),
		Client: auditText(op.Client, 128), HostName: auditText(hostName, 256), Reason: reason,
		OperationHash: hex.EncodeToString(h[:]),
	})
}

func (a *audit) history(q auditlog.Query) (auditlog.Page, error) { return a.store.History(q) }

func auditText(s string, limit int) string {
	if len(s) > limit {
		s = s[:limit]
	}
	for !utf8.ValidString(s) {
		s = s[:len(s)-1]
	}
	return s
}
