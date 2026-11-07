package vault

import (
	"crypto/ed25519"
	"crypto/hmac"
	"crypto/sha256"
	"crypto/x509"
	"encoding/hex"
)

// BrokerIdentity is an owner-only signing key, derived from the encrypted
// vault's key in a separate purpose domain. It is never part of a client grant
// or public RPC and does not survive outside an unlocked owner session.
func (s *Session) BrokerIdentity() (ed25519.PrivateKey, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return nil, ErrUnlock
	}
	return deriveBrokerIdentity(s.key, s.header.ID), nil
}

// deriveBrokerIdentity returns an independently owned signing key.
func deriveBrokerIdentity(key, id []byte) ed25519.PrivateKey {
	h := hmac.New(sha256.New, key)
	h.Write([]byte("smallssh/broker-identity/ed25519/v1\x00"))
	h.Write(id)
	seed := h.Sum(nil)
	defer wipe(seed)
	return ed25519.NewKeyFromSeed(seed)
}

func (s *Session) BrokerPublicKeyPin() (string, error) {
	key, err := s.BrokerIdentity()
	if err != nil {
		return "", err
	}
	defer wipe(key)
	return brokerPublicKeyPin(key)
}

// brokerPublicKeyPin hashes PKIX/SPKI DER, not the raw public key bytes.
func brokerPublicKeyPin(key ed25519.PrivateKey) (string, error) {
	public, err := x509.MarshalPKIXPublicKey(key.Public())
	if err != nil {
		return "", err
	}
	digest := sha256.Sum256(public)
	return "sha256:" + hex.EncodeToString(digest[:]), nil
}
