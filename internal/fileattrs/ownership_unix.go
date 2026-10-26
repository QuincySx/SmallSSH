//go:build !windows

package fileattrs

import (
	"os"
	"syscall"
)

func localOwnership(f os.FileInfo) (uint32, uint32, bool) {
	if st, ok := f.Sys().(*syscall.Stat_t); ok {
		return st.Uid, st.Gid, true
	}
	return 0, 0, false
}
