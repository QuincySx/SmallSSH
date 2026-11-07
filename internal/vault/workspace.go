package vault

import (
	"errors"

	"sshclient/internal/snippet"
)

// Snippets share the encrypted payload, since saved commands may contain
// secrets. They are available to the owner desktop, never returned by the
// public AI RPC.
func (s *Session) Snippets() []snippet.Command {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return nil
	}
	values := append([]snippet.Command(nil), s.snippets...)
	for i := range values {
		values[i] = values[i].Clone()
		values[i].History = nil
	}
	return values
}

func (s *Session) SaveSnippet(file string, value snippet.Command, remove bool) error {
	if value.ID == "" || len(value.ID) > 128 {
		return errors.New("命令 ID 无效")
	}
	if !remove {
		value = value.Clone()
		if err := snippet.Validate(value); err != nil {
			return err
		}
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return ErrUnlock
	}
	values := make([]snippet.Command, 0, len(s.snippets)+1)
	found := false
	for _, v := range s.snippets {
		if v.ID == value.ID {
			found = true
			if !remove {
				// History is owner-local state, not editor input. Preserve current
				// records only while this parameter still means the same thing.
				value.History = nil
				for _, history := range v.History {
					if definition := snippet.InputDefinition(value, history.System, history.Parameter); definition != "" && definition == history.Definition {
						value.History = append(value.History, history)
					}
				}
				values = append(values, value)
			}
		} else {
			values = append(values, v)
		}
	}
	if !found && !remove {
		value.History = nil
		if len(values) >= 1024 {
			return errors.New("常用命令数量超过上限")
		}
		values = append(values, value)
	}
	copy := &Session{header: s.header, key: s.key, profiles: s.profiles, snippets: values, notes: s.notes, auditHead: s.auditHead}
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
	copy := &Session{header: s.header, key: s.key, profiles: profiles, snippets: s.snippets, notes: notes, auditHead: s.auditHead}
	if e := Save(file, copy, false); e != nil {
		return e
	}
	s.profiles = profiles
	s.notes = notes
	return nil
}
