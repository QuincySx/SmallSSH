//go:build !darwin

package identityagent

import (
	"context"
	"errors"
)

type unavailable struct{}

func newBackend(string) backend { return unavailable{} }
func (unavailable) call(context.Context, command) (reply, error) {
	return reply{}, errors.New("安全隔区 SSH 密钥当前仅支持 macOS")
}
func (unavailable) Close() error { return nil }
