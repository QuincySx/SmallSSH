package systemauth

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"sshclient/internal/vault"
	"testing"
)

type fakeBackend struct {
	key   []byte
	deny  bool
	calls int
}

func (f *fakeBackend) Name() string                { return "test verifier" }
func (f *fakeBackend) Probe(context.Context) error { return nil }
func (f *fakeBackend) Wrap(_ context.Context, k []byte) (json.RawMessage, error) {
	f.key = append([]byte(nil), k...)
	return json.RawMessage(`{"opaque":true}`), nil
}
func (f *fakeBackend) Unwrap(ctx context.Context, _ json.RawMessage, _ string) ([]byte, error) {
	f.calls++
	if f.deny {
		return nil, errors.New("user cancelled")
	}
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}
	return append([]byte(nil), f.key...), nil
}
func unlockedTestVault(t *testing.T, dir string, pass []byte) *vault.Session {
	t.Helper()
	v, e := vault.New(pass)
	if e != nil {
		t.Fatal(e)
	}
	t.Cleanup(v.Close)
	if e = vault.Save(filepath.Join(dir, "vault.json"), v, true); e != nil {
		t.Fatal(e)
	}
	return v
}

func TestSystemGateCancelTamperAndFreshVerification(t *testing.T) {
	dir := t.TempDir()
	pass := []byte("independent test password")
	v := unlockedTestVault(t, dir, pass)
	b := &fakeBackend{}
	s := &Service{backend: b}
	ctx := context.Background()
	if _, e := s.Seal(ctx, nil); e == nil {
		t.Fatal("sealed without an unlocked vault")
	}
	sealed, e := s.Seal(ctx, v)
	if e != nil {
		t.Fatal(e)
	}
	for _, secret := range [][]byte{pass, b.key} {
		if contains(sealed, secret) {
			t.Fatal("plaintext key in the sealed record")
		}
	}
	if entries, _ := os.ReadDir(dir); len(entries) != 1 {
		t.Fatal("sealing wrote beside the vault", entries)
	}
	b.deny = true
	if _, e = s.OpenRecord(ctx, dir, sealed, "approve"); e == nil {
		t.Fatal("cancel bypass")
	}
	b.deny = false
	for i := 0; i < 2; i++ {
		opened, e := s.OpenRecord(ctx, dir, sealed, "approve")
		if e != nil {
			t.Fatal(e)
		}
		opened.Close()
	}
	if b.calls != 3 {
		t.Fatal("verification cached", b.calls)
	}
	b.key[0] ^= 1
	if _, e = s.OpenRecord(ctx, dir, sealed, "approve"); e == nil {
		t.Fatal("wrong device key accepted")
	}
	if s.Status(ctx, dir).Enabled {
		t.Fatal("status claims a record it does not hold")
	}
}
func contains(b, s []byte) bool {
	for i := 0; i+len(s) <= len(b); i++ {
		if string(b[i:i+len(s)]) == string(s) {
			return true
		}
	}
	return false
}

type cleaningBackend struct {
	fakeBackend
	count int
	fail  bool
}

func (b *cleaningBackend) Remove(_ context.Context, _ json.RawMessage) error {
	b.count++
	if b.fail {
		return errors.New("keyring unavailable")
	}
	return nil
}
func TestLegacyRecordRemovalAndBoundedRecords(t *testing.T) {
	dir := t.TempDir()
	pass := []byte("long cleanup test password")
	v := unlockedTestVault(t, dir, pass)
	b := &cleaningBackend{}
	s := &Service{backend: b}
	ctx := context.Background()
	if existed, e := s.RemoveLegacy(ctx, dir); existed || e != nil {
		t.Fatal("no legacy record", existed, e)
	}
	sealed, e := s.Seal(ctx, v)
	if e != nil {
		t.Fatal(e)
	}
	if e = os.WriteFile(legacyPath(dir), sealed, 0600); e != nil {
		t.Fatal(e)
	}
	existed, e := s.RemoveLegacy(ctx, dir)
	if !existed || e != nil || b.count != 1 {
		t.Fatal("legacy record or its keyring entry kept", existed, e, b.count)
	}
	if _, e = os.Stat(legacyPath(dir)); !os.IsNotExist(e) {
		t.Fatal("legacy file still on disk")
	}
	s.Discard(ctx, sealed)
	if b.count != 2 {
		t.Fatal("discard did not release the keyring entry")
	}
	for _, bad := range [][]byte{nil, make([]byte, 65537), []byte(`{"version":2}`), []byte(`not json`)} {
		if _, e := s.OpenRecord(ctx, dir, bad, "test"); e == nil {
			t.Fatal("invalid record accepted", len(bad))
		}
	}
}

type lateSuccessBackend struct {
	fakeBackend
	cancel context.CancelFunc
	onWrap bool
}

func (b *lateSuccessBackend) Unwrap(_ context.Context, _ json.RawMessage, _ string) ([]byte, error) {
	b.cancel()
	return append([]byte(nil), b.key...), nil
}
func (b *lateSuccessBackend) Wrap(ctx context.Context, key []byte) (json.RawMessage, error) {
	data, err := b.fakeBackend.Wrap(ctx, key)
	if b.onWrap {
		b.cancel()
	}
	return data, err
}
func TestLateSystemSuccessAfterCancellationCannotReleaseOrSeal(t *testing.T) {
	dir := t.TempDir()
	v := unlockedTestVault(t, dir, []byte("isolated cancellation master password"))
	b := &lateSuccessBackend{}
	service := &Service{backend: b}
	sealed, err := service.Seal(context.Background(), v)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	b.cancel = cancel
	if session, err := service.OpenRecord(ctx, dir, sealed, "test"); err == nil {
		session.Close()
		t.Fatal("late cancelled verification accepted")
	}
	ctx, cancel = context.WithCancel(context.Background())
	defer cancel()
	b.cancel = cancel
	b.onWrap = true
	if again, err := service.Seal(ctx, v); err == nil || again != nil {
		t.Fatal("cancelled sealing returned a record")
	}
}
