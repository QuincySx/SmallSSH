package sshconn

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"github.com/pkg/sftp"
	"io"
	"os"
	"path"
	"sshclient/internal/vault"
)

// Replace is an optimistic SFTP editor save: verify both versions, copy a
// separate backup, then atomically replace the directory entry. SFTP has no
// compare-and-swap primitive; external writers can still race the final check.
// The broker serializes its own writes. Existing ownership/ACLs are not copied.
func (c *Client) Replace(ctx context.Context, p string, r io.Reader, newHash, oldHash string) (string, error) {
	if e := remotePath(p); e != nil {
		return "", e
	}
	s, e := sftp.NewClient(c.SSH)
	if e != nil {
		return "", e
	}
	defer s.Close()
	stop := context.AfterFunc(ctx, func() { s.Close() })
	defer stop()
	if _, ok := s.HasExtension("posix-rename@openssh.com"); !ok {
		return "", errors.New("覆盖保存需要服务器支持原子重命名扩展")
	}
	info, e := s.Lstat(p)
	if e != nil {
		return "", e
	}
	if !info.Mode().IsRegular() {
		return "", errors.New("仅允许覆盖普通文件，拒绝符号链接")
	}
	tmp := path.Join(path.Dir(p), ".ssh-edit-"+vault.ShortID())
	f, e := s.OpenFile(tmp, os.O_WRONLY|os.O_CREATE|os.O_EXCL)
	if e != nil {
		return "", e
	}
	defer s.Remove(tmp)
	defer f.Close()
	if e = f.Chmod(0600); e != nil {
		return "", e
	}
	hash := sha256.New()
	if _, e = io.Copy(f, io.TeeReader(r, hash)); e != nil {
		return "", e
	}
	if hex.EncodeToString(hash.Sum(nil)) != newHash {
		return "", errors.New("新内容与批准的指纹不一致")
	}
	if e = f.Close(); e != nil {
		return "", e
	}
	original, e := s.Open(p)
	if e != nil {
		return "", e
	}
	defer original.Close()
	backup := path.Join(path.Dir(p), "."+path.Base(p)+".ssh-backup-"+vault.ShortID())
	b, e := s.OpenFile(backup, os.O_WRONLY|os.O_CREATE|os.O_EXCL)
	if e != nil {
		return "", e
	}
	defer b.Close()
	keep := false
	defer func() {
		if !keep {
			s.Remove(backup)
		}
	}()
	if e = b.Chmod(0600); e != nil {
		return "", e
	}
	hash = sha256.New()
	if _, e = io.Copy(b, io.TeeReader(original, hash)); e != nil {
		return "", e
	}
	if hex.EncodeToString(hash.Sum(nil)) != oldHash {
		return "", errors.New("远程文件已发生变化，请重新读取后再保存")
	}
	if e = b.Close(); e != nil {
		return "", e
	}
	original.Close()
	current, e := s.Open(p)
	if e != nil {
		return "", e
	}
	hash = sha256.New()
	_, e = io.Copy(hash, current)
	current.Close()
	if e != nil {
		return "", e
	}
	if hex.EncodeToString(hash.Sum(nil)) != oldHash {
		return "", errors.New("远程文件在备份期间发生变化，请重新读取")
	}
	// Use a fresh Lstat to reject a symlink introduced during editing.
	latest, e := s.Lstat(p)
	if e != nil {
		return "", e
	}
	if !latest.Mode().IsRegular() {
		return "", errors.New("目标文件类型已变化")
	}
	if e = s.Chmod(tmp, info.Mode().Perm()); e != nil {
		return "", e
	}
	keep = true
	if e = s.PosixRename(tmp, p); e != nil {
		return "", fmt.Errorf("提交结果未确认，旧文件副本保留于 %q: %w", backup, e)
	}
	return backup, nil
}
