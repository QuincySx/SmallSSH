// Package fileattrs contains file metadata and validated attribute changes.
package fileattrs

import (
	"errors"
	"fmt"
	"os"
	"strconv"

	"github.com/pkg/sftp"
)

// Snapshot is a value, not a mutable pointer: approval proofs bind every field.
type Snapshot struct {
	Valid          bool   `json:"valid"`
	Mode           uint32 `json:"mode"`
	UID            uint32 `json:"uid"`
	GID            uint32 `json:"gid"`
	OwnershipKnown bool   `json:"ownershipKnown"`
	Type           uint32 `json:"type"`
	Size           int64  `json:"size"`
	Modified       int64  `json:"modified"`
}

type Change struct {
	Expected Snapshot `json:"expected"`
	SetMode  bool     `json:"setMode,omitempty"`
	Mode     uint32   `json:"mode,omitempty"`
	SetUID   bool     `json:"setUid,omitempty"`
	UID      uint32   `json:"uid,omitempty"`
	SetGID   bool     `json:"setGid,omitempty"`
	GID      uint32   `json:"gid,omitempty"`
}

type Info struct {
	Snapshot
	Name   string    `json:"name"`
	Users  []Account `json:"users,omitempty"`
	Groups []Account `json:"groups,omitempty"`
	Note   string    `json:"note,omitempty"`
}

type Account struct {
	Name string `json:"name"`
	ID   uint32 `json:"id"`
}

func (c Change) Validate() error {
	if !c.Expected.Valid || c.Expected.Mode > 07777 || !c.SetMode && !c.SetUID && !c.SetGID || c.Mode > 0777 {
		return errors.New("文件属性变更不完整或权限无效")
	}
	if (c.SetUID || c.SetGID) && !c.Expected.OwnershipKnown {
		return errors.New("此文件系统不提供 Unix 所属用户和组")
	}
	if c.SetUID && c.UID == ^uint32(0) || c.SetGID && c.GID == ^uint32(0) {
		return errors.New("UID/GID 超出可用范围")
	}
	if !c.SetMode && c.Mode != 0 || !c.SetUID && c.UID != 0 || !c.SetGID && c.GID != 0 {
		return errors.New("未选择的文件属性不能携带修改值")
	}
	return nil
}

func ModeBits(mode os.FileMode) uint32 {
	n := uint32(mode.Perm())
	if mode&os.ModeSetuid != 0 {
		n |= 04000
	}
	if mode&os.ModeSetgid != 0 {
		n |= 02000
	}
	if mode&os.ModeSticky != 0 {
		n |= 01000
	}
	return n
}

func FileMode(bits uint32) os.FileMode {
	m := os.FileMode(bits & 0777)
	if bits&04000 != 0 {
		m |= os.ModeSetuid
	}
	if bits&02000 != 0 {
		m |= os.ModeSetgid
	}
	if bits&01000 != 0 {
		m |= os.ModeSticky
	}
	return m
}

func FromStat(f os.FileInfo) Snapshot {
	uid, gid, known := localOwnership(f)
	if st, ok := f.Sys().(*sftp.FileStat); ok {
		uid, gid, known = st.UID, st.GID, true
	}
	return Snapshot{Valid: true, Mode: ModeBits(f.Mode()), UID: uid, GID: gid, OwnershipKnown: known,
		Type: uint32(f.Mode() & os.ModeType), Size: f.Size(), Modified: f.ModTime().UnixNano()}
}

type Handle interface {
	Stat() (os.FileInfo, error)
	Chmod(os.FileMode) error
	Chown(int, int) error
}

// Apply operates on an already opened handle, never a second path lookup.
// Chown may clear set-ID bits; subsequent chmod retains the post-chown bits.
func Apply(f Handle, c Change) (Snapshot, error) {
	if err := c.Validate(); err != nil {
		return Snapshot{}, err
	}
	before, err := f.Stat()
	if err != nil {
		return Snapshot{}, err
	}
	actual := FromStat(before)
	if actual != c.Expected {
		return actual, errors.New("文件属性已变化，请重新核对后保存")
	}
	if !before.Mode().IsRegular() && !before.IsDir() {
		return actual, errors.New("仅修改普通文件和目录的属性")
	}
	result := func(action string, failure error) (Snapshot, error) {
		after, statErr := f.Stat()
		if statErr != nil {
			return Snapshot{}, fmt.Errorf("%s；无法重新读取属性：%w", action, statErr)
		}
		if failure != nil {
			return FromStat(after), fmt.Errorf("%s：%w", action, failure)
		}
		return FromStat(after), nil
	}
	if c.SetUID || c.SetGID {
		uid, gid := actual.UID, actual.GID
		if c.SetUID {
			uid = c.UID
		}
		if c.SetGID {
			gid = c.GID
		}
		if err = f.Chown(int(uid), int(gid)); err != nil {
			return result("所属用户和组保存失败", err)
		}
	}
	if c.SetMode {
		current, e := f.Stat()
		if e != nil {
			return result("权限保存前读取失败", e)
		}
		mode := ModeBits(current.Mode())&07000 | c.Mode
		if err = f.Chmod(FileMode(mode)); err != nil {
			return result("权限保存失败（其他已完成的修改不会回滚）", err)
		}
	}
	return result("属性已保存", nil)
}

func AccountValue(id uint32, accounts []Account) string {
	for _, a := range accounts {
		if a.ID == id {
			return a.Name
		}
	}
	return strconv.FormatUint(uint64(id), 10)
}

func ResolveAccount(text string, accounts []Account) (uint32, error) {
	for _, a := range accounts {
		if a.Name == text {
			return a.ID, nil
		}
	}
	n, err := strconv.ParseUint(text, 10, 32)
	if err != nil || n == uint64(^uint32(0)) {
		return 0, errors.New("请选择已有名称，或输入有效的数字 UID/GID")
	}
	return uint32(n), nil
}
