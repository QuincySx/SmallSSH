//go:build darwin || linux

package hardware

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/asn1"
	"math/big"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/ebitengine/purego"
	"golang.org/x/crypto/ssh"
	"sshclient/internal/connectcfg"
)

func TestMain(m *testing.M) {
	if MaybeWorker() {
		os.Exit(0)
	}
	os.Exit(m.Run())
}
func moduleFixture(t *testing.T) connectcfg.Hardware {
	t.Helper()
	compiler, e := exec.LookPath("cc")
	if e != nil {
		t.Skip("native ABI fixture requires a C compiler")
	}
	base, e := filepath.Abs("../../build/test-fixtures")
	if e != nil {
		t.Fatal(e)
	}
	if e = os.MkdirAll(base, 0700); e != nil {
		t.Fatal(e)
	}
	directory, e := os.MkdirTemp(base, "pkcs11-")
	if e != nil {
		t.Fatal(e)
	}
	t.Cleanup(func() { os.RemoveAll(directory) })
	output := filepath.Join(directory, "fixture.so")
	args := []string{"-shared", "-fPIC", "-o", output, "testdata/token.c"}
	if runtime.GOOS == "darwin" {
		output = filepath.Join(directory, "fixture.dylib")
		args = []string{"-dynamiclib", "-o", output, "testdata/token.c"}
	}
	if out, e := exec.Command(compiler, args...).CombinedOutput(); e != nil {
		t.Fatalf("native fixture compile: %v %s", e, out)
	}
	key, e := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if e != nil {
		t.Fatal(e)
	}
	template := &x509.Certificate{SerialNumber: big.NewInt(1), Subject: pkix.Name{CommonName: "synthetic fixture"}, NotBefore: time.Now().Add(-time.Hour), NotAfter: time.Now().Add(time.Hour), KeyUsage: x509.KeyUsageDigitalSignature}
	cert, e := x509.CreateCertificate(rand.Reader, template, template, &key.PublicKey, key)
	if e != nil {
		t.Fatal(e)
	}
	certPath := filepath.Join(directory, "public.der")
	if e = os.WriteFile(certPath, cert, 0600); e != nil {
		t.Fatal(e)
	}
	t.Setenv("SMALLSSH_TEST_TOKEN_CERT", certPath)
	public, e := ssh.NewPublicKey(&key.PublicKey)
	if e != nil {
		t.Fatal(e)
	}
	hash, e := ModuleHash(output)
	if e != nil {
		t.Fatal(e)
	}
	return connectcfg.Hardware{Module: output, ModuleSHA256: hash, TokenSerial: "fixture0001", KeyID: "01", Fingerprint: ssh.FingerprintSHA256(public), PIN: "testpin"}
}
func TestPKCS11NativeABIDiscoversSelectedPublicKeyAndNeverRetriesPIN(t *testing.T) {
	config := moduleFixture(t)
	module, e := openNative(config)
	if e != nil {
		t.Fatal(e)
	}
	defer module.Close()
	keys, e := module.keys(config, true)
	if e != nil || len(keys) != 1 || keys[0].Fingerprint != config.Fingerprint {
		t.Fatal(keys, e)
	}
	handle, e := purego.Dlopen(config.Module, purego.RTLD_NOW|purego.RTLD_LOCAL)
	if e != nil {
		t.Fatal(e)
	}
	defer purego.Dlclose(handle)
	var count func() word
	purego.RegisterLibFunc(&count, handle, "FixtureLoginCount")
	if count() != 1 {
		t.Fatal("PIN was retried during discovery", count())
	}
	wrong := config
	wrong.PIN = "incorrect"
	if _, e := module.keys(wrong, true); e == nil {
		t.Fatal("wrong PIN accepted")
	}
	if count() != 2 {
		t.Fatal("incorrect PIN was automatically retried", count())
	}
	unsigned := config
	unsigned.ModuleSHA256 = strings.Repeat("0", 64)
	if unexpected, e := openNative(unsigned); e == nil {
		unexpected.Close()
		t.Fatal("changed driver pin accepted")
	}
}
func TestPKCS11WorkerCancellationInterruptsStalledNativeSigning(t *testing.T) {
	config := moduleFixture(t)
	marker := filepath.Join(filepath.Dir(config.Module), "sign-started")
	t.Setenv("SMALLSSH_TEST_TOKEN_BLOCK", marker)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	signer, resource, e := Open(ctx, config)
	if e != nil {
		t.Fatal(e)
	}
	defer resource.Close()
	done := make(chan error, 1)
	go func() { _, e := signer.Sign(rand.Reader, []byte("synthetic authentication")); done <- e }()
	deadline := time.Now().Add(2 * time.Second)
	for {
		if _, e := os.Stat(marker); e == nil {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("native signing did not start")
		}
		time.Sleep(10 * time.Millisecond)
	}
	resource.Close()
	select {
	case e := <-done:
		if e == nil {
			t.Fatal("stalled native signer succeeded")
		}
	case <-time.After(time.Second):
		t.Fatal("closing worker failed to interrupt native provider")
	}
}
func TestPKCS11InspectionDoesNotSpreadOnePINAcrossTokens(t *testing.T) {
	config := moduleFixture(t)
	config.TokenSerial = ""
	module, e := openNative(config)
	if e != nil {
		t.Fatal(e)
	}
	defer module.Close()
	if _, e = module.keys(config, false); e == nil {
		t.Fatal("PIN without explicit token selection accepted")
	}
	config.PIN = ""
	if keys, e := module.keys(config, false); e != nil || len(keys) != 1 || keys[0].Serial != "fixture0001" {
		t.Fatal(keys, e)
	}
}

func TestPKCS11RejectsDuplicateTokenSerialBeforeLogin(t *testing.T) {
	config := moduleFixture(t)
	t.Setenv("SMALLSSH_TEST_DUPLICATE_SERIAL", "1")
	module, e := openNative(config)
	if e != nil {
		t.Fatal(e)
	}
	defer module.Close()
	if _, e := module.keys(config, false); e == nil {
		t.Fatal("duplicate token serial accepted")
	}
	handle, e := purego.Dlopen(config.Module, purego.RTLD_NOW|purego.RTLD_LOCAL)
	if e != nil {
		t.Fatal(e)
	}
	defer purego.Dlclose(handle)
	var count func() word
	purego.RegisterLibFunc(&count, handle, "FixtureLoginCount")
	if count() != 0 {
		t.Fatal("PIN was sent before resolving ambiguous token serial", count())
	}
}

func TestPKCS11SkipsTokenRemovedAfterSlotEnumeration(t *testing.T) {
	config := moduleFixture(t)
	t.Setenv("SMALLSSH_TEST_REMOVED_SLOT", "1")
	module, e := openNative(config)
	if e != nil {
		t.Fatal(e)
	}
	defer module.Close()
	keys, e := module.keys(config, true)
	if e != nil || len(keys) != 1 || keys[0].Fingerprint != config.Fingerprint {
		t.Fatal("remaining token was not discovered", keys, e)
	}
}

func TestPKCS11ReportsFunctionListError(t *testing.T) {
	config := moduleFixture(t)
	t.Setenv("SMALLSSH_TEST_FUNCTION_LIST_ERROR", "1")
	if module, e := openNative(config); e == nil {
		module.Close()
		t.Fatal("failed GetFunctionList accepted")
	} else if !strings.Contains(e.Error(), "GetFunctionList") || !strings.Contains(e.Error(), "0x5") {
		t.Fatal("provider error was hidden", e)
	}
}

func TestPKCS11DiscoversECPublicPointEncodings(t *testing.T) {
	config := moduleFixture(t)
	certificate, e := os.ReadFile(os.Getenv("SMALLSSH_TEST_TOKEN_CERT"))
	if e != nil {
		t.Fatal(e)
	}
	cert, e := x509.ParseCertificate(certificate)
	if e != nil {
		t.Fatal(e)
	}
	pub := cert.PublicKey.(*ecdsa.PublicKey)
	// The raw point's 0x04 tag and first coordinate byte 0x3f also form
	// a complete, misleading DER OCTET STRING of 63 bytes.
	for scalar := int64(1); ; scalar++ {
		x, y := pub.Curve.ScalarBaseMult(big.NewInt(scalar).Bytes())
		if candidate := elliptic.Marshal(pub.Curve, x, y); candidate[1] == 0x3f {
			pub = &ecdsa.PublicKey{Curve: pub.Curve, X: x, Y: y}
			break
		}
	}
	sshPublic, e := ssh.NewPublicKey(pub)
	if e != nil {
		t.Fatal(e)
	}
	config.Fingerprint = ssh.FingerprintSHA256(sshPublic)
	point := elliptic.Marshal(pub.Curve, pub.X, pub.Y)
	wrapped, e := asn1.Marshal(point)
	if e != nil {
		t.Fatal(e)
	}
	params, e := asn1.Marshal(asn1.ObjectIdentifier{1, 2, 840, 10045, 3, 1, 7})
	if e != nil {
		t.Fatal(e)
	}
	dir := filepath.Dir(config.Module)
	paramsPath, pointPath := filepath.Join(dir, "params.der"), filepath.Join(dir, "point.der")
	if e := os.WriteFile(paramsPath, params, 0600); e != nil {
		t.Fatal(e)
	}
	t.Setenv("SMALLSSH_TEST_PUBLIC_ONLY", "1")
	t.Setenv("SMALLSSH_TEST_EC_PARAMS", paramsPath)
	t.Setenv("SMALLSSH_TEST_EC_POINT", pointPath)
	for _, data := range [][]byte{wrapped, point} {
		if e := os.WriteFile(pointPath, data, 0600); e != nil {
			t.Fatal(e)
		}
		module, e := openNative(config)
		if e != nil {
			t.Fatal(e)
		}
		keys, e := module.keys(config, true)
		module.Close()
		if e != nil || len(keys) != 1 || keys[0].Fingerprint != config.Fingerprint {
			t.Fatal("EC public point could not be discovered", keys, e)
		}
	}
}

func TestPKCS11RetriesSlotListWhenTokenAppears(t *testing.T) {
	config := moduleFixture(t)
	t.Setenv("SMALLSSH_TEST_GROWING_SLOTS", "1")
	module, e := openNative(config)
	if e != nil {
		t.Fatal(e)
	}
	defer module.Close()
	keys, e := module.keys(config, true)
	if e != nil || len(keys) != 1 || keys[0].Fingerprint != config.Fingerprint {
		t.Fatal("token was not discovered after slot list grew", keys, e)
	}
}
