package protocol

import "time"

const (
	ServiceProtocolMajor = 1
	ServiceProtocolMinor = 1
	ServiceStateLocked   = "locked"
	ServiceStateUnlocked = "unlocked"
	ServiceStateStopping = "stopping"
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
}

type ServiceStatus struct {
	State         string    `json:"state"`
	StartedAt     time.Time `json:"startedAt"`
	ProtocolMajor int       `json:"protocolMajor"`
	ProtocolMinor int       `json:"protocolMinor"`
	Generation    uint64    `json:"generation"`
}
