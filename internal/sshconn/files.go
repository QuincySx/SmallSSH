package sshconn

import (
	"context"
	"errors"
	"os"

	"github.com/pkg/sftp"
)

// Mutate uses SFTP operations, never interpolates remote names into shell code.
// Removal is deliberately non-recursive, including for directories and symlinks.
func (c *Client) Mutate(ctx context.Context, kind, p, destination string, mode uint32) error {
	if e := remotePath(p); e != nil {
		return e
	}
	if p == "/" {
		return errors.New("不能操作根目录")
	}
	if kind == "rename" {
		if e := remotePath(destination); e != nil {
			return e
		}
		if destination == "/" {
			return errors.New("目标不能为根目录")
		}
	}
	s, e := sftp.NewClient(c.SSH)
	if e != nil {
		return e
	}
	defer s.Close()
	stop := context.AfterFunc(ctx, func() { s.Close() })
	defer stop()
	if e = ctx.Err(); e != nil {
		return e
	}
	switch kind {
	case "mkdir":
		return s.Mkdir(p)
	case "remove":
		st, e := s.Lstat(p)
		if e != nil {
			return e
		}
		if st.IsDir() {
			return s.RemoveDirectory(p)
		}
		return s.Remove(p)
	case "rename":
		if _, e = s.Lstat(destination); e == nil {
			return errors.New("目标已存在，请使用新名称")
		} else if !os.IsNotExist(e) {
			return e
		}
		return s.Rename(p, destination)
	case "chmod":
		if mode > 0777 {
			return errors.New("权限超出范围")
		}
		st, e := s.Lstat(p)
		if e != nil {
			return e
		}
		if st.Mode()&os.ModeSymlink != 0 {
			return errors.New("请直接选择目标文件，不修改符号链接权限")
		}
		return s.Chmod(p, os.FileMode(mode))
	default:
		return errors.New("不支持的文件操作")
	}
}
