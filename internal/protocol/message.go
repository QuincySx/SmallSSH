// Package protocol defines the request and response contract shared by the
// desktop, client, and broker. It does not grant authority or perform I/O.
package protocol

import (
	"sshclient/internal/approval"
	"sshclient/internal/auditlog"
	"sshclient/internal/connectcfg"
	"sshclient/internal/fileattrs"
	"sshclient/internal/monitor"
	"sshclient/internal/serverops"
	"sshclient/internal/sshdata"
	"sshclient/internal/vault"
)

const ErrorUploadTargetExists = "upload_target_exists"

// Message is the local wire protocol. Credentials are never response fields.
type Message struct {
	RouteMeasurements   []sshdata.RouteMeasurement `json:"routeMeasurements,omitempty"`
	ExpectedFingerprint string                     `json:"expectedFingerprint,omitempty"`
	ListenAddress       string                     `json:"listenAddress,omitempty"`
	ConnectionRoute     *connectcfg.RouteSelection `json:"connectionRoute,omitempty"`
	Network             *connectcfg.Network        `json:"network,omitempty"`
	KeepProxyCredential bool                       `json:"keepProxyCredential,omitempty"`
	// Channel locates a waiting operation connection. It is not authority:
	// only its live parent connection can authorize an operation on it.
	Channel   string `json:"channel,omitempty"`
	Challenge string `json:"challenge,omitempty"`
	After     string `json:"after,omitempty"`
	Next      string `json:"next,omitempty"`
	Limit     int    `json:"limit,omitempty"`
	// Redacted marks a host list served without owner proof: connection IDs,
	// names and groups only.
	Redacted      bool                          `json:"redacted,omitempty"`
	AuditQuery    *auditlog.Query               `json:"auditQuery,omitempty"`
	AuditPage     *auditlog.Page                `json:"auditPage,omitempty"`
	Renewal       *approval.RenewalChallenge    `json:"renewal,omitempty"`
	ErrorCode     string                        `json:"errorCode,omitempty"`
	ComposePlan   *serverops.ComposePlan        `json:"composePlan,omitempty"`
	ComposeStatus []serverops.ComposeStatus     `json:"composeStatus,omitempty"`
	Phase         string                        `json:"phase,omitempty"`
	FileInfo      *fileattrs.Info               `json:"fileInfo,omitempty"`
	Delegation    *approval.DelegationChallenge `json:"delegation,omitempty"`
	HostIdentity  *sshdata.HostIdentity         `json:"hostIdentity,omitempty"`
	Identities    []sshdata.KeyInfo             `json:"identities,omitempty"`
	ProfileChange *vault.ProfileChange          `json:"profileChange,omitempty"`
	Port          int                           `json:"port,omitempty"`
	MonitorError  string                        `json:"monitorError,omitempty"`
	// MonitorActive is whether the client still shows this session's server
	// status. The broker samples a server only while some subscriber shows it.
	MonitorActive    bool                  `json:"monitorActive,omitempty"`
	Transfer         *sshdata.TransferMeta `json:"transfer,omitempty"`
	Stats            *monitor.Stats        `json:"stats,omitempty"`
	Action           string                `json:"action,omitempty"`
	Operation        approval.Operation    `json:"operation,omitempty"`
	TTL              int                   `json:"ttl,omitempty"`
	Idle             int                   `json:"idle,omitempty"`
	SkipMediumReview bool                  `json:"skipMediumReview,omitempty"`
	ID               string                `json:"id,omitempty"`
	Ticket           string                `json:"ticket,omitempty"`
	Allow            bool                  `json:"allow,omitempty"`
	Proof            []byte                `json:"proof,omitempty"`
	Request          *approval.Request     `json:"request,omitempty"`
	Requests         []approval.Request    `json:"requests,omitempty"`
	Hosts            []string              `json:"hosts,omitempty"`
	Files            []sshdata.File        `json:"files,omitempty"`
	Data             []byte                `json:"data,omitempty"`
	Cols             int                   `json:"cols,omitempty"`
	Rows             int                   `json:"rows,omitempty"`
	ExitCode         int                   `json:"exitCode,omitempty"`
	Error            string                `json:"error,omitempty"`
	Profile          *vault.Profile        `json:"profile,omitempty"`
	Routes           []vault.RouteSummary  `json:"routes,omitempty"`
	Connections      []vault.Summary       `json:"connections,omitempty"`
	Details          *vault.Summary        `json:"details,omitempty"`
	Target           string                `json:"target,omitempty"`
	Workspace        *WorkspaceQuery       `json:"workspace,omitempty"`
	WorkspaceResult  *WorkspaceResult      `json:"workspaceResult,omitempty"`
	WorkspaceChange  *WorkspaceChange      `json:"workspaceChange,omitempty"`
}
