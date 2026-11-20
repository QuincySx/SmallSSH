package vncview

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestVNCViewerControlsAreOriginBoundLimitedAndCancelled(t *testing.T) {
	s, err := StartDirect(context.Background(), "192.0.2.10:5900", "fixture", false)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	var calls atomic.Int32
	s.ConfigureFullscreen(func(context.Context) error { calls.Add(1); return nil })
	s.ConfigureClipboard(func(_ context.Context, write bool, text string) (string, error) {
		calls.Add(1)
		if write && text != "synthetic" {
			t.Error("unexpected clipboard value")
		}
		return "synthetic", nil
	})
	for _, test := range []struct {
		path, origin, body string
		status             int
	}{
		{"fullscreen", "", "{}", 403}, {"fullscreen", "https://other.invalid", "{}", 404},
		{"fullscreen", s.origin, `{ "window":"other" }`, 400}, {"fullscreen", s.origin, "null", 400},
		{"fullscreen", s.origin, "{} {}", 400}, {"fullscreen", s.origin, "{}", 204},
		{"clipboard", "", `{ "action":"read" }`, 403},
		{"clipboard", s.origin, `{ "action":"read", "text":"other" }`, 400},
		{"clipboard", s.origin, `{ "action":"execute" }`, 400},
		{"clipboard", s.origin, `{ "action":"write", "text":"\u0000" }`, 400},
		{"clipboard", s.origin, `{ "action":"write", "text":"` + strings.Repeat("x", 65537) + `" }`, 400},
		{"clipboard", s.origin, `{ "action":"write", "text":"synthetic" }`, 200},
		{"clipboard", s.origin, `{ "action":"read" }`, 200},
	} {
		r, _ := http.NewRequest(http.MethodPost, s.URL()+test.path, strings.NewReader(test.body))
		r.Header.Set("Origin", test.origin)
		r.Header.Set("Content-Type", "application/json")
		response, err := http.DefaultClient.Do(r)
		if err != nil {
			t.Fatal(err)
		}
		if response.StatusCode != test.status {
			t.Errorf("%s %q: status %d, want %d", test.path, test.body[:min(len(test.body), 80)], response.StatusCode, test.status)
		}
		if response.StatusCode == 200 {
			var result struct{ Text string }
			if err := json.NewDecoder(response.Body).Decode(&result); err != nil || result.Text != "synthetic" || response.Header.Get("Cache-Control") != "no-store" {
				t.Fatal("clipboard response leaked or changed", err)
			}
		}
		response.Body.Close()
	}
	if calls.Load() != 3 {
		t.Fatal("invalid request invoked native control", calls.Load())
	}
	started, cancelled := make(chan struct{}), make(chan struct{})
	s.ConfigureFullscreen(func(ctx context.Context) error { close(started); <-ctx.Done(); close(cancelled); return ctx.Err() })
	requestDone := make(chan struct{})
	go func() {
		defer close(requestDone)
		r, _ := http.NewRequest(http.MethodPost, s.URL()+"fullscreen", strings.NewReader("{}"))
		r.Header.Set("Origin", s.origin)
		r.Header.Set("Content-Type", "application/json")
		response, _ := http.DefaultClient.Do(r)
		if response != nil {
			response.Body.Close()
		}
	}()
	select {
	case <-started:
	case <-time.After(3 * time.Second):
		t.Fatal("control did not start")
	}
	s.Close()
	select {
	case <-cancelled:
	case <-time.After(3 * time.Second):
		t.Fatal("closing viewer retained native operation")
	}
	<-requestDone
}
