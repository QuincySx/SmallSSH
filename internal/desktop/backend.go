package desktop

import (
	"context"
	"errors"
	"sshclient/internal/client"
	"sshclient/internal/vault"
	"sync"
	"sync/atomic"
	"time"
)

type desktopSession struct {
	owner   client.OwnerProof
	done    <-chan struct{}
	pin     string
	seal    func() error
	deposit func(context.Context, []byte) error // quick-unlock envelope; nil when unavailable
	close   func()
	abandon func()
	closed  atomic.Bool
	once    sync.Once
	err     error
}

func (s *desktopSession) Proof(p []byte) ([]byte, error) {
	if s == nil || s.closed.Load() {
		return nil, errors.New("保险库已锁定")
	}
	return s.owner.Proof(p)
}
func (s *desktopSession) PublicKeyPin() string { return s.pin }
func (s *desktopSession) BrokerPublicKeyPin() (string, error) {
	if s == nil || s.closed.Load() {
		return "", errors.New("保险库已锁定")
	}
	return s.pin, nil
}
func (s *desktopSession) revoke() { s.closed.Store(true) }
func (s *desktopSession) Lock() error {
	s.revoke()
	s.once.Do(func() {
		if s.seal != nil {
			s.err = s.seal()
		}
		if s.close != nil {
			s.close()
		}
	})
	return s.err
}
func (s *desktopSession) Close() { _ = s.Lock() }
func (s *desktopSession) Abandon() {
	s.revoke()
	s.once.Do(func() {
		if s.abandon != nil {
			s.abandon()
		} else if s.close != nil {
			s.close()
		}
	})
}
func openDesktopSession(ctx context.Context, dir string, source *vault.Session, seal func(context.Context, *vault.Session) ([]byte, error)) (*desktopSession, error) {
	defer source.Close()
	local, err := client.NewLocalService(dir)
	if err != nil {
		return nil, err
	}
	if err = local.Ensure(ctx); err != nil {
		return nil, err
	}
	return attachDesktopSession(ctx, local.Client(), source, seal)
}

// attachDesktopSession commits an owner session from a freshly opened vault.
// With seal, it also arms quick unlock while the vault is still open; a failure
// there only leaves quick unlock unarmed.
func attachDesktopSession(ctx context.Context, sdk client.ServiceClient, source *vault.Session, seal func(context.Context, *vault.Session) ([]byte, error)) (*desktopSession, error) {
	defer source.Close()
	prepared, err := sdk.PrepareOwner(ctx, source, 8*time.Hour)
	if err != nil {
		return nil, err
	}
	defer prepared.Close()
	owner, err := prepared.Commit(ctx)
	if err != nil {
		return nil, err
	}
	pin, err := owner.BrokerPublicKeyPin()
	if err != nil {
		owner.Close()
		return nil, err
	}
	deposit := func(ctx context.Context, sealed []byte) error {
		_, err := owner.DepositQuickUnlock(ctx, sealed)
		return err
	}
	if seal != nil {
		if sealed, err := seal(ctx, source); err == nil {
			_ = deposit(ctx, sealed)
			clear(sealed)
		}
	}
	return &desktopSession{owner: owner, pin: pin, done: owner.Done(), deposit: deposit, seal: func() error {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		_, err := owner.Lock(ctx)
		return err
	}, close: owner.Close, abandon: owner.Close}, nil
}
