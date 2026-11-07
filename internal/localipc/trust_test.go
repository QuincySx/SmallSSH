package localipc

import (
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestOwnerTrustAnchorIsAtomicAndNeverSilentlyReplaced(t *testing.T) {
	dir := t.TempDir()
	if err := os.Chmod(dir, 0700); err != nil {
		t.Fatal(err)
	}
	pin := "sha256:" + strings.Repeat("1", 64)
	other := "sha256:" + strings.Repeat("2", 64)
	if _, err := ReadBrokerTrust(dir); !errors.Is(err, ErrBrokerPairing) {
		t.Fatal(err)
	}
	if err := PairBroker(dir, pin, false); err != nil {
		t.Fatal(err)
	}
	if err := PairBroker(dir, pin, false); err != nil {
		t.Fatal(err)
	}
	if err := PairBroker(dir, other, false); err == nil {
		t.Fatal("silently replaced existing owner trust")
	}
	if got, err := ReadBrokerTrust(dir); err != nil || got != pin {
		t.Fatal(got, err)
	}
	if err := PairBroker(dir, other, true); err != nil {
		t.Fatal(err)
	}
	if got, err := ReadBrokerTrust(dir); err != nil || got != other {
		t.Fatal(got, err)
	}
	if runtime.GOOS != "windows" {
		if err := os.Chmod(filepath.Join(dir, "broker-trust.json"), 0644); err != nil {
			t.Fatal(err)
		}
		if _, err := ReadBrokerTrust(dir); err == nil {
			t.Fatal("accepted mutable public trust configuration")
		}
	}
}

func TestBrokerTrustRejectsMalformedAndLinkedFiles(t *testing.T) {
	dir := t.TempDir()
	if err := os.Chmod(dir, 0700); err != nil {
		t.Fatal(err)
	}
	name := filepath.Join(dir, "broker-trust.json")
	for _, body := range []string{`{}`, `{"version":1,"brokerKey":"sha256:bad"}`, `{"version":2}`, strings.Repeat("x", 1025), `{"version":1,"brokerKey":"sha256:` + strings.Repeat("1", 64) + `"} {}`} {
		if err := os.WriteFile(name, []byte(body), 0600); err != nil {
			t.Fatal(err)
		}
		if _, err := ReadBrokerTrust(dir); err == nil {
			t.Fatal("accepted malformed trust anchor")
		}
	}
	if runtime.GOOS == "windows" {
		return
	}
	if err := os.Remove(name); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(dir, "elsewhere"), name); err != nil {
		t.Fatal(err)
	}
	if err := PairBroker(dir, "sha256:"+strings.Repeat("1", 64), true); err == nil {
		t.Fatal("replaced a linked trust record")
	}
}
