//go:build !with_utls || !with_quic

package netroute

import (
	"context"
	"errors"
	"net"

	"sshclient/internal/connectcfg"
)

func dialEmbedded(context.Context, string, connectcfg.Proxy) (net.Conn, error) {
	return nil, errors.New("当前构建未包含代理协议；请通过 go run ./tools 构建")
}
