package ownercap

import (
	"crypto/ed25519"
	"errors"
)

const (
	sessionIDSize   = 32
	proofDomain     = "smallssh/ownercap/proof/v1"
	signatureDomain = "smallssh/ownercap/signature/v1\x00"
)

var errMalformedProof = errors.New("会话证明格式无效")

func validEntropyHex(value string) bool {
	if len(value) != sessionIDSize {
		return false
	}
	nonzero := false
	for index := range len(value) {
		character := value[index]
		if character >= '1' && character <= '9' || character >= 'a' && character <= 'f' {
			nonzero = true
		} else if character != '0' {
			return false
		}
	}
	return nonzero
}

func signingInput(epoch, sessionID string, payload []byte) []byte {
	message := make([]byte, 0, len(signatureDomain)+len(epoch)+len(sessionID)+len(payload))
	message = append(message, signatureDomain...)
	message = append(message, epoch...)
	message = append(message, sessionID...)
	message = append(message, payload...)
	return message
}

func encodeProof(sessionID string, signature []byte) []byte {
	proof := make([]byte, 0, len(proofDomain)+sessionIDSize+1+ed25519.SignatureSize)
	proof = append(proof, proofDomain...)
	proof = append(proof, sessionID...)
	proof = append(proof, 0)
	proof = append(proof, signature...)
	return proof
}

func decodeProof(proof []byte) (string, []byte, error) {
	expected := len(proofDomain) + sessionIDSize + 1 + ed25519.SignatureSize
	if len(proof) != expected || string(proof[:len(proofDomain)]) != proofDomain {
		return "", nil, errMalformedProof
	}
	idStart := len(proofDomain)
	idEnd := idStart + sessionIDSize
	if proof[idEnd] != 0 {
		return "", nil, errMalformedProof
	}
	sessionID := string(proof[idStart:idEnd])
	if !validEntropyHex(sessionID) {
		return "", nil, errMalformedProof
	}
	return sessionID, proof[idEnd+1:], nil
}
