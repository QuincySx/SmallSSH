package vault

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

const (
	readContractPassword = "synthetic read-contract password"
	readContractProof    = "read-contract identity proof"
	readContractPermErr  = "保险库文件权限必须仅对当前用户开放"
)

func newReadContractFixture(t *testing.T) (string, []byte, []byte, *Session) {
	t.Helper()

	pass := []byte(readContractPassword)
	owner, err := New(pass)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	t.Cleanup(func() {
		owner.Close()
	})

	data, err := owner.Seal()
	if err != nil {
		t.Fatalf("Seal: %v", err)
	}
	path := filepath.Join(t.TempDir(), "vault.json")
	if err := os.WriteFile(path, data, 0600); err != nil {
		t.Fatalf("write fixture: %v", err)
	}
	key, err := owner.UnlockKey()
	if err != nil {
		t.Fatalf("UnlockKey: %v", err)
	}
	return path, pass, key, owner
}

func assertReadKeyMatches(t *testing.T, got, want *Session) {
	t.Helper()
	if got == nil {
		t.Fatal("ReadKey returned a nil session")
	}
	defer got.Close()

	wantProof, err := want.Proof([]byte(readContractProof))
	if err != nil {
		t.Fatalf("original Proof: %v", err)
	}
	defer wipe(wantProof)
	gotProof, err := got.Proof([]byte(readContractProof))
	if err != nil {
		t.Fatalf("ReadKey Proof: %v", err)
	}
	defer wipe(gotProof)
	if !bytes.Equal(gotProof, wantProof) {
		t.Fatal("ReadKey proof does not match the original session")
	}
	if got.header.Version != want.header.Version || !bytes.Equal(got.header.Salt, want.header.Salt) || !bytes.Equal(got.header.ID, want.header.ID) {
		t.Fatal("ReadKey identity pin does not match the original session")
	}
	wantPin, err := want.BrokerPublicKeyPin()
	if err != nil {
		t.Fatalf("original BrokerPublicKeyPin: %v", err)
	}
	gotPin, err := got.BrokerPublicKeyPin()
	if err != nil {
		t.Fatalf("ReadKey BrokerPublicKeyPin: %v", err)
	}
	if gotPin != wantPin {
		t.Fatal("ReadKey broker public key pin does not match the original session")
	}
}

func assertReadErrUnlock(t *testing.T, got *Session, err error, operation string) {
	t.Helper()
	if got != nil {
		got.Close()
		t.Fatalf("%s returned a session with an error", operation)
	}
	if !errors.Is(err, ErrUnlock) {
		t.Fatalf("%s error = %v, want ErrUnlock", operation, err)
	}
}

func TestReadKeyValidAndPermissionContract(t *testing.T) {
	path, pass, key, owner := newReadContractFixture(t)
	defer wipe(key)

	got, err := ReadKey(path, key)
	if err != nil {
		t.Fatalf("ReadKey valid file: %v", err)
	}
	assertReadKeyMatches(t, got, owner)

	if runtime.GOOS == "windows" {
		return
	}
	if err := os.Chmod(path, 0644); err != nil {
		t.Fatalf("chmod fixture 0644: %v", err)
	}

	session, err := Read(path, pass)
	if session != nil {
		session.Close()
		t.Fatal("Read returned a session for a public vault file")
	}
	if err == nil || err.Error() != readContractPermErr {
		t.Fatalf("Read public file error = %v, want %q", err, readContractPermErr)
	}

	session, err = ReadKey(path, key)
	assertReadErrUnlock(t, session, err, "ReadKey public file")

	if err := os.Chmod(path, 0600); err != nil {
		t.Fatalf("restore fixture 0600: %v", err)
	}
	got, err = ReadKey(path, key)
	if err != nil {
		t.Fatalf("ReadKey after restoring 0600: %v", err)
	}
	assertReadKeyMatches(t, got, owner)
}

func TestReadDirectoryContract(t *testing.T) {
	_, pass, key, _ := newReadContractFixture(t)
	defer wipe(key)

	dir := t.TempDir()
	var probeInfo os.FileInfo
	probe, probeErr := os.Open(dir)
	if probeErr == nil {
		probeInfo, probeErr = probe.Stat()
		if err := probe.Close(); err != nil {
			t.Fatalf("close directory probe: %v", err)
		}
	}
	if probeErr == nil && probeInfo.Mode().IsRegular() {
		t.Fatal("t.TempDir probe unexpectedly describes a regular file")
	}

	session, err := Read(dir, pass)
	assertReadDirectoryError(t, session, err, probeErr)

	session, err = ReadKey(dir, key)
	assertReadDirectoryError(t, session, err, probeErr)
}

func assertReadDirectoryError(t *testing.T, got *Session, err, probeErr error) {
	t.Helper()
	if got != nil {
		got.Close()
		t.Fatal("directory read returned a non-nil session")
	}
	if probeErr == nil {
		if !errors.Is(err, ErrUnlock) {
			t.Fatalf("directory read error = %v, want ErrUnlock", err)
		}
		return
	}

	probePathErr, ok := probeErr.(*os.PathError)
	if !ok {
		t.Fatalf("directory probe error = %T, want *os.PathError", probeErr)
	}
	gotPathErr, ok := err.(*os.PathError)
	if !ok {
		t.Fatalf("directory read error = %T (%v), want *os.PathError", err, err)
	}
	if gotPathErr.Op != probePathErr.Op || gotPathErr.Path != probePathErr.Path || !errors.Is(gotPathErr.Err, probePathErr.Err) {
		t.Fatalf("directory read error = %v, want path error matching %v", gotPathErr, probePathErr)
	}
	if errors.Is(err, ErrUnlock) {
		t.Fatal("directory path error was replaced with ErrUnlock")
	}
}

func TestReadOversizedFileContract(t *testing.T) {
	_, pass, key, owner := newReadContractFixture(t)
	defer wipe(key)

	valid, err := owner.Seal()
	if err != nil {
		t.Fatalf("Seal valid oversized fixture: %v", err)
	}
	path := filepath.Join(t.TempDir(), "oversized.json")
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY, 0600)
	if err != nil {
		t.Fatalf("create oversized fixture: %v", err)
	}
	if _, err := f.Write(valid); err != nil {
		if closeErr := f.Close(); closeErr != nil {
			t.Errorf("close oversized fixture after prefix write failure: %v", closeErr)
		}
		t.Fatalf("write valid oversized fixture prefix: %v", err)
	}
	if err := f.Close(); err != nil {
		t.Fatalf("close valid oversized fixture prefix: %v", err)
	}

	session, err := ReadKey(path, key)
	if err != nil {
		t.Fatalf("ReadKey valid oversized fixture prefix: %v", err)
	}
	assertReadKeyMatches(t, session, owner)

	f, err = os.OpenFile(path, os.O_WRONLY|os.O_APPEND, 0600)
	if err != nil {
		t.Fatalf("reopen oversized fixture for padding: %v", err)
	}
	padding := bytes.Repeat([]byte(" "), 32*1024)
	for remaining := int64(maxFileSize) + 1 - int64(len(valid)); remaining > 0; {
		chunk := int64(len(padding))
		if remaining < chunk {
			chunk = remaining
		}
		if _, err := f.Write(padding[:int(chunk)]); err != nil {
			if closeErr := f.Close(); closeErr != nil {
				t.Errorf("close oversized fixture after padding write failure: %v", closeErr)
			}
			t.Fatalf("append oversized fixture padding: %v", err)
		}
		remaining -= chunk
	}
	if err := f.Close(); err != nil {
		t.Fatalf("close oversized fixture: %v", err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat oversized fixture: %v", err)
	}
	if info.Size() != int64(maxFileSize)+1 {
		t.Fatalf("oversized fixture size = %d, want %d", info.Size(), int64(maxFileSize)+1)
	}

	session, err = Read(path, pass)
	assertReadErrUnlock(t, session, err, "Read oversized file")

	session, err = ReadKey(path, key)
	assertReadErrUnlock(t, session, err, "ReadKey oversized file")
}

func TestReadMissingPathContract(t *testing.T) {
	_, pass, key, _ := newReadContractFixture(t)
	defer wipe(key)

	path := filepath.Join(t.TempDir(), "missing.json")
	session, err := Read(path, pass)
	assertMissingPathError(t, session, err, path, "Read")

	session, err = ReadKey(path, key)
	assertMissingPathError(t, session, err, path, "ReadKey")
}

func assertMissingPathError(t *testing.T, got *Session, err error, path, operation string) {
	t.Helper()
	if got != nil {
		got.Close()
		t.Fatalf("%s returned a session for a missing path", operation)
	}
	pathErr, ok := err.(*os.PathError)
	if !ok {
		t.Fatalf("%s missing path error = %T (%v), want *os.PathError", operation, err, err)
	}
	if pathErr.Path != path {
		t.Fatalf("%s missing path error path = %q, want %q", operation, pathErr.Path, path)
	}
	if !os.IsNotExist(err) {
		t.Fatalf("%s missing path error = %v, want os.IsNotExist", operation, err)
	}
	if errors.Is(err, ErrUnlock) {
		t.Fatalf("%s missing path error was replaced with ErrUnlock", operation)
	}
}
