//go:build !probe_baseline && !with_quic

package main

import "github.com/sagernet/sing-box/adapter/outbound"

func registerHy2(*outbound.Registry) {}
