package desktop

import (
	"bytes"
	"context"
	"errors"
	"os"
	"runtime"
	"strings"
	"testing"
	"time"

	"sshclient/internal/client"
	"sshclient/internal/protocol"
	"sshclient/internal/service"
	"sshclient/internal/systemauth"
	"sshclient/internal/vault"
)

func TestPasswordUnlockArmsQuickUnlockThatOutlivesTheLock(t *testing.T) {
	base := ""
	if runtime.GOOS != "windows" {
		base = "/tmp"
	}
	dir, err := os.MkdirTemp(base, "sshd-quick-")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(dir)
	source, err := vault.New([]byte("synthetic quick unlock password"))
	if err != nil {
		t.Fatal(err)
	}
	defer source.Close()
	if err := vault.Save(vault.Path(dir), source, true); err != nil {
		t.Fatal(err)
	}
	host, err := service.Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer host.Close()
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	sdk := client.ServiceClient{Dir: dir, Executable: executable}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	envelope := []byte(`{"version":1,"vaultHash":"synthetic","data":"sealed"}`)
	sealedFrom := (*vault.Session)(nil)
	session, err := attachDesktopSession(ctx, sdk, source, func(_ context.Context, v *vault.Session) ([]byte, error) {
		sealedFrom = v
		return append([]byte(nil), envelope...), nil
	})
	if err != nil {
		t.Fatal(err)
	}
	defer session.Close()
	if sealedFrom != source {
		t.Fatal("quick unlock was not sealed from the vault the password opened")
	}
	updates := make(chan func(), 8)
	a := &app{ctx: ctx, dir: dir, online: true, server: session, dispatch: func(fn func()) { updates <- fn }}
	a.lock()
	select {
	case <-a.unlockCleanup:
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
	if host.Status().State != protocol.ServiceStateLocked {
		t.Fatal("lock left the core open")
	}
	got, err := sdk.QuickUnlockRecord(ctx)
	if err != nil || !bytes.Equal(got, envelope) {
		t.Fatal("the envelope did not outlive the lock", string(got), err)
	}
	clear(got)
}

type fakeQuickStore struct {
	record  []byte
	err     error
	cleared int
}

func (s *fakeQuickStore) Status(context.Context) (protocol.QuickUnlockState, error) {
	return protocol.QuickUnlockState{Available: s.err == nil && len(s.record) > 0}, nil
}
func (s *fakeQuickStore) Record(context.Context) ([]byte, error) {
	if s.err != nil {
		return nil, s.err
	}
	return append([]byte(nil), s.record...), nil
}
func (s *fakeQuickStore) Clear(context.Context) error { s.cleared++; s.record = nil; return nil }

func TestSwitchingQuickUnlockOffClearsTheServiceWithoutAPassword(t *testing.T) {
	store := &fakeQuickStore{record: []byte("opaque")}
	updates := make(chan func(), 8)
	dir := t.TempDir()
	if err := os.Chmod(dir, 0o700); err != nil { // preferences live in a private directory
		t.Fatal(err)
	}
	a := &app{ctx: context.Background(), dir: dir, auth: systemauth.New(0), quickUnlock: store,
		authStatus: systemauth.Status{Available: true, Enabled: true, Name: "Touch ID"}, dispatch: func(fn func()) { updates <- fn }}
	a.setQuickUnlock(false)
	// The work posts its cancellation cleanup and then its result.
	deadline := time.After(5 * time.Second)
	for a.quickUnlockPreferred() {
		select {
		case fn := <-updates:
			fn()
		case <-deadline:
			t.Fatal("switching off stalled", a.status)
		}
	}
	if store.cleared != 1 || a.quickUnlockPreferred() || a.quickUnlockSealer() != nil {
		t.Fatal("switching off left quick unlock armed", store.cleared, a.quickUnlockPreferred())
	}
}

func TestQuickUnlockSealsOnlyWhenWantedAndAvailable(t *testing.T) {
	on, off := true, false
	for _, c := range []struct {
		pref      *bool
		available bool
		want      bool
	}{{nil, true, true}, {&on, true, true}, {&off, true, false}, {nil, false, false}} {
		a := &app{auth: systemauth.New(0), authStatus: systemauth.Status{Available: c.available}}
		a.behavior.QuickUnlock = c.pref
		if (a.quickUnlockSealer() != nil) != c.want {
			t.Fatal("sealer", c.pref, c.available)
		}
	}
	if (&app{authStatus: systemauth.Status{Available: true}}).quickUnlockSealer() != nil {
		t.Fatal("sealed without a system verifier")
	}
}

func TestUnavailableQuickUnlockAsksForThePassword(t *testing.T) {
	store := &fakeQuickStore{err: client.ErrQuickUnlockUnavailable}
	if _, err := openQuickUnlock(context.Background(), systemauth.New(0), store, t.TempDir(), "解锁", nil); err == nil || !strings.Contains(err.Error(), "主密码") {
		t.Fatal("unavailable quick unlock did not ask for the password", err)
	}
	if _, err := openQuickUnlock(context.Background(), nil, store, t.TempDir(), "解锁", nil); err == nil {
		t.Fatal("opened without a system verifier")
	}
	store = &fakeQuickStore{err: errors.New("service unreachable")}
	if _, err := openQuickUnlock(context.Background(), systemauth.New(0), store, t.TempDir(), "解锁", nil); err == nil {
		t.Fatal("service failure opened the vault")
	}
}
