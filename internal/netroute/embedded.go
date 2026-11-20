//go:build with_utls && with_quic

package netroute

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"strings"
	"sync"

	box "github.com/sagernet/sing-box"
	"github.com/sagernet/sing-box/adapter/certificate"
	"github.com/sagernet/sing-box/adapter/endpoint"
	"github.com/sagernet/sing-box/adapter/inbound"
	"github.com/sagernet/sing-box/adapter/outbound"
	boxservice "github.com/sagernet/sing-box/adapter/service"
	"github.com/sagernet/sing-box/dns"
	"github.com/sagernet/sing-box/dns/transport/local"
	"github.com/sagernet/sing-box/option"
	"github.com/sagernet/sing-box/protocol/anytls"
	"github.com/sagernet/sing-box/protocol/hysteria2"
	"github.com/sagernet/sing-box/protocol/shadowsocks"
	"github.com/sagernet/sing-box/protocol/snell"
	"github.com/sagernet/sing-box/protocol/vless"
	sjson "github.com/sagernet/sing/common/json"
	M "github.com/sagernet/sing/common/metadata"

	"sshclient/internal/connectcfg"
	"sshclient/internal/proxyconfig"
)

func dialEmbedded(ctx context.Context, target string, p connectcfg.Proxy) (net.Conn, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	raw, err := proxyconfig.Outbound(p.Mode, p.Address, p.Config, p.Password, true)
	if err != nil {
		return nil, err
	}
	// The handshake context is cancelled by the SSH caller after dialing. The
	// route's session pools instead live exactly as long as the returned socket.
	life, cancel := context.WithCancel(context.WithoutCancel(ctx))
	registry := outbound.NewRegistry()
	shadowsocks.RegisterOutbound(registry)
	snell.RegisterOutbound(registry)
	vless.RegisterOutbound(registry)
	anytls.RegisterOutbound(registry)
	hysteria2.RegisterOutbound(registry)
	resolvers := dns.NewTransportRegistry()
	local.RegisterTransport(resolvers)
	life = box.Context(life, inbound.NewRegistry(), registry, endpoint.NewRegistry(),
		resolvers, boxservice.NewRegistry(), certificate.NewRegistry())
	var node option.Outbound
	if err := sjson.UnmarshalContext(life, raw, &node); err != nil {
		cancel()
		return nil, errors.New("代理节点参数无法解析")
	}
	if options, ok := node.Options.(*option.Hysteria2OutboundOptions); ok {
		// SSH needs standard QUIC/TLS, not browser fingerprint emulation. The
		// standard handshake also supports Ed25519 server certificates.
		options.DisableChromeParrot = true
	}
	node.Tag = "ssh-route"
	instance, err := box.New(box.Options{Context: life, Options: option.Options{
		Log: &option.LogOptions{Disabled: true}, Outbounds: []option.Outbound{node},
	}})
	if err != nil {
		cancel()
		return nil, embeddedError(p, err)
	}
	release := sync.OnceFunc(func() { cancel(); _ = instance.Close() })
	// Cancellation may interrupt dialing, but never races Box.Close against
	// Box.Start. The caller below always performs the final cleanup serially.
	stop := context.AfterFunc(ctx, cancel)
	fail := func(err error) (net.Conn, error) {
		stop()
		release()
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		return nil, embeddedError(p, err)
	}
	if err := instance.Start(); err != nil {
		return fail(err)
	}
	route, ok := instance.Outbound().Outbound(node.Tag)
	if !ok {
		return fail(errors.New("outbound unavailable"))
	}
	conn, err := route.DialContext(ctx, "tcp", M.ParseSocksaddr(target))
	if err != nil {
		return fail(err)
	}
	if !stop() || ctx.Err() != nil {
		_ = conn.Close()
		return fail(context.Canceled)
	}
	return &embeddedConn{Conn: conn, release: release}, nil
}

type embeddedConn struct {
	net.Conn
	release func()
	once    sync.Once
	err     error
}

func (c *embeddedConn) Close() error {
	c.once.Do(func() {
		c.err = c.Conn.Close()
		c.release()
	})
	return c.err
}

func embeddedError(p connectcfg.Proxy, err error) error {
	message := err.Error()
	var secrets map[string]string
	_ = json.Unmarshal([]byte(p.Password), &secrets)
	for _, secret := range secrets {
		if secret != "" {
			message = strings.ReplaceAll(message, secret, "[redacted]")
		}
	}
	return fmt.Errorf("%s 线路连接失败：%s", proxyconfig.Label(p.Mode), message)
}
