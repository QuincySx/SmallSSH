// Package auditlog defines the metadata-only, local operational history.
// It is deliberately not a tamper-proof forensic ledger.
package auditlog

import (
	"encoding/json"
	"path/filepath"
	"strings"
	"time"
	"unicode/utf8"
)

type Event struct {
	ActorPID             int       `json:"actorPid,omitempty"`
	ActorExecutable      string    `json:"actorExecutable,omitempty"`
	ActorInstance        string    `json:"actorInstance,omitempty"`
	ActorCodeID          string    `json:"actorCodeId,omitempty"`
	KeyName              string    `json:"keyName,omitempty"`
	Executable           string    `json:"executable,omitempty"`
	Bundle               string    `json:"bundle,omitempty"`
	Endpoint             string    `json:"endpoint,omitempty"`
	HostFingerprint      string    `json:"hostFingerprint,omitempty"`
	KeyFingerprint       string    `json:"keyFingerprint,omitempty"`
	Authorization        string    `json:"authorization,omitempty"`
	Permission           string    `json:"permission,omitempty"`
	ReadRoot             string    `json:"readRoot,omitempty"`
	WriteRoot            string    `json:"writeRoot,omitempty"`
	Expires              time.Time `json:"expires,omitzero"`
	IdleSeconds          int       `json:"idleSeconds,omitempty"`
	PreviousIdle         *int      `json:"previousIdle,omitempty"`
	SkipMediumReview     bool      `json:"skipMediumReview,omitempty"`
	MediumReview         *bool     `json:"mediumReview,omitempty"`
	PreviousMediumReview *bool     `json:"previousMediumReview,omitempty"`
	ExcerptTruncated     bool      `json:"excerptTruncated,omitempty"`
	RecordHash           string    `json:"recordHash,omitempty"` // calculated on read, never stored
	PeerPID              int       `json:"peerPid,omitempty"`
	ProcessInstance      string    `json:"processInstance,omitempty"`
	ExecutableID         string    `json:"executableId,omitempty"`
	Outcome              string    `json:"outcome,omitempty"`
	ExitCode             *int      `json:"exitCode,omitempty"`
	Generation           string    `json:"generation,omitempty"`
	ParentID             string    `json:"parentId,omitempty"`
	ParentGeneration     string    `json:"parentGeneration,omitempty"`
	OwnerIssued          bool      `json:"ownerIssued,omitempty"`
	OwnerSession         bool      `json:"ownerSession,omitempty"`
	Renewals             int       `json:"renewals,omitempty"`
	Time                 time.Time `json:"time"`
	Event                string    `json:"event"`
	RequestID            string    `json:"requestId,omitempty"`
	Host                 string    `json:"hostId,omitempty"`
	Kind                 string    `json:"kind,omitempty"`
	OperationHash        string    `json:"operationHash,omitempty"`
	// Client is an unverified caller label, never an authenticated identity.
	Client   string `json:"client,omitempty"`
	HostName string `json:"hostName,omitempty"`
	Reason   string `json:"reason,omitempty"`
	// Command, Path, Destination and Object are the operation excerpt: what ran
	// and on what. They are what makes a record reviewable after the fact, and
	// they are bounded so a long command cannot fill the history or a page.
	Command     string `json:"command,omitempty"`
	Path        string `json:"path,omitempty"`
	Destination string `json:"destination,omitempty"`
	Object      string `json:"object,omitempty"`
	// Prev is the SHA256 of the record written before this one, so removing or
	// editing a record in the middle breaks the chain and Verify reports it.
	Prev string `json:"prev,omitempty"`
}

type Query struct {
	Before      string    `json:"before,omitempty"`
	Limit       int       `json:"limit,omitempty"`
	Category    string    `json:"category,omitempty"`
	Search      string    `json:"search,omitempty"`
	Application string    `json:"application,omitempty"`
	Related     string    `json:"related,omitempty"`
	Result      string    `json:"result,omitempty"`
	From        time.Time `json:"from,omitzero"`
	To          time.Time `json:"to,omitzero"`
	Summary     bool      `json:"summary,omitempty"`
	Raw         bool      `json:"raw,omitempty"`
}

type Page struct {
	Events  []Event           `json:"events"`
	Next    string            `json:"next,omitempty"`
	Summary *Summary          `json:"summary,omitempty"`
	Raw     []json.RawMessage `json:"raw,omitempty"`
}

type ApplicationCount struct {
	Application    string `json:"application"`
	Authorizations int    `json:"authorizations"`
	Operations     int    `json:"operations"`
	Signatures     int    `json:"signatures"`
}
type Summary struct {
	Records        int                `json:"records"`
	Authorizations int                `json:"authorizations"`
	Operations     int                `json:"operations"`
	Signatures     int                `json:"signatures"`
	Applications   []ApplicationCount `json:"applications"`
	First          time.Time          `json:"first"`
	Head           string             `json:"head"`
}

func Application(v Event) string {
	if v.Executable != "" {
		return v.Executable
	}
	if v.Bundle != "" {
		return v.Bundle
	}
	if v.ExecutableID != "" {
		return v.ExecutableID
	}
	if v.OwnerIssued {
		return "本机所有者"
	}
	return "未记录"
}

func ApplicationName(v Event) string { return filepath.Base(Application(v)) }

func Category(v Event) string {
	if strings.HasPrefix(v.Event, "system_") {
		return "system"
	}
	if strings.HasPrefix(v.Kind, "ssh-key") || strings.HasPrefix(v.Event, "ssh_") {
		return "ssh"
	}
	if v.Kind == "grant" || v.Event == "approved" || v.Event == "requested" || v.Event == "denied" || v.Event == "revoked" || v.Event == "expired" || v.Event == "grant_policy_changed" || v.Event == "renewalRequested" || v.Event == "withdrawn" {
		return "authorization"
	}
	if v.Event == "started" || v.Event == "ended" || v.Event == "delegated" || v.Event == "workspace_write" {
		return "operation"
	}
	return "system"
}

func (q Query) matches(v Event) bool {
	if q.Category != "" && q.Category != Category(v) || q.Application != "" && q.Application != Application(v) || q.Result != "" && q.Result != v.Event && q.Result != v.Outcome && "ssh_"+q.Result != v.Event && !(q.Result == "approved" && v.Event == "ssh_authorized") || q.Related != "" && q.Related != v.RequestID && q.Related != v.ParentID || !q.From.IsZero() && v.Time.Before(q.From) || !q.To.IsZero() && !v.Time.Before(q.To) {
		return false
	}
	return q.Search == "" || strings.Contains(strings.ToLower(strings.Join([]string{v.RequestID, v.ParentID, v.Executable, v.Bundle, v.Client, v.Host, v.HostName, v.Endpoint, v.Command, v.Path, v.Destination, v.Object, v.KeyFingerprint, v.KeyName}, "\n")), strings.ToLower(q.Search))
}

// Historical files can contain damaged or externally written metadata. Bound
// each displayed field so a valid 100-row page also fits the local RPC frame,
// even when JSON escapes every character. Missing legacy identity stays empty.
func bounded(v Event) Event {
	v.ActorExecutable = text(v.ActorExecutable, 1024)
	v.ActorInstance = text(v.ActorInstance, 128)
	v.ActorCodeID = text(v.ActorCodeID, 128)
	v.KeyName = text(v.KeyName, 128)
	v.Executable = text(v.Executable, 1024)
	v.Bundle = text(v.Bundle, 256)
	v.Endpoint = text(v.Endpoint, 512)
	v.HostFingerprint = text(v.HostFingerprint, 128)
	v.KeyFingerprint = text(v.KeyFingerprint, 128)
	v.Authorization = text(v.Authorization, 64)
	v.Permission = text(v.Permission, 32)
	v.ReadRoot = text(v.ReadRoot, 4096)
	v.WriteRoot = text(v.WriteRoot, 4096)
	v.ProcessInstance = text(v.ProcessInstance, 128)
	v.ExecutableID = text(v.ExecutableID, 128)
	switch v.Outcome {
	case "success", "failed", "cancelled", "unknown", "":
	default:
		v.Outcome = "unknown"
	}
	v.Generation = text(v.Generation, 64)
	v.ParentID = text(v.ParentID, 128)
	v.Event = text(v.Event, 64)
	v.RequestID = text(v.RequestID, 128)
	v.Host = text(v.Host, 128)
	v.Kind = text(v.Kind, 64)
	v.OperationHash = text(v.OperationHash, 64)
	v.Client = text(v.Client, 128)
	v.HostName = text(v.HostName, 256)
	v.Reason = text(v.Reason, 64)
	v.Command = text(v.Command, 32768)
	v.Path = text(v.Path, 4096)
	v.Destination = text(v.Destination, 4096)
	v.Object = text(v.Object, 128)
	return v
}

func text(s string, limit int) string {
	if len(s) > limit {
		s = s[:limit]
	}
	for !utf8.ValidString(s) {
		s = s[:len(s)-1]
	}
	return s
}
