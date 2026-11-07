package vault

import "errors"

// ResolveRoute snapshots the configured route from destination toward the first
// hop. It never dials, and its credentials remain internal to the broker.
func (s *Session) ResolveRoute(target Profile) ([]Profile, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return nil, ErrUnlock
	}
	return resolveRoute(target, s.profiles)
}
func resolveRoute(target Profile, profiles []Profile) ([]Profile, error) {
	route := []Profile{cloneProfile(target)}
	seen := map[string]bool{target.ID: true}
	for route[len(route)-1].Network.Relay != "" {
		id := route[len(route)-1].Network.Relay
		if seen[id] || len(route) >= 4 {
			return nil, errors.New("SSH 中转存在循环或超过 4 跳")
		}
		seen[id] = true
		found := false
		for _, p := range profiles {
			if p.ID == id {
				route = append(route, cloneProfile(p))
				found = true
				break
			}
		}
		if !found {
			return nil, errors.New("中转连接不存在，请先保存中转服务器")
		}
	}
	// A proxy is used only on the first physical hop. Reject ambiguous saved
	// settings instead of silently ignoring a destination's proxy selection.
	for _, p := range route[:len(route)-1] {
		if p.Network.Proxy.Mode != "" && p.Network.Proxy.Mode != "none" {
			return nil, errors.New("同一跳不能同时选择外部代理和 SSH 中转")
		}
	}
	return route, nil
}
