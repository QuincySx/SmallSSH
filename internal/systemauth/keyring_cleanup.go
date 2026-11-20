package systemauth

import (
	"context"
	"encoding/base64"
	"errors"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"time"

	"sshclient/internal/localipc"
)

const keyringCleanupDirectory = "system-auth-cleanup"

var errKeyringLockBusy = errors.New("系统钥匙串清理正在使用")

// The same permanent inode coordinates sealing in an old frontend with cleanup
// in a replacement daemon. In particular, the register + secret-store sequence
// must finish before cleanup can decide that an identifier has no key to remove.
func withKeyringLock(ctx context.Context, dir string, action func() error) error {
	root, err := keyringCleanupRoot(dir, true)
	if err != nil {
		return err
	}
	defer root.Close()
	directory, err := root.Open(".")
	if err != nil {
		return err
	}
	defer directory.Close()
	ticker := time.NewTicker(25 * time.Millisecond)
	defer ticker.Stop()
	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		release, err := tryKeyringLock(directory)
		if err == nil {
			defer release()
			return action()
		}
		if !errors.Is(err, errKeyringLockBusy) {
			return err
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
		}
	}
}

func validKeyringID(id string) bool {
	decoded, err := base64.RawURLEncoding.DecodeString(id)
	return err == nil && len(id) == 24 && len(decoded) == 18
}

// Each empty file names one external device-key entry. No wrapped vault key,
// nonce, ciphertext, or plaintext key is persisted in this directory.
func keyringCleanupRoot(dir string, create bool) (*os.Root, error) {
	if !filepath.IsAbs(dir) {
		return nil, errors.New("系统钥匙串清理目录必须是完整路径")
	}
	path := filepath.Join(dir, keyringCleanupDirectory)
	if create {
		if err := localipc.Prepare(path); err != nil {
			return nil, err
		}
	} else {
		info, err := os.Lstat(path)
		if err != nil {
			return nil, err
		}
		if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 || runtime.GOOS != "windows" && info.Mode().Perm()&0077 != 0 {
			return nil, errors.New("系统钥匙串清理目录无效")
		}
	}
	return os.OpenRoot(path)
}

func registerKeyringID(dir, id string) error {
	if !validKeyringID(id) {
		return errors.New("系统钥匙串条目标识无效")
	}
	root, err := keyringCleanupRoot(dir, true)
	if err != nil {
		return err
	}
	defer root.Close()
	file, err := root.OpenFile(id, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if err != nil {
		return err
	}
	err = file.Sync()
	err = errors.Join(err, file.Close())
	if err == nil && runtime.GOOS != "windows" {
		// The identifier must survive a crash after secret storage succeeds.
		directory, openErr := root.Open(".")
		if openErr != nil {
			err = openErr
		} else {
			err = directory.Sync()
			err = errors.Join(err, directory.Close())
		}
	}
	if err == nil && runtime.GOOS != "windows" {
		parent, openErr := os.Open(dir)
		if openErr != nil {
			err = openErr
		} else {
			err = parent.Sync()
			err = errors.Join(err, parent.Close())
		}
	}
	if err != nil {
		_ = root.Remove(id)
	}
	return err
}

func unregisterKeyringID(dir, id string) error {
	if !validKeyringID(id) {
		return errors.New("系统钥匙串条目标识无效")
	}
	root, err := keyringCleanupRoot(dir, false)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	defer root.Close()
	info, err := root.Lstat(id)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	if !info.Mode().IsRegular() || info.Size() != 0 || runtime.GOOS != "windows" && info.Mode().Perm()&0077 != 0 {
		return errors.New("系统钥匙串清理记录无效")
	}
	err = root.Remove(id)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	return err
}

// The caller must own this data directory's service lock and must not yet be
// accepting frontends. Other directories' live envelopes are never inspected.
func cleanupKeyringIDs(ctx context.Context, dir string, remove func(context.Context, string) error) error {
	// Missing metadata needs neither directory creation nor a lock file.
	root, err := keyringCleanupRoot(dir, false)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	root.Close()
	return withKeyringLock(ctx, dir, func() error {
		return cleanupLockedKeyringIDs(ctx, dir, remove)
	})
}

func cleanupLockedKeyringIDs(ctx context.Context, dir string, remove func(context.Context, string) error) error {
	root, err := keyringCleanupRoot(dir, false)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	defer root.Close()
	directory, err := root.Open(".")
	if err != nil {
		return err
	}
	defer directory.Close()
	var result error
	for {
		entries, readErr := directory.ReadDir(128)
		for _, entry := range entries {
			if err := ctx.Err(); err != nil {
				return errors.Join(result, err)
			}
			id := entry.Name()
			if !validKeyringID(id) {
				continue
			}
			info, err := root.Lstat(id)
			if err != nil {
				result = errors.Join(result, err)
				continue
			}
			if !info.Mode().IsRegular() || info.Size() != 0 || runtime.GOOS != "windows" && info.Mode().Perm()&0077 != 0 {
				result = errors.Join(result, errors.New("系统钥匙串清理记录无效"))
				continue
			}
			if err := remove(ctx, id); err != nil {
				result = errors.Join(result, err)
				continue
			}
			if err := root.Remove(id); err != nil && !errors.Is(err, os.ErrNotExist) {
				result = errors.Join(result, err)
			}
		}
		if readErr != nil {
			if errors.Is(readErr, io.EOF) {
				return result
			}
			return errors.Join(result, readErr)
		}
	}
}
