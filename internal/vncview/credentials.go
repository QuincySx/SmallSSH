package vncview

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"unicode/utf8"
)

type Credentials struct {
	Username string `json:"username,omitempty"`
	Password string `json:"password,omitempty"`
	Remember bool   `json:"remember"`
}

// ConfigureCredentials binds one native-chosen destination to owner-only vault
// access. The private page never supplies a host ID, route, or vault authority.
func (s *Session) ConfigureCredentials(value Credentials, save func(context.Context, Credentials) error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.closed {
		s.credentials, s.saveCredentials = value, save
	}
}

func (s *Session) currentCredentials() Credentials {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.credentials
}

func (s *Session) storeCredentials(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost || r.Header.Get("Origin") != s.origin || r.Header.Get("Content-Type") != "application/json" {
		http.Error(w, "origin or method denied", http.StatusForbidden)
		return
	}
	var value Credentials
	decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, 16<<10))
	decoder.DisallowUnknownFields()
	if decoder.Decode(&value) != nil || decoder.Decode(new(any)) != io.EOF || len(value.Username) > 2048 || len(value.Password) > 2048 || !utf8.ValidString(value.Username+value.Password) || strings.ContainsRune(value.Username+value.Password, 0) {
		http.Error(w, "invalid credentials", http.StatusBadRequest)
		return
	}
	s.mu.Lock()
	save, closed := s.saveCredentials, s.closed
	s.mu.Unlock()
	if closed || s.ctx.Err() != nil || save == nil {
		http.Error(w, "viewer unavailable", http.StatusGone)
		return
	}
	if err := save(r.Context(), value); err != nil {
		http.Error(w, "credentials not saved", http.StatusServiceUnavailable)
		return
	}
	if !value.Remember {
		value.Username, value.Password = "", ""
	}
	s.mu.Lock()
	if !s.closed {
		s.credentials = value
	}
	s.mu.Unlock()
	w.WriteHeader(http.StatusNoContent)
}
