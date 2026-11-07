package protocol

import "encoding/json"

const serviceOwnerControlDomain = "smallssh/service-owner/control/v1\x00"

// ServiceOwnerControl is the data bound to a management lock or stop request.
// It carries no authority on its own.
type ServiceOwnerControl struct {
	Action     string `json:"action"`
	Epoch      string `json:"epoch"`
	Generation uint64 `json:"generation"`
	Binding    string `json:"binding"`
	OwnerID    string `json:"ownerID"`
}

// ServiceOwnerControlPayload returns the deterministic bytes to sign for a
// management control request. It performs no signing, validation, or I/O.
func ServiceOwnerControlPayload(p ServiceOwnerControl) ([]byte, error) {
	body, err := json.Marshal(p)
	if err != nil {
		return nil, err
	}
	payload := make([]byte, 0, len(serviceOwnerControlDomain)+len(body))
	payload = append(payload, serviceOwnerControlDomain...)
	payload = append(payload, body...)
	return payload, nil
}
