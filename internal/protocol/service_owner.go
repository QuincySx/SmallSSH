package protocol

import (
	"encoding/json"
	"time"
)

const serviceOwnerPrepareDomain = "smallssh/service-owner/prepare/v1\x00"

// OwnerPrepare is the data bound into a service-owner prepare request. KeyHash
// identifies the unlock key without placing that key in the signed bytes.
type OwnerPrepare struct {
	Epoch           string `json:"epoch"`
	Challenge       string `json:"challenge"`
	Binding         string `json:"binding"`
	Generation      uint64 `json:"generation"`
	PublicKey       []byte `json:"publicKey"`
	LifetimeSeconds int    `json:"lifetimeSeconds"`
	KeyHash         string `json:"keyHash"`
}

// ServiceOwnerInfo is public service-owner metadata. It carries no authority.
type ServiceOwnerInfo struct {
	ID        string    `json:"id"`
	Epoch     string    `json:"epoch"`
	Expires   time.Time `json:"expires"`
	BrokerKey string    `json:"brokerKey"`
}

// ServiceOwnerPreparePayload returns the deterministic bytes to sign for an
// owner prepare request. It performs no signing, validation, or I/O.
func ServiceOwnerPreparePayload(p OwnerPrepare) ([]byte, error) {
	body, err := json.Marshal(p)
	if err != nil {
		return nil, err
	}
	payload := make([]byte, 0, len(serviceOwnerPrepareDomain)+len(body))
	payload = append(payload, serviceOwnerPrepareDomain...)
	payload = append(payload, body...)
	return payload, nil
}
