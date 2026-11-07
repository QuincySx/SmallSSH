package client

import "reflect"

// OwnerProof is the only capability the SDK needs from an already-unlocked
// owner source. It proves requests; it does not expose vault data or authority.
type OwnerProof interface {
	Proof([]byte) ([]byte, error)
}

// HasOwnerProof rejects both a nil interface and a typed-nil implementation.
// This is a local plumbing check, not an authorization decision.
func HasOwnerProof(source OwnerProof) bool {
	if source == nil {
		return false
	}
	value := reflect.ValueOf(source)
	switch value.Kind() {
	case reflect.Chan, reflect.Func, reflect.Interface, reflect.Map, reflect.Ptr, reflect.Slice:
		return !value.IsNil()
	default:
		return true
	}
}
