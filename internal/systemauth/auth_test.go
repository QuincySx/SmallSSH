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
func TestSystemGateCancelTamperAndFreshVerification(t *testing.T) {
	dir := t.TempDir()
	pass := []byte("independent test password")
	v, e := vault.New(pass)
	if e != nil {
		t.Fatal(e)
	}
	defer v.Close()
	if e = vault.Save(filepath.Join(dir, "vault.json"), v, true); e != nil {
		t.Fatal(e)
	}
	b := &fakeBackend{}
	s := &Service{backend: b}
	ctx := context.Background()
	if e = s.Enroll(ctx, dir, []byte("wrong")); e == nil {
		t.Fatal("enrollment without master proof")
	}
	if e = s.Enroll(ctx, dir, pass); e != nil {
		t.Fatal(e)
	}
	raw, _ := os.ReadFile(path(dir))
	for _, secret := range [][]byte{pass, b.key} {
		if contains(raw, secret) {
			t.Fatal("plaintext key saved")
		}
	}
	b.deny = true
	if _, e = s.Open(ctx, dir, "approve"); e == nil {
		t.Fatal("cancel bypass")
	}
	b.deny = false
	for i := 0; i < 2; i++ {
		v, e = s.Open(ctx, dir, "approve")
		if e != nil {
			t.Fatal(e)
		}
		v.Close()
	}
	if b.calls != 3 {
		t.Fatal("verification cached", b.calls)
	}
	b.key[0] ^= 1
	if _, e = s.Open(ctx, dir, "approve"); e == nil {
		t.Fatal("wrong device key accepted")
	}
	if e = s.Disable(dir, []byte("wrong")); e == nil {
		t.Fatal("removed without master")
	}
	if e = s.Disable(dir, pass); e != nil {
		t.Fatal(e)
	}
	if s.Status(ctx, dir).Enabled {
		t.Fatal("still enabled")
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
func TestDeviceRecordCleanupAndBoundedReading(t *testing.T) {
	dir := t.TempDir()
	pass := []byte("long cleanup test password")
	v, _ := vault.New(pass)
	vault.Save(filepath.Join(dir, "vault.json"), v, true)
	v.Close()
	b := &cleaningBackend{}
	s := &Service{backend: b}
	ctx := context.Background()
	if e := s.Enroll(ctx, dir, pass); e != nil {
		t.Fatal(e)
	}
	if e := s.Enroll(ctx, dir, pass); e != nil {
		t.Fatal(e)
	}
	if b.count != 1 {
		t.Fatal("old keyring item retained")
	}
	b.fail = true
	if e := s.Disable(dir, pass); e == nil {
		t.Fatal("cleanup failure hidden")
	}
	if !s.Status(ctx, dir).Enabled {
		t.Fatal("record removed before cleanup")
	}
	b.fail = false
	if e := s.Disable(dir, pass); e != nil {
		t.Fatal(e)
	}
	if e := os.WriteFile(path(dir), make([]byte, 65537), 0600); e != nil {
		t.Fatal(e)
	}
	if _, e := s.Open(ctx, dir, "test"); e == nil {
		t.Fatal("oversized record accepted")
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
func TestLateSystemSuccessAfterCancellationCannotReleaseOrEnroll(t *testing.T) {
	dir := t.TempDir()
	pass := []byte("isolated cancellation master password")
	v, err := vault.New(pass)
	if err != nil {
		t.Fatal(err)
	}
	if err = vault.Save(filepath.Join(dir, "vault.json"), v, true); err != nil {
		t.Fatal(err)
	}
	v.Close()
	b := &lateSuccessBackend{}
	service := &Service{backend: b}
	if err = service.Enroll(context.Background(), dir, pass); err != nil {
		t.Fatal(err)
	}
	original, err := os.ReadFile(path(dir))
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	b.cancel = cancel
	if session, err := service.Open(ctx, dir, "test"); err == nil {
		session.Close()
		t.Fatal("late cancelled verification accepted")
	}
	ctx, cancel = context.WithCancel(context.Background())
	defer cancel()
	b.cancel = cancel
	b.onWrap = true
	if err = service.Enroll(ctx, dir, pass); err == nil {
		t.Fatal("cancelled enrollment accepted")
	}
	after, err := os.ReadFile(path(dir))
	if err != nil || string(after) != string(original) {
		t.Fatal("cancelled enrollment changed device record", err)
	}
}
