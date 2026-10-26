package auditlog

import (
	"errors"
	"fmt"
	"os"

	"golang.org/x/sys/windows"
)

func openPrivate(path string, create bool) (*os.File, error) {
	st, err := os.Lstat(path)
	if err != nil && !(create && os.IsNotExist(err)) {
		return nil, err
	}
	if err == nil && (!st.Mode().IsRegular() || st.Mode()&os.ModeSymlink != 0) {
		return nil, errors.New("历史文件必须是普通文件")
	}
	name, err := windows.UTF16PtrFromString(path)
	if err != nil {
		return nil, err
	}
	disposition := uint32(windows.OPEN_EXISTING)
	if create {
		disposition = windows.OPEN_ALWAYS
	}
	// Open the reparse point itself, and allow renaming this verified handle
	// during rotation. ACL changes below apply to the handle, never a raced path.
	handle, err := windows.CreateFile(name, windows.GENERIC_READ|windows.GENERIC_WRITE|windows.READ_CONTROL|windows.WRITE_DAC, windows.FILE_SHARE_READ|windows.FILE_SHARE_WRITE|windows.FILE_SHARE_DELETE, nil, disposition, windows.FILE_ATTRIBUTE_NORMAL|windows.FILE_FLAG_OPEN_REPARSE_POINT, 0)
	if err != nil {
		return nil, err
	}
	f := os.NewFile(uintptr(handle), path)
	opened, err := f.Stat()
	after, statErr := os.Lstat(path)
	if err != nil || statErr != nil || !opened.Mode().IsRegular() || !os.SameFile(opened, after) || after.Mode()&os.ModeSymlink != 0 {
		f.Close()
		return nil, errors.New("历史文件已改变或不是普通文件")
	}
	var info windows.ByHandleFileInformation
	if err = windows.GetFileInformationByHandle(handle, &info); err != nil {
		f.Close()
		return nil, err
	}
	if info.NumberOfLinks != 1 || info.FileAttributes&windows.FILE_ATTRIBUTE_REPARSE_POINT != 0 {
		f.Close()
		return nil, errors.New("历史文件不能是链接")
	}
	user, err := windows.GetCurrentProcessToken().GetTokenUser()
	if err != nil {
		f.Close()
		return nil, err
	}
	security, err := windows.GetSecurityInfo(handle, windows.SE_FILE_OBJECT, windows.OWNER_SECURITY_INFORMATION)
	if err != nil {
		f.Close()
		return nil, err
	}
	owner, _, err := security.Owner()
	if err != nil || owner == nil || !owner.Equals(user.User.Sid) {
		f.Close()
		return nil, errors.New("历史文件不属于当前用户")
	}
	private, err := windows.SecurityDescriptorFromString("D:P(A;;FA;;;" + user.User.Sid.String() + ")")
	if err != nil {
		f.Close()
		return nil, err
	}
	acl, _, err := private.DACL()
	if err == nil {
		err = windows.SetSecurityInfo(handle, windows.SE_FILE_OBJECT, windows.DACL_SECURITY_INFORMATION|windows.PROTECTED_DACL_SECURITY_INFORMATION, nil, nil, acl, nil)
	}
	if err != nil {
		f.Close()
		return nil, err
	}
	return f, nil
}

func fileIdentity(f *os.File) (string, error) {
	var st windows.ByHandleFileInformation
	if err := windows.GetFileInformationByHandle(windows.Handle(f.Fd()), &st); err != nil {
		return "", err
	}
	return fmt.Sprintf("%d:%d:%d", st.VolumeSerialNumber, st.FileIndexHigh, st.FileIndexLow), nil
}
