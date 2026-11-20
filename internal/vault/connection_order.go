package vault

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"strings"
)

// ConnectionOrder carries public placement metadata only. Its revision detects
// another window changing membership or order while a drag was in progress.
type ConnectionPlacement struct {
	ID    string `json:"id"`
	Group string `json:"group"`
}
type ConnectionOrder struct {
	Revision string                `json:"revision"`
	Items    []ConnectionPlacement `json:"items"`
}

func ConnectionOrderRevision(rows []Summary) string {
	items := make([]ConnectionPlacement, len(rows))
	for i, row := range rows {
		items[i] = ConnectionPlacement{ID: row.ID, Group: row.Group}
	}
	return connectionOrderDigest(items)
}
func connectionOrderDigest(items []ConnectionPlacement) string {
	b, _ := json.Marshal(items)
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}

// SaveConnectionOrder changes neither credentials nor connection authority.
// The persisted profile sequence is the order, including the first occurrence
// of each group, so other vault mutations cannot accidentally drop a sidecar.
func (s *Session) SaveConnectionOrder(file string, change ConnectionOrder) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return ErrUnlock
	}
	current := make([]ConnectionPlacement, len(s.profiles))
	byID := make(map[string]Profile, len(s.profiles))
	for i, p := range s.profiles {
		current[i] = ConnectionPlacement{p.ID, p.Group}
		byID[p.ID] = p
	}
	if change.Revision != connectionOrderDigest(current) {
		return errors.New("连接列表已更新，请重试拖动")
	}
	if len(change.Items) != len(s.profiles) || len(change.Items) > MaxProfiles {
		return errors.New("连接排序必须包含所有连接")
	}
	next := make([]Profile, 0, len(change.Items))
	seen := make(map[string]bool, len(change.Items))
	for _, item := range change.Items {
		p, ok := byID[item.ID]
		if !ok || seen[item.ID] {
			return errors.New("连接排序包含无效或重复的连接")
		}
		if len(item.Group) > 128 || strings.ContainsAny(item.Group, "\x00\r\n") {
			return errors.New("连接分组无效")
		}
		seen[item.ID] = true
		p.Group = item.Group
		next = append(next, p)
	}
	copy := &Session{header: s.header, key: s.key, routes: s.routes, profiles: next, snippets: s.snippets, notes: s.notes, auditHead: s.auditHead}
	if err := Save(file, copy, false); err != nil {
		return err
	}
	s.profiles = next
	return nil
}
