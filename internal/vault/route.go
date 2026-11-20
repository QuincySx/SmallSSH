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
	return resolveRoute(target, s.profiles, s.routes)
}

// Preserve working routes, including indirect dependents of an edited relay.
// Older vaults may already contain dangling routes; let the owner repair those
// one at a time instead of requiring every unrelated route to work first.
func validateRouteChanges(profiles []Profile, saved []SavedRoute, previousProfiles []Profile, previousSaved []SavedRoute) error {
	for _, p := range profiles {
		if _, err := resolveRoute(p, profiles, saved); err != nil {
			previouslyInvalid := false
			for _, previous := range previousProfiles {
				if previous.ID == p.ID {
					_, previousErr := resolveRoute(previous, previousProfiles, previousSaved)
					previouslyInvalid = previousErr != nil
					break
				}
			}
			if !previouslyInvalid {
				return err
			}
		}
	}
	return nil
}

func resolveRoute(target Profile, profiles []Profile, saved []SavedRoute) ([]Profile, error) {
	expand := func(p Profile) (Profile, error) {
		p = cloneProfile(p)
		if err := p.Network.Validate(); err != nil {
			return p, err
		}
		if p.Network.RouteID == "" {
			return p, nil
		}
		for _, r := range saved {
			if r.ID == p.Network.RouteID {
				p.Network.RouteID = ""
				p.Network.Proxy, p.Network.Relay = r.Proxy, r.Relay
				return p, nil
			}
		}
		return p, errors.New("已保存线路不存在，请重新选择线路")
	}
	target, err := expand(target)
	if err != nil {
		return nil, err
	}
	route := []Profile{target}
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
				hop, err := expand(p)
				if err != nil {
					return nil, err
				}
				route = append(route, hop)
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
