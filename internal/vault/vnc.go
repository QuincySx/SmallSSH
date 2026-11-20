package vault

import (
	"errors"
	"strings"
	"unicode/utf8"

	"sshclient/internal/connectcfg"
)

// VNCSettings is owner-only encrypted connection data, never a public summary.
// Address and Mode bind the password to the exact VNC destination and path.
type VNCSettings struct {
	Address  string `json:"address,omitempty"`
	Mode     string `json:"mode,omitempty"`
	Username string `json:"username,omitempty"`
	Password string `json:"password,omitempty"`
}

func (v VNCSettings) Validate() error {
	if v == (VNCSettings{}) {
		return nil
	}
	if connectcfg.Address(v.Address, false) != nil || (v.Mode != "direct" && v.Mode != "ssh") {
		return errors.New("VNC 目标或连接方式无效")
	}
	if len(v.Username) > 2048 || len(v.Password) > 2048 || !utf8.ValidString(v.Username+v.Password) || strings.ContainsRune(v.Username+v.Password, 0) {
		return errors.New("VNC 登录信息无效或过长")
	}
	return nil
}

func (s *Session) VNCSettings(id string) (VNCSettings, error) {
	p, err := s.Profile(id)
	return p.VNC, err
}

func (s *Session) SaveVNCSettings(file, id string, settings VNCSettings) error {
	if err := settings.Validate(); err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return ErrUnlock
	}
	profiles := append([]Profile(nil), s.profiles...)
	found := false
	for i := range profiles {
		if profiles[i].ID == id {
			profiles[i].VNC, found = settings, true
			break
		}
	}
	if !found {
		return errors.New("连接不存在")
	}
	copy := &Session{header: s.header, key: s.key, routes: s.routes, profiles: profiles, snippets: s.snippets, notes: s.notes, auditHead: s.auditHead}
	if err := Save(file, copy, false); err != nil {
		return err
	}
	s.profiles = profiles
	return nil
}
