//go:build !windows && !linux && !darwin

package localipc

import "errors"

// peerPIDOfFD is unavailable here; the caller falls back to its own accounting.
func peerPIDOfFD(int) (int, error) {
	return 0, errors.New("当前平台无法读取本地对端进程")
}
