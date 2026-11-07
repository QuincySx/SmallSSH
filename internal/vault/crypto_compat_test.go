package vault

import (
	"encoding/hex"
	"testing"
)

const (
	publicGoldenKeyHex    = "000102030405060708090a0b0c0d0e0f101112131415161718191a1b1c1d1e1f"
	publicGoldenIDHex     = "101112131415161718191a1b1c1d1e1f"
	publicGoldenMessage   = "root-material-public-test-message"
	publicGoldenProofHex  = "3262ac3ee80e44e2c19efaf362b8ee858907d2a55ee56edf96fa9ac70a3908d8"
	publicGoldenBrokerPin = "sha256:47a1d77ee1921e2fea5b8a0e4f1829fdaf77251fb0fea06cbd6facb0a2e6302c"
)

func newPublicGoldenSession(t *testing.T) *Session {
	t.Helper()

	key, err := hex.DecodeString(publicGoldenKeyHex)
	if err != nil {
		t.Fatalf("decode public golden key: %v", err)
	}
	t.Cleanup(func() { wipe(key) })

	id, err := hex.DecodeString(publicGoldenIDHex)
	if err != nil {
		t.Fatalf("decode public golden ID: %v", err)
	}

	s := &Session{
		header: Header{
			Version: Version,
			Salt:    []byte{0x00, 0x01, 0x02, 0x03, 0x04, 0x05, 0x06, 0x07, 0x08, 0x09, 0x0a, 0x0b, 0x0c, 0x0d, 0x0e, 0x0f},
			ID:      id,
		},
		key: key,
	}
	t.Cleanup(s.Close)
	return s
}

func TestSessionProofPublicGolden(t *testing.T) {
	s := newPublicGoldenSession(t)

	proof, err := s.Proof([]byte(publicGoldenMessage))
	if err != nil {
		t.Fatalf("Session.Proof: %v", err)
	}
	defer wipe(proof)

	if got := hex.EncodeToString(proof); got != publicGoldenProofHex {
		t.Fatalf("Session.Proof = %s, want %s", got, publicGoldenProofHex)
	}
}

func TestSessionBrokerPublicKeyPinPublicGolden(t *testing.T) {
	s := newPublicGoldenSession(t)

	pin, err := s.BrokerPublicKeyPin()
	if err != nil {
		t.Fatalf("Session.BrokerPublicKeyPin: %v", err)
	}
	if pin != publicGoldenBrokerPin {
		t.Fatalf("Session.BrokerPublicKeyPin = %s, want %s", pin, publicGoldenBrokerPin)
	}
}
