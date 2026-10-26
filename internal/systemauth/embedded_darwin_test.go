//go:build darwin

package systemauth

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func TestEmbeddedLibraryChecksPackagedHashBeforeLoading(t *testing.T) {
	dir := t.TempDir()
	macos := filepath.Join(dir, "SmallSSH.app", "Contents", "MacOS")
	resources := filepath.Join(dir, "SmallSSH.app", "Contents", "Resources")
	if err := os.MkdirAll(macos, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(resources, 0700); err != nil {
		t.Fatal(err)
	}
	executable := filepath.Join(macos, "SmallSSH")
	library := filepath.Join(resources, "libsmallssh-auth.dylib")
	fixture := []byte("isolated fixture, never a loadable native component")
	sum := sha256.Sum256(fixture)
	manifest, _ := json.Marshal(map[string]string{"darwin/" + runtime.GOARCH + "/embedded": hex.EncodeToString(sum[:])})
	if _, err := verifiedEmbeddedLibrary(executable, manifest); err == nil {
		t.Fatal("missing native library accepted")
	}
	if err := os.WriteFile(library, fixture, 0600); err != nil {
		t.Fatal(err)
	}
	if got, err := verifiedEmbeddedLibrary(executable, manifest); err != nil || filepath.Clean(got) != library {
		t.Fatal("verified Resources component not found", got, err)
	}
	for _, invalid := range [][]byte{nil, []byte(`{}`), []byte(`{"darwin/` + runtime.GOARCH + `/embedded":"abcd"}`)} {
		if _, err := verifiedEmbeddedLibrary(executable, invalid); err == nil {
			t.Fatal("invalid or missing embedded manifest entry accepted")
		}
	}
	if err := os.WriteFile(library, append(fixture, 1), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := verifiedEmbeddedLibrary(executable, manifest); err == nil {
		t.Fatal("tampered component accepted")
	}
}
