//go:build !windows

package broker

// windowsProcessPath is unused outside Windows.
func windowsProcessPath(uint32) string { return "" }
