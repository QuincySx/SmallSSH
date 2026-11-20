package desktop

import (
	"context"
	"errors"
	"time"

	"sshclient/internal/client"
	"sshclient/internal/protocol"
	"sshclient/internal/systemauth"
	"sshclient/internal/vault"
)

// Quick unlock: after a master-password unlock the desktop seals the vault key
// with this machine's owner-verification key and hands the envelope to the
// local service, which keeps it in memory only and returns it to this exact
// executable. After a lock (idle, screen lock, sleep or a manual lock) Touch ID
// or the platform equivalent opens it again. A restart, an update, 48 hours or
// switching it off require the master password. Nothing is kept on disk, so no
// other program can ask the OS to unwrap it behind a prompt of its own wording.

// quickUnlockStore is the local service's side of quick unlock.
type quickUnlockStore interface {
	Status(context.Context) (protocol.QuickUnlockState, error)
	Record(context.Context) ([]byte, error)
	Clear(context.Context) error
}

type serviceQuickUnlockStore struct{ dir string }

func (s serviceQuickUnlockStore) sdk() (client.ServiceClient, error) {
	local, err := client.NewLocalService(s.dir)
	if err != nil {
		return client.ServiceClient{}, err
	}
	return local.Client(), nil
}

func (s serviceQuickUnlockStore) Status(ctx context.Context) (protocol.QuickUnlockState, error) {
	sdk, err := s.sdk()
	if err != nil {
		return protocol.QuickUnlockState{}, err
	}
	return sdk.QuickUnlockStatus(ctx)
}

func (s serviceQuickUnlockStore) Record(ctx context.Context) ([]byte, error) {
	sdk, err := s.sdk()
	if err != nil {
		return nil, err
	}
	return sdk.QuickUnlockRecord(ctx)
}

func (s serviceQuickUnlockStore) Clear(ctx context.Context) error {
	sdk, err := s.sdk()
	if err != nil {
		return err
	}
	return sdk.ClearQuickUnlock(ctx)
}

func (a *app) quickStore() quickUnlockStore {
	if a.quickUnlock != nil {
		return a.quickUnlock
	}
	return serviceQuickUnlockStore{dir: a.dir}
}

// quickUnlockPreferred is the setting; it is on unless switched off.
func (a *app) quickUnlockPreferred() bool { return behaviorEnabled(a.behavior.QuickUnlock) }

// quickUnlockSealer returns how to seal a freshly password-opened vault, or nil
// when quick unlock is off or this machine cannot verify the owner.
func (a *app) quickUnlockSealer() func(context.Context, *vault.Session) ([]byte, error) {
	if !a.quickUnlockPreferred() || a.auth == nil || !a.authStatus.Available {
		return nil
	}
	auth, dir := a.auth, a.dir
	return func(ctx context.Context, v *vault.Session) ([]byte, error) {
		return auth.SealForDirectory(ctx, dir, v)
	}
}

// openQuickUnlock fetches the envelope this executable deposited and opens the
// vault with a fresh system verification whose prompt says reason.
func openQuickUnlock(ctx context.Context, auth *systemauth.Service, store quickUnlockStore, dir, reason string, presentation *systemauth.EmbeddedPresentation) (*vault.Session, error) {
	if auth == nil {
		return nil, errors.New(tr(nil, "系统验证不可用"))
	}
	sealed, err := store.Record(ctx)
	if err != nil {
		if errors.Is(err, client.ErrQuickUnlockUnavailable) {
			return nil, errors.New(tr(nil, "快速解锁不可用，请输入主密码"))
		}
		return nil, err
	}
	defer clear(sealed)
	if presentation != nil {
		return auth.OpenRecordEmbedded(ctx, dir, sealed, reason, presentation)
	}
	return auth.OpenRecord(ctx, dir, sealed, reason)
}

// migrateLegacyDeviceUnlock deletes the on-disk record older releases kept.
// The preference keeps its default (on), so the next password unlock arms the
// in-memory replacement.
func (a *app) migrateLegacyDeviceUnlock() {
	if a.auth == nil || a.legacyAuthChecked {
		return
	}
	a.legacyAuthChecked = true
	auth, dir := a.auth, a.dir
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		_, _ = auth.RemoveLegacy(ctx, dir)
	}()
}

// setQuickUnlock switches quick unlock. Turning it on needs the master password
// and arms it at once through the current owner session; turning it off only
// drops authority and needs nothing.
func (a *app) setQuickUnlock(enable bool) {
	if a.auth == nil || a.busy {
		return
	}
	next := a.behavior
	value := enable
	next.QuickUnlock = &value
	auth, dir, store, server := a.auth, a.dir, a.quickStore(), a.server
	pass := []byte(a.ownerPass)
	a.ownerPass = ""
	ctx, finish := a.authContext()
	a.work(func() (func(), error) {
		defer wipe(pass)
		defer finish()
		if !enable {
			if sealed, err := store.Record(ctx); err == nil {
				auth.Discard(ctx, sealed)
				clear(sealed)
			}
			if err := store.Clear(ctx); err != nil && !errors.Is(err, client.ErrQuickUnlockUnavailable) {
				return nil, err
			}
			return func() {
				if err := a.applyBehaviorPreferences(next); err != nil {
					a.status = err.Error()
					return
				}
				a.refreshAuth()
				a.status = tr(nil, "快速解锁已关闭；之后解锁需要主密码")
			}, nil
		}
		v, err := vault.Read(vault.Path(dir), pass)
		if err != nil {
			return nil, err
		}
		defer v.Close()
		if server == nil || server.deposit == nil {
			return nil, errors.New(tr(nil, "请先解锁保险库"))
		}
		sealed, err := auth.SealForDirectory(ctx, dir, v)
		if err != nil {
			return nil, err
		}
		defer clear(sealed)
		if err := server.deposit(ctx, sealed); err != nil {
			auth.Discard(ctx, sealed)
			return nil, err
		}
		return func() {
			if err := a.applyBehaviorPreferences(next); err != nil {
				a.status = err.Error()
				return
			}
			a.refreshAuth()
			a.status = tr(nil, "快速解锁已开启")
		}, nil
	})
}
