// Package auditlog defines the metadata-only, local operational history.
// It is deliberately not a tamper-proof forensic ledger.
package auditlog

import (
	"time"
	"unicode/utf8"
)

type Event struct {
	Outcome       string    `json:"outcome,omitempty"`
	ExitCode      *int      `json:"exitCode,omitempty"`
	Generation    string    `json:"generation,omitempty"`
	ParentID      string    `json:"parentId,omitempty"`
	OwnerIssued   bool      `json:"ownerIssued,omitempty"`
	OwnerSession  bool      `json:"ownerSession,omitempty"`
	Renewals      int       `json:"renewals,omitempty"`
	Time          time.Time `json:"time"`
	Event         string    `json:"event"`
	RequestID     string    `json:"requestId,omitempty"`
	Host          string    `json:"hostId,omitempty"`
	Kind          string    `json:"kind,omitempty"`
	OperationHash string    `json:"operationHash,omitempty"`
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
	Before string `json:"before,omitempty"`
	Limit  int    `json:"limit,omitempty"`
}

type Page struct {
	Events []Event `json:"events"`
	Next   string  `json:"next,omitempty"`
}

// Historical files can contain damaged or externally written metadata. Bound
// each displayed field so a valid 100-row page also fits the local RPC frame,
// even when JSON escapes every character. Missing legacy identity stays empty.
func bounded(v Event) Event {
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
	v.Command = text(v.Command, 512)
	v.Path = text(v.Path, 512)
	v.Destination = text(v.Destination, 512)
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
