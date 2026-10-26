package fileattrs

import (
	"context"
	"errors"
	"os"
	"os/user"
	"path/filepath"
	"strconv"
)

func LocalInfo(ctx context.Context, name string) (Info, error) {
	if err := ctx.Err(); err != nil {
		return Info{}, err
	}
	st, err := os.Lstat(name)
	if err != nil {
		return Info{}, err
	}
	info := Info{Snapshot: FromStat(st), Name: st.Name()}
	if !info.OwnershipKnown {
		info.Note = "此文件系统不提供 Unix 所属用户和组"
		return info, nil
	}
	for _, target := range []struct {
		path string
		into *[]Account
	}{{"/etc/passwd", &info.Users}, {"/etc/group", &info.Groups}} {
		f, e := os.Open(target.path)
		if e != nil {
			info.Note = "部分账户名称不可用，可填写数字 UID/GID"
			continue
		}
		stop := context.AfterFunc(ctx, func() { f.Close() })
		*target.into, e = Accounts(f)
		stop()
		f.Close()
		if e != nil {
			info.Note = "部分账户名称不可用，可填写数字 UID/GID"
		}
	}
	// macOS directory-service users are not necessarily in /etc/passwd.
	if u, e := user.LookupId(strconv.FormatUint(uint64(info.UID), 10)); e == nil {
		info.Users = appendAccount(info.Users, Account{Name: u.Username, ID: info.UID})
	}
	if g, e := user.LookupGroupId(strconv.FormatUint(uint64(info.GID), 10)); e == nil {
		info.Groups = appendAccount(info.Groups, Account{Name: g.Name, ID: info.GID})
	}
	if err = ctx.Err(); err != nil {
		return Info{}, err
	}
	return info, nil
}

func appendAccount(accounts []Account, a Account) []Account {
	for _, existing := range accounts {
		if existing.Name == a.Name {
			return accounts
		}
	}
	return append(accounts, a)
}

func SetLocalAttributes(ctx context.Context, name string, change Change) (Info, error) {
	if err := ctx.Err(); err != nil {
		return Info{}, err
	}
	if filepath.Dir(filepath.Clean(name)) == filepath.Clean(name) {
		return Info{}, errors.New("不能修改根目录属性")
	}
	if err := change.Validate(); err != nil {
		return Info{}, err
	}
	st, err := os.Lstat(name)
	if err != nil {
		return Info{}, err
	}
	info := Info{Snapshot: FromStat(st), Name: st.Name()}
	if !st.Mode().IsRegular() && !st.IsDir() {
		return info, errors.New("仅修改普通文件和目录的属性；符号链接请选择真实目标")
	}
	if info.Snapshot != change.Expected {
		return info, errors.New("文件属性已变化，请重新核对后保存")
	}
	f, err := openAttributeHandle(name)
	if err != nil {
		return info, err
	}
	defer f.Close()
	opened, err := f.Stat()
	if err != nil {
		return info, err
	}
	if !os.SameFile(st, opened) {
		return info, errors.New("文件已被替换，请重新打开属性")
	}
	stop := context.AfterFunc(ctx, func() { f.Close() })
	defer stop()
	info.Snapshot, err = Apply(f, change)
	return info, err
}
