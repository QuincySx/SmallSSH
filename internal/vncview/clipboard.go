package vncview

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"unicode/utf8"
)

// ConfigureClipboard binds explicit clipboard actions to the native host.
// Browser clipboard APIs are never used. No automatic OS synchronization occurs.
func (s *Session) ConfigureClipboard(access func(context.Context, bool, string) (string, error)) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.closed {
		s.clipboard = access
	}
}

func clipboardTextValid(text string) bool {
	return len(text) <= 64<<10 && utf8.ValidString(text) && !strings.ContainsRune(text, 0)
}

func (s *Session) handleClipboard(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost || r.Header.Get("Origin") != s.origin || r.Header.Get("Content-Type") != "application/json" {
		http.Error(w, "origin or method denied", http.StatusForbidden)
		return
	}
	var value struct {
		Action string `json:"action"`
		Text   string `json:"text"`
	}
	decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, 400<<10))
	decoder.DisallowUnknownFields()
	if decoder.Decode(&value) != nil || decoder.Decode(new(any)) != io.EOF || (value.Action != "read" && value.Action != "write") || !clipboardTextValid(value.Text) || (value.Action == "read" && value.Text != "") {
		http.Error(w, "invalid clipboard request", http.StatusBadRequest)
		return
	}
	s.mu.Lock()
	access, closed := s.clipboard, s.closed
	s.mu.Unlock()
	if closed || s.ctx.Err() != nil || access == nil {
		http.Error(w, "viewer unavailable", http.StatusGone)
		return
	}
	ctx, cancel := context.WithCancel(r.Context())
	stop := context.AfterFunc(s.ctx, cancel)
	defer stop()
	defer cancel()
	text, err := access(ctx, value.Action == "write", value.Text)
	if err != nil {
		http.Error(w, "clipboard unavailable", http.StatusServiceUnavailable)
		return
	}
	if !clipboardTextValid(text) {
		http.Error(w, "invalid clipboard text", http.StatusUnprocessableEntity)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(struct {
		Text string `json:"text"`
	}{text})
}
