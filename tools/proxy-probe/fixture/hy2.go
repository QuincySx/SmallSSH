//go:build !probe_baseline && with_quic

package main

import (
	"github.com/sagernet/sing-box/adapter/outbound"
	"github.com/sagernet/sing-box/protocol/hysteria2"
)

func registerHy2(registry *outbound.Registry) { hysteria2.RegisterOutbound(registry) }
