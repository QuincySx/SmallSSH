package vault

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"time"
	"unicode/utf8"
)

// Owner-only annotations are deliberately absent from connection summaries
// and the public broker protocol. Both fields share the encrypted vault.
type ServerNote struct {
	Manual     string    `json:"manual,omitempty"`
	Discovered string    `json:"discovered,omitempty"`
	Details    string    `json:"details,omitempty"`
	CheckedAt  time.Time `json:"checkedAt,omitempty"`
	Target     string    `json:"target,omitempty"`
}

func NoteTarget(p Summary) string {
	h := sha256.Sum256([]byte(fmt.Sprintf("%s\x00%d\x00%s\x00%s", p.Host, p.Port, p.User, p.Fingerprint)))
	return hex.EncodeToString(h[:])
}

func cloneNotes(values map[string]ServerNote) map[string]ServerNote {
	out := make(map[string]ServerNote, len(values)+1)
	for id, note := range values {
		out[id] = note
	}
	return out
}

func (s *Session) ServerNote(id string) (ServerNote, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return ServerNote{}, ErrUnlock
	}
	for _, p := range s.profiles {
		if p.ID == id {
			return s.notes[id], nil
		}
	}
	return ServerNote{}, errors.New("连接不存在")
}

func (s *Session) SaveServerNote(file, id string, note ServerNote) error {
	if len(note.Manual) > 8192 || len(note.Discovered) > 16384 || len(note.Details) > 16384 || len(note.Target) > 128 ||
		!utf8.ValidString(note.Manual) || !utf8.ValidString(note.Discovered) || !utf8.ValidString(note.Details) || strings.ContainsAny(note.Manual+note.Discovered+note.Details, "\x00\x1b") {
		return errors.New("备注过长或含控制字符；手写备注最多 8 KiB")
	}
	if (note.Discovered != "" || note.Details != "") && (note.Discovered == "" || note.CheckedAt.IsZero() || note.Target == "") {
		return errors.New("自动概览缺少检测时间或目标信息")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return ErrUnlock
	}
	found := false
	for _, p := range s.profiles {
		if p.ID == id {
			found = true
			break
		}
	}
	if !found {
		return errors.New("连接不存在")
	}
	values := cloneNotes(s.notes)
	if note == (ServerNote{}) {
		delete(values, id)
	} else {
		values[id] = note
	}
	copy := &Session{header: s.header, key: s.key, profiles: s.profiles, snippets: s.snippets, notes: values}
	if err := Save(file, copy, false); err != nil {
		return err
	}
	s.notes = values
	return nil
}
