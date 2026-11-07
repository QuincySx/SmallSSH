package vault

import (
	"bytes"
	"encoding/hex"
	"errors"
	"sync"
	"testing"
)

func openKeyOwnershipClone(b []byte) []byte {
	return append([]byte(nil), b...)
}

func openKeyOwnershipGoldenKey(t *testing.T) []byte {
	t.Helper()

	key, err := hex.DecodeString(publicGoldenKeyHex)
	if err != nil {
		t.Fatalf("decode public golden key: %v", err)
	}
	t.Cleanup(func() { wipe(key) })
	return key
}

func TestOpenKeyOwnershipCopiesCallerKey(t *testing.T) {
	source := newPublicGoldenSession(t)

	data, err := source.Seal()
	if err != nil {
		t.Fatalf("Session.Seal: %v", err)
	}
	t.Cleanup(func() { wipe(data) })

	dataBefore := openKeyOwnershipClone(data)
	t.Cleanup(func() { wipe(dataBefore) })

	callerKey := openKeyOwnershipGoldenKey(t)
	callerKeyBefore := openKeyOwnershipClone(callerKey)
	t.Cleanup(func() { wipe(callerKeyBefore) })

	session, err := OpenKey(data, callerKey)
	if err != nil {
		t.Fatalf("OpenKey: %v", err)
	}
	closed := false
	t.Cleanup(func() {
		if !closed {
			session.Close()
		}
	})

	if !bytes.Equal(callerKey, callerKeyBefore) {
		t.Fatal("OpenKey changed the caller key")
	}

	for i := range callerKey {
		callerKey[i] ^= 0xff
	}
	mutatedCallerKey := openKeyOwnershipClone(callerKey)
	t.Cleanup(func() { wipe(mutatedCallerKey) })
	if bytes.Equal(callerKey, callerKeyBefore) {
		t.Fatal("caller key mutation did not take effect")
	}

	proof, err := session.Proof([]byte(publicGoldenMessage))
	if err != nil {
		t.Fatalf("Session.Proof: %v", err)
	}
	defer wipe(proof)
	if got := hex.EncodeToString(proof); got != publicGoldenProofHex {
		t.Fatalf("Session.Proof = %s, want %s", got, publicGoldenProofHex)
	}

	pin, err := session.BrokerPublicKeyPin()
	if err != nil {
		t.Fatalf("Session.BrokerPublicKeyPin: %v", err)
	}
	if pin != publicGoldenBrokerPin {
		t.Fatalf("Session.BrokerPublicKeyPin = %s, want %s", pin, publicGoldenBrokerPin)
	}

	if !bytes.Equal(data, dataBefore) {
		t.Fatal("OpenKey changed the caller envelope data")
	}

	session.Close()
	closed = true
	if !bytes.Equal(callerKey, mutatedCallerKey) {
		t.Fatal("Session.Close changed the caller key")
	}
	if !bytes.Equal(data, dataBefore) {
		t.Fatal("Session.Close changed the caller envelope data")
	}
}

func TestOpenKeyOwnershipFailurePreservesCallerInputs(t *testing.T) {
	source := newPublicGoldenSession(t)

	sealed, err := source.Seal()
	if err != nil {
		t.Fatalf("Session.Seal: %v", err)
	}
	t.Cleanup(func() { wipe(sealed) })

	validKey := openKeyOwnershipGoldenKey(t)

	shortKey := openKeyOwnershipClone(validKey[:len(validKey)-1])
	t.Cleanup(func() { wipe(shortKey) })

	wrongKey := openKeyOwnershipClone(validKey)
	t.Cleanup(func() { wipe(wrongKey) })
	wrongKey[0] ^= 0xff

	tampered := openKeyOwnershipClone(sealed)
	t.Cleanup(func() { wipe(tampered) })
	if len(tampered) < 3 || tampered[len(tampered)-1] != '}' {
		t.Fatal("sealed envelope has an unexpected JSON shape")
	}
	closingQuote := len(tampered) - 2
	if tampered[closingQuote] != '"' {
		t.Fatal("sealed envelope does not end with a string value")
	}
	openingQuote := bytes.LastIndexByte(tampered[:closingQuote], '"')
	if openingQuote < 0 || openingQuote+1 >= closingQuote {
		t.Fatal("sealed envelope has an empty final string value")
	}
	valueStart := openingQuote + 1
	if tampered[valueStart] == 'A' {
		tampered[valueStart] = 'B'
	} else {
		tampered[valueStart] = 'A'
	}

	tests := []struct {
		name string
		data []byte
		key  []byte
	}{
		{name: "short key", data: sealed, key: shortKey},
		{name: "wrong key", data: sealed, key: wrongKey},
		{name: "tampered envelope", data: tampered, key: validKey},
	}

	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			data := openKeyOwnershipClone(test.data)
			t.Cleanup(func() { wipe(data) })
			key := openKeyOwnershipClone(test.key)
			t.Cleanup(func() { wipe(key) })

			dataBefore := openKeyOwnershipClone(data)
			t.Cleanup(func() { wipe(dataBefore) })
			keyBefore := openKeyOwnershipClone(key)
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

type openKeyOwnershipProofResult struct {
	proof []byte
	err   error
}

func TestOpenKeyOwnershipConcurrentProofAndClose(t *testing.T) {
	source := newPublicGoldenSession(t)

	data, err := source.Seal()
	if err != nil {
		t.Fatalf("Session.Seal: %v", err)
	}
	t.Cleanup(func() { wipe(data) })

	key := openKeyOwnershipGoldenKey(t)
	session, err := OpenKey(data, key)
	if err != nil {
		t.Fatalf("OpenKey: %v", err)
	}
	closed := false
	t.Cleanup(func() {
		if !closed {
			session.Close()
		}
	})

	const workers = 8
	start := make(chan struct{})
	results := make(chan openKeyOwnershipProofResult, workers)
	var wg sync.WaitGroup
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			proof, err := session.Proof([]byte(publicGoldenMessage))
			results <- openKeyOwnershipProofResult{proof: proof, err: err}
		}()
	}

	close(start)
	session.Close()
	closed = true
	wg.Wait()
	close(results)

	for result := range results {
		if result.err != nil {
			wipe(result.proof)
			if result.err.Error() != "保险库已锁定" {
				t.Fatalf("concurrent Session.Proof error = %v, want closed session", result.err)
			}
			if result.proof != nil {
				t.Fatal("closed Session.Proof returned proof bytes with an error")
			}
			continue
		}
		if got := hex.EncodeToString(result.proof); got != publicGoldenProofHex {
			wipe(result.proof)
			t.Fatalf("concurrent Session.Proof = %s, want %s", got, publicGoldenProofHex)
		}
		wipe(result.proof)
	}
}
