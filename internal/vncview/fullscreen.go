package vncview

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
)

// ConfigureFullscreen grants only a toggle of this viewer's native host window.
// The page cannot select other windows, run native commands, or choose a target.
func (s *Session) ConfigureFullscreen(toggle func(context.Context) error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.closed {
		s.fullscreen = toggle
	}
}

func (s *Session) toggleFullscreen(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost || r.Header.Get("Origin") != s.origin || r.Header.Get("Content-Type") != "application/json" {
		http.Error(w, "origin or method denied", http.StatusForbidden)
		return
	}
	var value *struct{}
	decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, 64))
	decoder.DisallowUnknownFields()
	if decoder.Decode(&value) != nil || value == nil || decoder.Decode(new(any)) != io.EOF {
		http.Error(w, "invalid control", http.StatusBadRequest)
		return
	}
	s.mu.Lock()
	toggle, closed := s.fullscreen, s.closed
	s.mu.Unlock()
	if closed || s.ctx.Err() != nil || toggle == nil {
		http.Error(w, "viewer unavailable", http.StatusGone)
		return
	}
	ctx, cancel := context.WithCancel(r.Context())
	stop := context.AfterFunc(s.ctx, cancel)
	defer stop()
	defer cancel()
	if toggle(ctx) != nil {
		http.Error(w, "fullscreen unavailable", http.StatusServiceUnavailable)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
