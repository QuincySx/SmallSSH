package systemauth

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"sshclient/internal/vault"
)

func TestKeyringOrphanCleanupIsScopedAndRetryable(t *testing.T) {
	first, other := t.TempDir(), t.TempDir()
	removed, retry, live := vault.ShortID(), vault.ShortID(), vault.ShortID()
	for _, value := range []struct{ dir, id string }{{first, removed}, {first, retry}, {other, live}} {
		if err := registerKeyringID(value.dir, value.id); err != nil {
			t.Fatal(err)
		}
		path := filepath.Join(value.dir, keyringCleanupDirectory, value.id)
		info, err := os.Stat(path)
		if err != nil || !info.Mode().IsRegular() || info.Size() != 0 {
			t.Fatal("cleanup metadata contains something beyond an empty identifier file", info, err)
		}
		if runtime.GOOS != "windows" && info.Mode().Perm() != 0600 {
			t.Fatal("cleanup identifier is not private")
		}
	}
	unavailable := errors.New("keyring locked")
	visited := make(map[string]bool)
	err := cleanupKeyringIDs(context.Background(), first, func(_ context.Context, id string) error {
		visited[id] = true
		if id == retry {
			return unavailable
		}
		return nil
	})
	if !errors.Is(err, unavailable) || !visited[removed] || !visited[retry] || visited[live] {
		t.Fatal("cleanup crossed directories or hid a failed removal", visited, err)
	}
	if _, err := os.Stat(filepath.Join(first, keyringCleanupDirectory, removed)); !os.IsNotExist(err) {
		t.Fatal("successfully cleaned identifier remains", err)
	}
	if _, err := os.Stat(filepath.Join(first, keyringCleanupDirectory, retry)); err != nil {
		t.Fatal("failed removal lost its retry identifier", err)
	}
	if err := cleanupKeyringIDs(context.Background(), first, func(_ context.Context, id string) error {
		if id != retry {
			t.Fatal("retry removed an unrelated entry", id)
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(other, keyringCleanupDirectory, live)); err != nil {
		t.Fatal("cleanup removed another vault's live identifier", err)
	}
	if err := cleanupKeyringIDs(context.Background(), first, func(context.Context, string) error {
		t.Fatal("empty cleanup ran a keyring operation")
		return nil
	}); err != nil {
		t.Fatal(err)
	}
}

func TestKeyringCleanupRejectsUnexpectedFilesWithoutDeletingThem(t *testing.T) {
	dir := t.TempDir()
	id := vault.ShortID()
	if err := registerKeyringID(dir, id); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, keyringCleanupDirectory, id)
	if err := os.WriteFile(path, []byte("keep this data"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := unregisterKeyringID(dir, id); err == nil {
		t.Fatal("discard removed a non-marker file")
	}
	if err := cleanupKeyringIDs(context.Background(), dir, func(context.Context, string) error {
		t.Fatal("unexpected metadata reached the keyring")
		return nil
	}); err == nil {
		t.Fatal("accepted nonempty metadata")
	}
	data, err := os.ReadFile(path)
	if err != nil || string(data) != "keep this data" {
		t.Fatal("cleanup deleted unrelated data", err)
	}
	for _, bad := range []string{"", "../vault.json", "/absolute/path", "not-an-identifier"} {
		if err := registerKeyringID(dir, bad); err == nil {
			t.Fatal("accepted invalid identifier", bad)
		}
	}
}

func TestKeyringCleanupDoesNotFollowMarkerOrDirectorySymlinks(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlink creation requires a separate Windows permission")
	}
	dir := t.TempDir()
	id := vault.ShortID()
	if err := registerKeyringID(dir, id); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, keyringCleanupDirectory, id)
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(t.TempDir(), "untouched")
	if err := os.WriteFile(target, []byte("safe"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(target, path); err != nil {
		t.Fatal(err)
	}
	if err := unregisterKeyringID(dir, id); err == nil {
		t.Fatal("discard accepted a symlink marker")
	}
	if err := cleanupKeyringIDs(context.Background(), dir, func(context.Context, string) error {
		t.Fatal("symlink marker reached the keyring")
		return nil
	}); err == nil {
		t.Fatal("cleanup accepted a symlink marker")
	}
	data, err := os.ReadFile(target)
	if err != nil || string(data) != "safe" {
		t.Fatal("cleanup changed a symlink target", err)
	}
	other := t.TempDir()
	if err := os.Symlink(filepath.Dir(path), filepath.Join(other, keyringCleanupDirectory)); err != nil {
		t.Fatal(err)
	}
	if err := cleanupKeyringIDs(context.Background(), other, func(context.Context, string) error { return nil }); err == nil {
		t.Fatal("cleanup accepted a symlink directory")
	}
}

func TestCanceledKeyringCleanupKeepsIdentifiersForRetry(t *testing.T) {
	dir, id := t.TempDir(), vault.ShortID()
	if err := registerKeyringID(dir, id); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := cleanupKeyringIDs(ctx, dir, func(context.Context, string) error {
		t.Fatal("canceled cleanup touched the keyring")
		return nil
	}); !errors.Is(err, context.Canceled) {
		t.Fatal("cleanup ignored cancellation", err)
	}
	if err := unregisterKeyringID(dir, id); err != nil {
		t.Fatal("canceled cleanup lost the identifier", err)
	}
}

type scopedCleaningBackend struct {
	cleaningBackend
	directory string
	cancel    context.CancelFunc
}

func (b *scopedCleaningBackend) WrapForDirectory(ctx context.Context, dir string, key []byte) (json.RawMessage, error) {
	b.directory = dir
	data, err := b.fakeBackend.Wrap(ctx, key)
	if b.cancel != nil {
		b.cancel()
	}
	return data, err
}

func TestDirectorySealingSelectsScopedBackendAndDiscardsLateSuccess(t *testing.T) {
	dir := t.TempDir()
	v := unlockedTestVault(t, dir, []byte("isolated scoped seal password"))
	backend := &scopedCleaningBackend{}
	service := &Service{backend: backend}
	if _, err := service.SealForDirectory(context.Background(), dir, v); err != nil || backend.directory != dir {
		t.Fatal("scoped sealing lost the owning data directory", backend.directory, err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	backend.cancel = cancel
	if data, err := service.SealForDirectory(ctx, dir, v); err == nil || data != nil || backend.count != 1 {
		t.Fatal("late canceled scoped seal kept its external key", err, backend.count)
	}
}

// Done is observed only when the lock helper reaches its wait-for-lock select.
// This lets the regression order its participants without timing assumptions.
type keyringWaitingContext struct {
	context.Context
	waiting chan struct{}
	once    sync.Once
}

func (c *keyringWaitingContext) Done() <-chan struct{} {
	c.once.Do(func() { close(c.waiting) })
	return c.Context.Done()
}

func TestOrphanCleanupWaitsForRegisteredKeyToFinishStoring(t *testing.T) {
	for _, canceled := range []bool{false, true} {
		name := "cleanup after store"
		if canceled {
			name = "canceled cleanup retains active marker"
		}
		t.Run(name, func(t *testing.T) {
			dir, id := t.TempDir(), vault.ShortID()
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			registered := make(chan struct{})
			finishStore := make(chan struct{})
			var releaseStore sync.Once
			defer releaseStore.Do(func() { close(finishStore) })
			var stored, removed atomic.Bool
			wrapDone := make(chan error, 1)
			go func() {
				wrapDone <- withKeyringLock(ctx, dir, func() error {
					if err := registerKeyringID(dir, id); err != nil {
						return err
					}
					close(registered)
					select {
					case <-finishStore:
						stored.Store(true)
						return nil
					case <-ctx.Done():
						return ctx.Err()
					}
				})
			}()
			select {
			case <-registered:
			case err := <-wrapDone:
				t.Fatal("registration failed", err)
			case <-ctx.Done():
				t.Fatal(ctx.Err())
			}
			cleanupBase, cancelCleanup := context.WithCancel(ctx)
			defer cancelCleanup()
			cleanupCtx := &keyringWaitingContext{Context: cleanupBase, waiting: make(chan struct{})}
			cleanupDone := make(chan error, 1)
			remove := func(_ context.Context, key string) error {
				if key != id || !stored.Load() {
					return errors.New("cleanup inspected an ID before its store completed")
				}
				removed.Store(true)
				return nil
			}
			go func() { cleanupDone <- cleanupKeyringIDs(cleanupCtx, dir, remove) }()
			select {
			case <-cleanupCtx.waiting:
			case err := <-cleanupDone:
				t.Fatal("cleanup bypassed the active store lock", err)
			case <-ctx.Done():
				t.Fatal(ctx.Err())
			}
			marker := filepath.Join(dir, keyringCleanupDirectory, id)
			if canceled {
				cancelCleanup()
				if err := <-cleanupDone; !errors.Is(err, context.Canceled) {
					t.Fatal("waiting cleanup ignored cancellation", err)
				}
				if _, err := os.Stat(marker); err != nil || removed.Load() {
					t.Fatal("canceled cleanup removed an active store's marker", err)
				}
			}
			releaseStore.Do(func() { close(finishStore) })
			if err := <-wrapDone; err != nil {
				t.Fatal(err)
			}
			if canceled {
				if _, err := os.Stat(marker); err != nil {
					t.Fatal("completed store became an untracked key", err)
				}
				if err := cleanupKeyringIDs(ctx, dir, remove); err != nil {
					t.Fatal(err)
				}
			} else if err := <-cleanupDone; err != nil {
				t.Fatal(err)
			}
			if !removed.Load() {
				t.Fatal("cleanup never removed the completed orphan")
			}
			if _, err := os.Stat(marker); !os.IsNotExist(err) {
				t.Fatal("completed cleanup left its marker", err)
			}
		})
	}
}

func TestKeyringCoordinationLockDoesNotFollowSymlinks(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlink creation requires a separate Windows permission")
	}
	dir := t.TempDir()
	if err := registerKeyringID(dir, vault.ShortID()); err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(t.TempDir(), "untouched")
	if err := os.WriteFile(target, []byte("safe"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(target, filepath.Join(dir, keyringCleanupDirectory, ".lock")); err != nil {
		t.Fatal(err)
	}
	if err := withKeyringLock(context.Background(), dir, func() error {
		t.Fatal("accepted a symlink coordination lock")
		return nil
	}); err == nil {
		t.Fatal("accepted a symlink coordination lock")
	}
	data, err := os.ReadFile(target)
	if err != nil || string(data) != "safe" {
		t.Fatal("coordination lock changed its symlink target", err)
	}
}
