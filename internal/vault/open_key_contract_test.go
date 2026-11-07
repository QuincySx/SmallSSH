package vault

import (
	"bytes"
	"crypto/aes"
	"crypto/cipher"
	"encoding/hex"
	"encoding/json"
	"errors"
	"testing"
)

func openKeyContractClone(b []byte) []byte {
	return append([]byte(nil), b...)
}

func openKeyContractGoldenKey(t *testing.T) []byte {
	t.Helper()

	key, err := hex.DecodeString(publicGoldenKeyHex)
	if err != nil {
		t.Fatalf("decode public golden key: %v", err)
	}
	t.Cleanup(func() { wipe(key) })
	return key
}

func openKeyContractDecodeEnvelope(t *testing.T, data []byte) envelope {
	t.Helper()

	var env envelope
	if err := json.Unmarshal(data, &env); err != nil {
		t.Fatalf("decode public envelope: %v", err)
	}
	return env
}

func openKeyContractSeal(t *testing.T, header Header, key, nonce, plain []byte) []byte {
	t.Helper()

	block, err := aes.NewCipher(key)
	if err != nil {
		t.Fatalf("aes.NewCipher: %v", err)
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		t.Fatalf("cipher.NewGCM: %v", err)
	}
	aad, err := json.Marshal(header)
	if err != nil {
		t.Fatalf("marshal public header: %v", err)
	}
	ciphertext := aead.Seal(nil, nonce, plain, aad)
	t.Cleanup(func() { wipe(ciphertext) })

	data, err := json.Marshal(envelope{Header: header, Nonce: openKeyContractClone(nonce), Ciphertext: ciphertext})
	if err != nil {
		t.Fatalf("marshal public envelope: %v", err)
	}
	return data
}

func openKeyContractPublicNonce() []byte {
	return []byte{0x20, 0x21, 0x22, 0x23, 0x24, 0x25, 0x26, 0x27, 0x28, 0x29, 0x2a, 0x2b}
}

func TestOpenKeyRejectsInvalidEnvelopeContract(t *testing.T) {
	source := newPublicGoldenSession(t)
	sealed, err := source.Seal()
	if err != nil {
		t.Fatalf("Session.Seal: %v", err)
	}
	t.Cleanup(func() { wipe(sealed) })

	validKey := openKeyContractGoldenKey(t)
	validEnv := openKeyContractDecodeEnvelope(t, sealed)
	normalPlain := []byte(`{"profiles":[]}`)
	t.Cleanup(func() { wipe(normalPlain) })

	badJSON := []byte(`{"header":`)
	t.Cleanup(func() { wipe(badJSON) })

	badVersionHeader := validEnv.Header
	badVersionHeader.Version = Version + 1
	badVersion := openKeyContractSeal(t, badVersionHeader, validKey, openKeyContractPublicNonce(), normalPlain)
	t.Cleanup(func() { wipe(badVersion) })

	badSaltHeader := validEnv.Header
	badSaltHeader.Salt = openKeyContractClone(validEnv.Header.Salt[:15])
	t.Cleanup(func() { wipe(badSaltHeader.Salt) })
	badSalt := openKeyContractSeal(t, badSaltHeader, validKey, openKeyContractPublicNonce(), normalPlain)
	t.Cleanup(func() { wipe(badSalt) })

	badIDHeader := validEnv.Header
	badIDHeader.ID = openKeyContractClone(validEnv.Header.ID[:15])
	t.Cleanup(func() { wipe(badIDHeader.ID) })
	badID := openKeyContractSeal(t, badIDHeader, validKey, openKeyContractPublicNonce(), normalPlain)
	t.Cleanup(func() { wipe(badID) })

	badNonceEnv := validEnv
	badNonceEnv.Nonce = openKeyContractClone(validEnv.Nonce[:11])
	t.Cleanup(func() { wipe(badNonceEnv.Nonce) })
	badNonce, err := json.Marshal(badNonceEnv)
	if err != nil {
		t.Fatalf("marshal short nonce envelope: %v", err)
	}
	t.Cleanup(func() { wipe(badNonce) })

	shortKey := openKeyContractClone(validKey[:31])
	t.Cleanup(func() { wipe(shortKey) })

	badCiphertextEnv := openKeyContractDecodeEnvelope(t, sealed)
	if len(badCiphertextEnv.Ciphertext) == 0 {
		t.Fatal("public sealed envelope has an empty ciphertext")
	}
	badCiphertextEnv.Ciphertext[0] ^= 0x01
	badCiphertext, err := json.Marshal(badCiphertextEnv)
	if err != nil {
		t.Fatalf("marshal tampered ciphertext envelope: %v", err)
	}
	t.Cleanup(func() { wipe(badCiphertext) })

	badAuthenticatedHeaderEnv := openKeyContractDecodeEnvelope(t, sealed)
	badAuthenticatedHeaderEnv.Header.Salt[0] ^= 0x01
	badAuthenticatedHeader, err := json.Marshal(badAuthenticatedHeaderEnv)
	if err != nil {
		t.Fatalf("marshal tampered header envelope: %v", err)
	}
	t.Cleanup(func() { wipe(badAuthenticatedHeader) })

	tests := []struct {
		name string
		data []byte
		key  []byte
	}{
		{name: "malformed json envelope", data: badJSON, key: validKey},
		{name: "version rejected after authenticated encryption", data: badVersion, key: validKey},
		{name: "salt length rejected after authenticated encryption", data: badSalt, key: validKey},
		{name: "id length rejected after authenticated encryption", data: badID, key: validKey},
		{name: "nonce length", data: badNonce, key: validKey},
		{name: "key length", data: sealed, key: shortKey},
		{name: "ciphertext tamper", data: badCiphertext, key: validKey},
		{name: "header authentication failure", data: badAuthenticatedHeader, key: validKey},
	}

	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			data := openKeyContractClone(test.data)
			t.Cleanup(func() { wipe(data) })
			key := openKeyContractClone(test.key)
			t.Cleanup(func() { wipe(key) })

			dataBefore := openKeyContractClone(data)
			t.Cleanup(func() { wipe(dataBefore) })
			keyBefore := openKeyContractClone(key)
			t.Cleanup(func() { wipe(keyBefore) })

			session, err := OpenKey(data, key)
			if session != nil {
				session.Close()
				t.Fatal("OpenKey returned a non-nil Session on failure")
			}
			if !errors.Is(err, ErrUnlock) {
				t.Fatalf("OpenKey error = %v, want ErrUnlock", err)
			}
			if !bytes.Equal(data, dataBefore) {
				t.Fatal("OpenKey changed caller data on failure")
			}
			if !bytes.Equal(key, keyBefore) {
				t.Fatal("OpenKey changed caller key on failure")
			}
		})
	}
}

func TestOpenKeyAuthenticatedEnvelopeStillParsesCompletePayload(t *testing.T) {
	source := newPublicGoldenSession(t)
	sealed, err := source.Seal()
	if err != nil {
		t.Fatalf("Session.Seal: %v", err)
	}
	t.Cleanup(func() { wipe(sealed) })

	validKey := openKeyContractGoldenKey(t)
	header := openKeyContractDecodeEnvelope(t, sealed).Header

	normalPlain := []byte(`{"profiles":[]}`)
	t.Cleanup(func() { wipe(normalPlain) })
	normalData := openKeyContractSeal(t, header, validKey, openKeyContractPublicNonce(), normalPlain)
	t.Cleanup(func() { wipe(normalData) })

	session, err := OpenKey(normalData, validKey)
	if err != nil {
		t.Fatalf("OpenKey normal authenticated payload: %v", err)
	}
	closed := false
	t.Cleanup(func() {
		if !closed {
			session.Close()
		}
	})

	proof, err := session.Proof([]byte(publicGoldenMessage))
	if err != nil {
		t.Fatalf("Session.Proof: %v", err)
	}
	if got := hex.EncodeToString(proof); got != publicGoldenProofHex {
		wipe(proof)
		t.Fatalf("Session.Proof = %s, want %s", got, publicGoldenProofHex)
	}
	wipe(proof)
	session.Close()
	closed = true

	invalidPlains := []struct {
		name  string
		plain []byte
	}{
		{name: "malformed payload json", plain: []byte(`{`)},
		{name: "valid payload json with wrong field type", plain: []byte(`{"profiles":"wrong type"}`)},
	}
	for _, test := range invalidPlains {
		test := test
		t.Cleanup(func() { wipe(test.plain) })
		t.Run(test.name, func(t *testing.T) {
			key := openKeyContractClone(validKey)
			t.Cleanup(func() { wipe(key) })
			data := openKeyContractSeal(t, header, key, openKeyContractPublicNonce(), test.plain)
			t.Cleanup(func() { wipe(data) })

			keyBefore := openKeyContractClone(key)
			t.Cleanup(func() { wipe(keyBefore) })
			dataBefore := openKeyContractClone(data)
			t.Cleanup(func() { wipe(dataBefore) })

			session, err := OpenKey(data, key)
			if session != nil {
				session.Close()
				t.Fatal("OpenKey returned a non-nil Session for an invalid payload")
			}
			if !errors.Is(err, ErrUnlock) {
				t.Fatalf("OpenKey error = %v, want ErrUnlock", err)
			}
			if !bytes.Equal(key, keyBefore) {
				t.Fatal("OpenKey changed caller key for an invalid payload")
			}
			if !bytes.Equal(data, dataBefore) {
				t.Fatal("OpenKey changed caller data for an invalid payload")
			}
		})
	}
}
