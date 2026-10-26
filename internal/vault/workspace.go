package vault

import (
	"errors"
	"strings"
)

// Snippets share the encrypted payload, since saved commands may contain secrets.
// They are available to the owner desktop, never returned by the public AI RPC.
type Snippet struct {
	ID    string `json:"id"`
	Name  string `json:"name"`
	Group string `json:"group"`
	Body  string `json:"body"`
}

func (s *Session) Snippets() []Snippet {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return nil
	}
	return append([]Snippet(nil), s.snippets...)
}

func (s *Session) SaveSnippet(file string, value Snippet, remove bool) error {
	if value.ID == "" || len(value.ID) > 128 {
		return errors.New("命令 ID 无效")
	}
	if !remove && (strings.TrimSpace(value.Name) == "" || strings.TrimSpace(value.Body) == "" || len(value.Name) > 256 || len(value.Group) > 128 || len(value.Body) > 32768 || strings.ContainsAny(value.Body, "\x00\x1b")) {
		return errors.New("请填写命令名称和内容；内容最多 32 KiB，不允许控制序列")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return ErrUnlock
	}
	values := make([]Snippet, 0, len(s.snippets)+1)
	found := false
	for _, v := range s.snippets {
		if v.ID == value.ID {
			found = true
			if !remove {
				values = append(values, value)
			}
		} else {
			values = append(values, v)
		}
	}
	if !found && !remove {
		if len(values) >= 1024 {
			return errors.New("常用命令数量超过上限")
		}
		values = append(values, value)
	}
	copy := &Session{header: s.header, key: s.key, profiles: s.profiles, snippets: values, notes: s.notes}
	if e := Save(file, copy, false); e != nil {
		return e
	}
	s.snippets = values
	return nil
}

func (s *Session) DeleteProfile(file, id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return ErrUnlock
	}
	profiles := make([]Profile, 0, len(s.profiles))
	found := false
	for _, p := range s.profiles {
		if p.ID == id {
			found = true
		} else {
			profiles = append(profiles, p)
		}
	}
	if !found {
		return errors.New("连接不存在")
	}
	notes := cloneNotes(s.notes)
	delete(notes, id)
	copy := &Session{header: s.header, key: s.key, profiles: profiles, snippets: s.snippets, notes: notes}
	if e := Save(file, copy, false); e != nil {
		return e
	}
	s.profiles = profiles
	s.notes = notes
	return nil
}
