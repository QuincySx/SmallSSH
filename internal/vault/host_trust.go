package vault

import (
	"encoding/base64"
	"errors"
	"strings"
)

// HostTrustChange pins only the host key observed after an explicit connect.
// It cannot replace credentials, routing, or newer edits from another window.
type HostTrustChange struct {
	ID          string `json:"id"`
	Host        string `json:"host"`
	Port        int    `json:"port"`
	Previous    string `json:"previous"`
	Fingerprint string `json:"fingerprint"`
}

func (s *Session) SaveHostTrust(file string, change HostTrustChange) error {
	decoded, err := base64.RawStdEncoding.DecodeString(strings.TrimPrefix(change.Fingerprint, "SHA256:"))
	if !strings.HasPrefix(change.Fingerprint, "SHA256:") || err != nil || len(decoded) != 32 || base64.RawStdEncoding.EncodeToString(decoded) != strings.TrimPrefix(change.Fingerprint, "SHA256:") {
		return errors.New("服务器指纹格式无效")
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return ErrUnlock
	}
	for i, p := range s.profiles {
		if p.ID != change.ID {
			continue
		}
		if p.Host != change.Host || p.Port != change.Port || p.Fingerprint != change.Previous {
			return errors.New("连接目标或指纹已更新，请重新连接")
		}
		next := append([]Profile(nil), s.profiles...)
		next[i].Fingerprint = change.Fingerprint
		copy := &Session{header: s.header, key: s.key, profiles: next, routes: s.routes, snippets: s.snippets, notes: s.notes, auditHead: s.auditHead}
		if err := Save(file, copy, false); err != nil {
			return err
		}
		s.profiles = next
		return nil
	}
	return errors.New("连接不存在")
}
