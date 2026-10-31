package broker

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"path/filepath"
	"strings"

	"sshclient/internal/approval"
	"sshclient/internal/auditlog"
	"time"
	"unicode/utf8"
)

// Audit intentionally omits credentials, output and tickets. It keeps the
// operation excerpt — the command that ran or the path it touched — because a
// history the owner cannot recognise answers no question worth asking. The file
// is a private 0600 record in the owner's own data directory, so a command that
// carries a secret is written there too: keep secrets out of commands.
type audit struct {
	store *auditlog.Store
}

// head is the digest of the last record, kept by the vault as an anchor against a
// log rewritten as a whole.
func (a *audit) head() string {
	if a == nil || a.store == nil {
		return ""
	}
	return a.store.Head()
}

// contains reports whether the retained history still holds the anchored record.
func (a *audit) contains(digest string) (bool, error) {
	if a == nil || a.store == nil {
		return false, errors.New("审计历史不可用")
	}
	return a.store.Contains(digest)
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
		v.Renewals = r.Renewals
		v.OwnerSession = r.OwnerSession
		v.Host = auditText(r.Operation.Host, 128)
		v.Kind = auditText(r.Operation.Kind, 64)
		v.Client = auditText(r.Operation.Client, 128)
		v.HostName = auditText(hostName, 256)
		addExcerpt(&v, r.Operation)
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
	rejected := auditEvent{
		Time: time.Now().UTC(), Event: "denied", RequestID: hex.EncodeToString(id[:]),
		ParentID: auditText(parentID, 128), Host: auditText(op.Host, 128), Kind: auditText(op.Kind, 64),
		Client: auditText(op.Client, 128), HostName: auditText(hostName, 256), Reason: reason,
		OperationHash: hex.EncodeToString(h[:]),
	}
	addExcerpt(&rejected, op)
	return a.store.Append(rejected)
}

// Operation excerpts are bounded on write as well, so a long command cannot fill
// the history it is meant to explain.
const (
	auditCommandLimit = 512
	auditPathLimit    = 512
)

// addExcerpt records what the owner needs to recognise an operation after the
// fact: the command that ran, the path it touched, and the container or Compose
// project it addressed. Credentials, output and tickets still stay out.
func addExcerpt(v *auditEvent, op approval.Operation) {
	v.Command = auditText(op.Command, auditCommandLimit)
	v.Path = auditText(op.Path, auditPathLimit)
	v.Destination = auditText(op.Destination, auditPathLimit)
	switch op.Kind {
	case "docker-logs", "docker-terminal":
		v.Object = auditText(op.Docker.Target.ID, 128)
	case "compose-plan", "compose-update":
		v.Object = auditText(strings.TrimSpace(op.Compose.Project+" "+op.Compose.Services), 128)
	}
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
