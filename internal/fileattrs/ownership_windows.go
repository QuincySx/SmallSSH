//go:build windows

package fileattrs

import "os"

func localOwnership(os.FileInfo) (uint32, uint32, bool) { return 0, 0, false }
