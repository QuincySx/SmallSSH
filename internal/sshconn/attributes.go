package sshconn

import (
	"context"
	"errors"
	"sshclient/internal/fileattrs"

	"github.com/pkg/sftp"
)

func (c *Client) FileInfo(ctx context.Context, p string) (fileattrs.Info, error) {
	if err := remotePath(p); err != nil {
		return fileattrs.Info{}, err
	}
	s, err := sftp.NewClient(c.SSH)
	if err != nil {
		return fileattrs.Info{}, err
	}
	defer s.Close()
	stop := context.AfterFunc(ctx, func() { s.Close() })
	defer stop()
	st, err := s.Lstat(p)
	if err != nil {
		return fileattrs.Info{}, err
	}
	info := fileattrs.Info{Snapshot: fileattrs.FromStat(st), Name: st.Name()}
	for _, target := range []struct {
		path string
		into *[]fileattrs.Account
	}{{"/etc/passwd", &info.Users}, {"/etc/group", &info.Groups}} {
		f, e := s.Open(target.path)
		if e != nil {
			info.Note = "部分账户名称不可用，可填写数字 UID/GID"
			continue
		}
		*target.into, e = fileattrs.Accounts(f)
		f.Close()
		if e != nil {
			info.Note = "部分账户名称不可用，可填写数字 UID/GID"
		}
	}
	if err = ctx.Err(); err != nil {
		return fileattrs.Info{}, err
	}
	return info, nil
}

func (c *Client) SetFileAttributes(ctx context.Context, p string, change fileattrs.Change) (fileattrs.Info, error) {
	if err := remotePath(p); err != nil {
		return fileattrs.Info{}, err
	}
	if p == "/" {
		return fileattrs.Info{}, errors.New("不能修改根目录属性")
	}
	if err := change.Validate(); err != nil {
		return fileattrs.Info{}, err
	}
	s, err := sftp.NewClient(c.SSH)
	if err != nil {
		return fileattrs.Info{}, err
	}
	defer s.Close()
	stop := context.AfterFunc(ctx, func() { s.Close() })
	defer stop()
	st, err := s.Lstat(p)
	if err != nil {
		return fileattrs.Info{}, err
	}
	info := fileattrs.Info{Snapshot: fileattrs.FromStat(st), Name: st.Name()}
	if !st.Mode().IsRegular() && !st.IsDir() {
		return info, errors.New("仅修改普通文件和目录的属性；符号链接请选择真实目标")
	}
	if info.Snapshot != change.Expected {
		return info, errors.New("文件属性已变化，请重新核对后保存")
	}
	f, err := s.Open(p)
	if err != nil {
		return info, err
	}
	defer f.Close()
	info.Snapshot, err = fileattrs.Apply(f, change)
	return info, err
}
