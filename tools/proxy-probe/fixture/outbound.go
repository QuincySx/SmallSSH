//go:build !probe_baseline

package main

import (
	"context"
	"fmt"
	"net"

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
	"github.com/sagernet/sing-box/protocol/shadowsocks"
	"github.com/sagernet/sing-box/protocol/snell"
	"github.com/sagernet/sing-box/protocol/vless"
	"github.com/sagernet/sing/common/json"
	M "github.com/sagernet/sing/common/metadata"
)

func dial(ctx context.Context, raw []byte, target string) (net.Conn, func(), error) {
	registry := outbound.NewRegistry()
	shadowsocks.RegisterOutbound(registry)
	snell.RegisterOutbound(registry)
	vless.RegisterOutbound(registry)
	anytls.RegisterOutbound(registry)
	registerHy2(registry)
	resolvers := dns.NewTransportRegistry()
	// This is the OS resolver client, not a DNS listener or a DNS server.
	local.RegisterTransport(resolvers)
	ctx = box.Context(ctx, inbound.NewRegistry(), registry, endpoint.NewRegistry(),
		resolvers, boxservice.NewRegistry(), certificate.NewRegistry())
	var config option.Outbound
	if err := json.UnmarshalContext(ctx, raw, &config); err != nil {
		return nil, nil, err
	}
	config.Tag = "probe"
	instance, err := box.New(box.Options{
		Context: ctx,
		Options: option.Options{
			Log:       &option.LogOptions{Disabled: true},
			Outbounds: []option.Outbound{config},
		},
	})
	if err != nil {
		return nil, nil, err
	}
	closeRoute := func() { _ = instance.Close() }
	if err := instance.Start(); err != nil {
		closeRoute()
		return nil, nil, err
	}
	route, ok := instance.Outbound().Outbound(config.Tag)
	if !ok {
		closeRoute()
		return nil, nil, fmt.Errorf("probe outbound missing after startup")
	}
	conn, err := route.DialContext(ctx, "tcp", M.ParseSocksaddr(target))
	if err != nil {
		closeRoute()
		return nil, nil, err
	}
	return conn, closeRoute, nil
}
