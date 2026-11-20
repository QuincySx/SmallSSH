package vault

import (
	"errors"
	"strings"

	"sshclient/internal/connectcfg"
	"sshclient/internal/proxyconfig"
)

// SavedRoute is encrypted with the vault. Only RouteSummary leaves the broker.
// Keepalive remains a property of each connection, not of a shared route.
type SavedRoute struct {
	ID    string           `json:"id"`
	Name  string           `json:"name"`
	Proxy connectcfg.Proxy `json:"proxy,omitzero"`
	Relay string           `json:"relay,omitempty"`
}

type RouteSummary struct {
	ID          string           `json:"id"`
	Name        string           `json:"name"`
	Proxy       connectcfg.Proxy `json:"proxy,omitzero"`
	Relay       string           `json:"relay,omitempty"`
	HasPassword bool             `json:"hasPassword,omitempty"`
	References  int              `json:"references,omitempty"`
}

func (r SavedRoute) Validate() error {
	if r.ID == "" || len(r.ID) > 128 || strings.ContainsAny(r.ID, "\x00\r\n") {
		return errors.New("线路 ID 无效")
	}
	if strings.TrimSpace(r.Name) == "" || len(r.Name) > 128 || strings.ContainsAny(r.Name, "\x00\r\n") {
		return errors.New("线路名称无效")
	}
	if r.Relay == "" && (r.Proxy.Mode == "" || r.Proxy.Mode == "none") {
		return errors.New("请选择代理类型或 SSH 跳板机")
	}
	return (connectcfg.Network{Proxy: r.Proxy, Relay: r.Relay}).Validate()
}

func (s *Session) Routes() []RouteSummary {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return nil
	}
	out := make([]RouteSummary, 0, len(s.routes))
	for _, r := range s.routes {
		summary := RouteSummary{ID: r.ID, Name: r.Name, Proxy: r.Proxy.Public(), Relay: r.Relay, HasPassword: r.Proxy.Password != ""}
		for _, p := range s.profiles {
			if p.Network.RouteID == r.ID {
				summary.References++
			}
		}
		out = append(out, summary)
	}
	return out
}

// SaveRoute publishes only after the encrypted write succeeds. Deleting a route
// in use is refused, rather than turning any connection into a direct dial.
func (s *Session) SaveRoute(file string, value SavedRoute, keepPassword, remove bool) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return ErrUnlock
	}
	values := append([]SavedRoute(nil), s.routes...)
	index := -1
	for i, r := range values {
		if r.ID == value.ID {
			index = i
			break
		}
	}
	if remove {
		if index < 0 {
			return errors.New("线路不存在")
		}
		for _, p := range s.profiles {
			if p.Network.RouteID == value.ID {
				return errors.New("线路仍被连接引用，请先更换这些连接的线路")
			}
		}
		values = append(values[:index], values[index+1:]...)
	} else {
		if keepPassword {
			if index < 0 {
				return errors.New("原线路不存在，无法保留代理密码")
			}
			if value.Proxy.Mode != values[index].Proxy.Mode {
				return errors.New("更换线路协议时需重新提供节点凭据")
			}
			value.Proxy.Password = values[index].Proxy.Password
		}
		if err := value.Validate(); err != nil {
			return err
		}
		if proxyconfig.Supported(value.Proxy.Mode) {
			if _, err := proxyconfig.Outbound(value.Proxy.Mode, value.Proxy.Address, value.Proxy.Config, value.Proxy.Password, true); err != nil {
				return err
			}
		}
		if index < 0 {
			if len(values) >= 128 {
				return errors.New("已保存线路最多 128 条")
			}
			values = append(values, value)
		} else {
			values[index] = value
		}
		if _, err := resolveRoute(Profile{Network: connectcfg.Network{RouteID: value.ID}}, s.profiles, values); err != nil {
			return err
		}
		if err := validateRouteChanges(s.profiles, values, s.profiles, s.routes); err != nil {
			return err
		}
	}
	copy := &Session{header: s.header, key: s.key, routes: values, profiles: s.profiles, snippets: s.snippets, notes: s.notes, auditHead: s.auditHead}
	if err := Save(file, copy, false); err != nil {
		return err
	}
	s.routes = values
	return nil
}
