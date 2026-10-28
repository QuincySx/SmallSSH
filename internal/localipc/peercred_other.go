//go:build !windows && !linux && !darwin

package localipc

import "errors"

// peerUIDOfFD is unavailable here; connections are refused rather than trusted.
func peerUIDOfFD(int) (uint32, error) {
	return 0, errors.New("当前平台无法核对本地连接身份")
}
