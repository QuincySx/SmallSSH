// Package identitymodel contains public metadata for SSH signing identities.
// It never carries private key material or server-operation grants.
package identitymodel

import "path/filepath"

type Key struct {
	ID             string `json:"id"`
	Name           string `json:"name"`
	Authentication string `json:"authentication"`
	PublicKey      string `json:"publicKey"`
	Fingerprint    string `json:"fingerprint"`
}

type State struct {
	Supported     bool    `json:"supported"`
	Keys          []Key   `json:"keys"`
	Socket        string  `json:"socket"`
	Pending       int     `json:"pending"`
	Notifications string  `json:"notifications"`
	Leases        []Lease `json:"leases,omitempty"`
}

// Lease authorizes signatures with one key for all local SSH-agent callers.
// It never authorizes commands on a server or disconnects an established SSH session.
type Lease struct {
	ID          string `json:"id"`
	Key         string `json:"key"`
	Started     int64  `json:"started"`
	Expires     int64  `json:"expires"`
	LastUsed    int64  `json:"lastUsed"`
	IdleSeconds int    `json:"idleSeconds"`
}

type AuditEvent struct {
	Name            string `json:"name,omitempty"`
	PeerPID         int    `json:"peerPid,omitempty"`
	Executable      string `json:"executable,omitempty"`
	ProcessInstance string `json:"processInstance,omitempty"`
	ExecutableID    string `json:"executableId,omitempty"`
	ID              string `json:"id"`
	Time            int64  `json:"time"`
	Event           string `json:"event"`
	Key             string `json:"key"`
	Fingerprint     string `json:"fingerprint"`
	Authorization   string `json:"authorization"`
	Reason          string `json:"reason"`
	LeaseID         string `json:"leaseId"`
	Expires         int64  `json:"expires"`
	IdleSeconds     int    `json:"idleSeconds"`
	PreviousIdle    *int   `json:"previousIdle,omitempty"`
}

// Change is owner-only. No member is an SSH agent sign request or secret.
type Change struct {
	Action         string `json:"action"`
	ID             string `json:"id,omitempty"`
	Name           string `json:"name,omitempty"`
	Authentication string `json:"authentication,omitempty"`
	Seconds        int    `json:"seconds,omitempty"`
	LeaseID        string `json:"leaseId,omitempty"`
	IdleSeconds    int    `json:"idleSeconds,omitempty"`
}

func SocketPath(dir string) string { return filepath.Join(dir, "signing", "agent.sock") }
