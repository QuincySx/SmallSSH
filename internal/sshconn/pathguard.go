package sshconn

import (
	"context"
	"fmt"
	"os"
	"path"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/pkg/sftp"
	"sshclient/internal/accesspolicy"
)

// CheckProtectedPaths checks the remote directory entries of a typed mutation
// over SFTP. Target ancestry containing any symlink is rejected; protected roots
// may themselves be aliases and are resolved using Lstat/ReadLink and RealPath.
// The broker must still serialize its own writes. SFTP has no race-free openat
// equivalent: a separate remote process can change paths after this check.
func (c *Client) CheckProtectedPaths(ctx context.Context, paths, protected []string) error {
	if len(paths) == 0 {
		return nil
	}
	if len(protected) == 0 {
		protected = (accesspolicy.Policy{}).Paths()
	}
	if err := accesspolicy.CheckPaths(paths, protected); err != nil {
		return err
	}
	if err := ctx.Err(); err != nil {
		return pathGuardError(err)
	}
	s, err := sftp.NewClient(c.SSH)
	if err != nil {
		return pathGuardError(err)
	}
	defer s.Close()
	stop := context.AfterFunc(ctx, func() { s.Close() })
	defer stop()
	canonicalRoots := make([]string, 0, len(protected))
	for _, root := range protected {
		resolved, err := guardedPath(s, root, false, 0)
		if err != nil {
			return pathGuardError(err)
		}
		canonicalRoots = append(canonicalRoots, resolved)
	}
	for _, target := range paths {
		resolved, err := guardedPath(s, target, true, 0)
		if err != nil {
			return pathGuardError(err)
		}
		if err := accesspolicy.CheckPaths([]string{resolved}, canonicalRoots); err != nil {
			return err
		}
	}
	if err := ctx.Err(); err != nil {
		return pathGuardError(err)
	}
	return nil
}

func pathGuardError(err error) error {
	return fmt.Errorf("%w: 无法确认远端路径安全: %w", accesspolicy.ErrProtected, err)
}

func guardPathValid(p string) bool {
	return len(p) > 0 && len(p) <= 4096 && utf8.ValidString(p) && path.IsAbs(p) && path.Clean(p) == p && !strings.ContainsFunc(p, unicode.IsControl)
}

func guardedPath(s *sftp.Client, p string, rejectLinks bool, links int) (string, error) {
	if !guardPathValid(p) || links > 40 {
		return "", fmt.Errorf("路径无法规范解析")
	}
	current := "/"
	parts := strings.Split(strings.TrimPrefix(p, "/"), "/")
	if p == "/" {
		return verifiedRealPath(s, current)
	}
	for i, part := range parts {
		next := path.Join(current, part)
		info, err := s.Lstat(next)
		if os.IsNotExist(err) {
			base, err := verifiedRealPath(s, current)
			if err != nil {
				return "", err
			}
			return path.Join(append([]string{base}, parts[i:]...)...), nil
		}
		if err != nil {
			return "", err
		}
		if info.Mode()&os.ModeSymlink != 0 {
			if rejectLinks {
				return "", fmt.Errorf("写入目标包含符号链接，请选择真实绝对路径")
			}
			target, err := s.ReadLink(next)
			if err != nil {
				return "", err
			}
			// Cleaning a link target before resolving it changes the meaning
			// of "link/.." when link is itself a symlink. Fail closed rather
			// than protecting a different directory from the remote kernel.
			for _, component := range strings.Split(target, "/") {
				if component == ".." {
					return "", fmt.Errorf("受保护路径的符号链接目标包含无法确认的上级目录")
				}
			}
			if !path.IsAbs(target) {
				target = path.Join(current, target)
			}
			if i+1 < len(parts) {
				target = path.Join(append([]string{target}, parts[i+1:]...)...)
			}
			return guardedPath(s, path.Clean(target), false, links+1)
		}
		if i+1 < len(parts) && !info.IsDir() {
			return "", fmt.Errorf("路径祖先不是目录")
		}
		current = next
	}
	return verifiedRealPath(s, current)
}

func verifiedRealPath(s *sftp.Client, p string) (string, error) {
	real, err := s.RealPath(p)
	if err != nil {
		return "", err
	}
	if !guardPathValid(real) || real != p {
		return "", fmt.Errorf("服务器返回无法确认的规范路径")
	}
	return real, nil
}
