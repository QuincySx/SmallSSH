package protocol

import "time"

const (
	ServiceProtocolMajor       = 1
	ServiceProtocolMinor       = 3 // 2: in-memory quick unlock; 3: locked-vault notice
	ServiceUnlockNoticeMinor   = 3
	ServiceActionRequestUnlock = "requestUnlock"
	ServiceStateLocked         = "locked"
	ServiceStateUnlocked       = "unlocked"
	ServiceStateStopping       = "stopping"
)

type ServiceMessage struct {
	Action       string            `json:"action"`
	Major        int               `json:"major"`
	Minor        int               `json:"minor"`
	Epoch        string            `json:"epoch"`
	Status       *ServiceStatus    `json:"status,omitempty"`
	ErrorCode    string            `json:"errorCode,omitempty"`
	Error        string            `json:"error,omitempty"`
	Challenge    string            `json:"challenge,omitempty"`
	Generation   uint64            `json:"generation,omitempty"`
	ID           string            `json:"id,omitempty"`
	OwnerPrepare *OwnerPrepare     `json:"ownerPrepare,omitempty"`
	UnlockKey    []byte            `json:"unlockKey,omitempty"`
	Proof        []byte            `json:"proof,omitempty"`
	OwnerInfo    *ServiceOwnerInfo `json:"ownerInfo,omitempty"`
	// QuickUnlock is an opaque system-verification envelope. It is secret
	// material: callers clear it after use, like UnlockKey.
	QuickUnlock      []byte            `json:"quickUnlock,omitempty"`
	QuickUnlockState *QuickUnlockState `json:"quickUnlockState,omitempty"`
}

// QuickUnlockState says whether the service holds an envelope this caller may
// use. It never carries the envelope itself.
type QuickUnlockState struct {
	Available bool      `json:"available"`
	Expires   time.Time `json:"expires,omitempty"`
}

type ServiceStatus struct {
	State             string     `json:"state"`
	StartedAt         time.Time  `json:"startedAt"`
	ProtocolMajor     int        `json:"protocolMajor"`
	ProtocolMinor     int        `json:"protocolMinor"`
	Generation        uint64     `json:"generation"`
	UnlockNoticeUntil *time.Time `json:"unlockNoticeUntil,omitempty"`
}
