package hardware

import (
	"crypto"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/sha512"
	"testing"
	"unsafe"
)

func TestPKCS11SigningMatchesRSASHA2AndNISTECDSA(t *testing.T) {
	private, e := rsa.GenerateKey(rand.Reader, 2048)
	if e != nil {
		t.Fatal(e)
	}
	for _, hash := range []crypto.Hash{crypto.SHA256, crypto.SHA384, crypto.SHA512} {
		t.Run(hash.String(), func(t *testing.T) {
			module := &nativeModule{public: &private.PublicKey}
			module.signInit = func(_ word, parameters unsafe.Pointer, _ word) word {
				if getWord(unsafe.Slice((*byte)(parameters), mechanismSize)) != 1 {
					t.Error("RSA did not use PKCS#1 v1.5 mechanism")
				}
				return 0
			}
			module.signValue = func(_ word, input unsafe.Pointer, length word, output unsafe.Pointer, size *word) word {
				data := unsafe.Slice((*byte)(input), int(length))
				signature, e := rsa.SignPKCS1v15(rand.Reader, private, crypto.Hash(0), data)
				if e != nil {
					t.Error(e)
					return 5
				}
				copy(unsafe.Slice((*byte)(output), int(*size)), signature)
				*size = word(len(signature))
				return 0
			}
			var digest []byte
			switch hash {
			case crypto.SHA256:
				sum := sha256.Sum256([]byte("fixture"))
				digest = sum[:]
			case crypto.SHA384:
				sum := sha512.Sum384([]byte("fixture"))
				digest = sum[:]
			case crypto.SHA512:
				sum := sha512.Sum512([]byte("fixture"))
				digest = sum[:]
			}
			signature, e := module.sign(digest, hash)
			if e != nil || rsa.VerifyPKCS1v15(&private.PublicKey, hash, digest, signature) != nil {
				t.Fatal("RSA digest-info prefix or signature conversion incorrect", e)
			}
		})
	}
	for _, curve := range []elliptic.Curve{elliptic.P256(), elliptic.P384(), elliptic.P521()} {
		t.Run(curve.Params().Name, func(t *testing.T) {
			key, e := ecdsa.GenerateKey(curve, rand.Reader)
			if e != nil {
				t.Fatal(e)
			}
			module := &nativeModule{public: &key.PublicKey, pin: []byte("synthetic PIN"), alwaysAuthenticate: true}
			initialized := false
			logins := 0
			module.signInit = func(_ word, parameters unsafe.Pointer, _ word) word {
				initialized = getWord(unsafe.Slice((*byte)(parameters), mechanismSize)) == 0x1041
				return 0
			}
			module.login = func(_ word, user word, _ unsafe.Pointer, length word) word {
				if !initialized || user != 2 || int(length) != len(module.pin) {
					t.Error("context authentication occurred outside SignInit")
				}
				logins++
				return 0
			}
			module.signValue = func(_ word, input unsafe.Pointer, length word, output unsafe.Pointer, size *word) word {
				data := unsafe.Slice((*byte)(input), int(length))
				r, s, e := ecdsa.Sign(rand.Reader, key, data)
				if e != nil {
					t.Error(e)
					return 5
				}
				width := (curve.Params().BitSize + 7) / 8
				signature := make([]byte, 2*width)
				r.FillBytes(signature[:width])
				s.FillBytes(signature[width:])
				copy(unsafe.Slice((*byte)(output), int(*size)), signature)
				*size = word(len(signature))
				return 0
			}
			digest := sha256.Sum256([]byte("fixture"))
			signature, e := module.sign(digest[:], crypto.SHA256)
			if e != nil || !ecdsa.VerifyASN1(&key.PublicKey, digest[:], signature) || logins != 1 {
				t.Fatal("ECDSA raw R/S conversion or context authentication incorrect", e)
			}
			// A bad PIN aborts before a provider can sign; it is never retried.
			logins = 0
			module.login = func(word, word, unsafe.Pointer, word) word { logins++; return 0xa0 }
			module.signValue = func(word, unsafe.Pointer, word, unsafe.Pointer, *word) word {
				t.Error("signed after context authentication failed")
				return 5
			}
			if _, e := module.sign(digest[:], crypto.SHA256); e == nil || logins != 1 {
				t.Fatal("context PIN failure was ignored or retried", e)
			}
		})
	}
}
