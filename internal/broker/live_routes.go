package broker

import (
	"errors"

	"sshclient/internal/connectcfg"
	"sshclient/internal/vault"
)

// Runtime routes name an owner-approved live operation, never an arbitrary
// caller-provided proxy address. They are resolved only inside the broker.
func (s *Server) operationRoute(target vault.Profile, selection connectcfg.RouteSelection) ([]vault.Profile, error) {
	return s.operationRouteAt(target, selection, map[string]bool{}, 0)
}
func (s *Server) operationRouteAt(target vault.Profile, selection connectcfg.RouteSelection, seen map[string]bool, depth int) ([]vault.Profile, error) {
	if err := selection.Validate(); err != nil {
		return nil, err
	}
	if depth >= 4 {
		return nil, errors.New("SSH 中继超过 4 跳")
	}
	if selection.Mode != "session" && selection.Mode != "tunnel" {
		target.Network = selection.Apply(target.Network)
		return s.Vault.ResolveRoute(target)
	}
	if seen[selection.ID] {
		return nil, errors.New("SSH 中继存在循环")
	}
	seen[selection.ID] = true
	source, err := s.Approvals.Get(selection.ID)
	if err != nil || !source.OwnerIssued || source.Status != "running" {
		return nil, errors.New("中继会话已结束，请重新选择")
	}
	target.Network = (connectcfg.RouteSelection{Mode: "direct"}).Apply(target.Network)
	if selection.Mode == "tunnel" {
		if source.Operation.Kind != "tunnel" || source.Operation.Tunnel.Kind != "socks5" {
			return nil, errors.New("所选会话不是 SOCKS5 入口")
		}
		address, ok := s.liveForwards.Load(selection.ID)
		if !ok {
			return nil, errors.New("SOCKS5 入口已停止")
		}
		target.Network.Proxy = connectcfg.Proxy{Mode: "socks5", Address: address.(string)}
		return s.Vault.ResolveRoute(target)
	}
	if source.Operation.Kind != "shell" || source.Operation.Host == target.ID {
		return nil, errors.New("不能使用此会话作为 SSH 中继")
	}
	relay, err := s.Vault.Profile(source.Operation.Host)
	if err != nil {
		return nil, err
	}
	upstream, err := s.operationRouteAt(relay, source.Operation.Route, seen, depth+1)
	if err != nil {
		return nil, err
	}
	if len(upstream) >= 4 {
		return nil, errors.New("SSH 中继超过 4 跳")
	}
	for _, hop := range upstream {
		if hop.ID == target.ID {
			return nil, errors.New("SSH 中继存在循环")
		}
	}
	target.Network.Relay = relay.ID
	return append([]vault.Profile{target}, upstream...), nil
}
