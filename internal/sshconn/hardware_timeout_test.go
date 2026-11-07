//go:build darwin || linux

package sshconn

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"errors"
	"math/big"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	"golang.org/x/crypto/ssh"
	"sshclient/internal/connectcfg"
	"sshclient/internal/hardware"
	"sshclient/internal/testserver"
)

func TestMain(m *testing.M) {
	if hardware.MaybeWorker() {
		os.Exit(0)
	}
	os.Exit(m.Run())
}
func TestSSHHandshakeDeadlineInterruptsANativeHardwareSigner(t *testing.T) {
	compiler, e := exec.LookPath("cc")
	if e != nil {
		t.Skip("native ABI timeout fixture requires a C compiler")
	}
	root, e := filepath.Abs("../../build/test-fixtures")
	if e != nil {
		t.Fatal(e)
	}
	if e = os.MkdirAll(root, 0700); e != nil {
		t.Fatal(e)
	}
	directory, e := os.MkdirTemp(root, "ssh-hardware-")
	if e != nil {
		t.Fatal(e)
	}
	t.Cleanup(func() { os.RemoveAll(directory) })
	source, e := filepath.Abs("../hardware/testdata/token.c")
	if e != nil {
		t.Fatal(e)
	}
	library := filepath.Join(directory, "fixture.so")
	arguments := []string{"-shared", "-fPIC", "-o", library, source}
	if runtime.GOOS == "darwin" {
		library = filepath.Join(directory, "fixture.dylib")
		arguments = []string{"-dynamiclib", "-o", library, source}
	}
	if output, e := exec.Command(compiler, arguments...).CombinedOutput(); e != nil {
		t.Fatalf("native fixture build: %v %s", e, output)
	}
	key, e := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if e != nil {
		t.Fatal(e)
	}
	certificate := &x509.Certificate{SerialNumber: big.NewInt(1), Subject: pkix.Name{CommonName: "synthetic signing timeout fixture"}, NotBefore: time.Now().Add(-time.Hour), NotAfter: time.Now().Add(time.Hour), KeyUsage: x509.KeyUsageDigitalSignature}
	der, e := x509.CreateCertificate(rand.Reader, certificate, certificate, &key.PublicKey, key)
	if e != nil {
		t.Fatal(e)
	}
	file := filepath.Join(directory, "public.der")
	if e = os.WriteFile(file, der, 0600); e != nil {
		t.Fatal(e)
	}
	marker := filepath.Join(directory, "sign-started")
	t.Setenv("SMALLSSH_TEST_TOKEN_CERT", file)
	t.Setenv("SMALLSSH_TEST_TOKEN_BLOCK", marker)
	public, e := ssh.NewPublicKey(&key.PublicKey)
	if e != nil {
		t.Fatal(e)
	}
	fixture := testserver.NewWithAcceptedPublicKey(t, public)
	profile := fixture.Profile()
	profile.Auth = "pkcs11"
	hash, e := hardware.ModuleHash(library)
	if e != nil {
		t.Fatal(e)
	}
	profile.Hardware = connectcfg.Hardware{Module: library, ModuleSHA256: hash, TokenSerial: "fixture0001", KeyID: "01", Fingerprint: ssh.FingerprintSHA256(public), PIN: "testpin"}
	parent, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	started := time.Now()
	connection, e := Dial(parent, profile)
	if e == nil {
		connection.Close()
		t.Fatal("stalled hardware signer authenticated")
	}
	if _, markerError := os.Stat(marker); markerError != nil {
		t.Fatal("hardware signing was not reached", e, markerError)
	}
	if !errors.Is(e, context.DeadlineExceeded) || parent.Err() != nil || time.Since(started) > 20*time.Second {
		t.Fatal("handshake deadline failed to interrupt native signing independently of its live operation", e, time.Since(started))
	}
}
