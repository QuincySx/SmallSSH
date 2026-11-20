//go:build !darwin

package hardware

import "errors"

func openMacSmartCard() (hardwareSession, error) {
	return nil, errors.New("系统智能卡仅在 macOS 上可用")
}
