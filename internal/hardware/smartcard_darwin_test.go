package hardware

import (
	"crypto"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"runtime"
	"testing"

	"github.com/ebitengine/purego"
	"github.com/ebitengine/purego/objc"
)

// Use an ephemeral software key to verify the Security.framework calling
// convention, digest handling, and DER signature format without a smart card.
func TestMacSmartCardBridgeSignsECDSADigest(t *testing.T) {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	session, err := openMacSmartCard()
	if err != nil {
		t.Fatal(err)
	}
	m := session.(*macSmartCard)
	defer m.Close()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	pub, err := key.PublicKey.ECDH()
	if err != nil {
		t.Fatal(err)
	}
	// Security.framework uses the uncompressed public point followed by the
	// fixed-width private scalar for an EC private key.
	raw := append(pub.Bytes(), key.D.FillBytes(make([]byte, 32))...)
	defer clear(raw)
	data := m.data(0, &raw[0], int64(len(raw)))
	runtime.KeepAlive(raw)
	if data == 0 {
		t.Fatal("could not create test key data")
	}
	defer m.release(data)
	attrs := objc.ID(objc.GetClass("NSMutableDictionary")).Send(objc.RegisterName("new"))
	defer attrs.Send(objc.RegisterName("release"))
	set := objc.RegisterName("setObject:forKey:")
	attrs.Send(set, m.ecType, m.keyType)
	attrs.Send(set, m.privateKey, m.keyClass)
	security, err := purego.Dlopen("/System/Library/Frameworks/Security.framework/Security", purego.RTLD_NOW|purego.RTLD_LOCAL)
	if err != nil {
		t.Fatal(err)
	}
	var createKey func(objc.ID, objc.ID, *objc.ID) objc.ID
	purego.RegisterLibFunc(&createKey, security, "SecKeyCreateWithData")
	m.selected = createKey(data, attrs, nil)
	if m.selected == 0 {
		t.Fatal("cannot create ephemeral Security.framework key")
	}
	m.selectedAlgorithm = "ecdsa-sha2-nistp256"
	digest := sha256.Sum256([]byte("synthetic SSH public-key session"))
	signature, err := m.sign(digest[:], crypto.SHA256)
	if err != nil {
		t.Fatal(err)
	}
	if !ecdsa.VerifyASN1(&key.PublicKey, digest[:], signature) {
		t.Fatal("Security.framework returned an invalid signature")
	}
}
