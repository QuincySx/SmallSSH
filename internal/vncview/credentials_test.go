package vncview

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"testing"
)

func TestVNCCredentialsPrivateOriginAndBoundSave(t *testing.T) {
	s, err := StartDirect(context.Background(), "192.0.2.10:5900", "fixture", false)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	value := Credentials{Username: "fixture", Password: "synthetic-secret", Remember: true}
	var saves int
	s.ConfigureCredentials(value, func(_ context.Context, got Credentials) error {
		if got != value {
			t.Error("wrong credentials")
		}
		saves++
		return nil
	})
	for _, origin := range []string{"", "https://untrusted.invalid", s.origin} {
		body, _ := json.Marshal(value)
		r, _ := http.NewRequest(http.MethodPost, s.URL()+"credentials", bytes.NewReader(body))
		r.Header.Set("Content-Type", "application/json")
		r.Header.Set("Origin", origin)
		response, err := http.DefaultClient.Do(r)
		if err != nil {
			t.Fatal(err)
		}
		response.Body.Close()
		if origin == s.origin && response.StatusCode != http.StatusNoContent || origin != s.origin && response.StatusCode < 400 {
			t.Fatal("origin isolation failed", response.StatusCode)
		}
	}
	if saves != 1 {
		t.Fatal("save callback escaped fixed origin")
	}
	response, err := http.Get(s.URL() + "config.json")
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	var config struct{ Credentials Credentials }
	if err := json.NewDecoder(response.Body).Decode(&config); err != nil {
		t.Fatal(err)
	}
	if config.Credentials != value || response.Header.Get("Cache-Control") != "no-store" {
		t.Fatal("saved credentials unavailable or cacheable")
	}
	s.Close()
	if s.currentCredentials() != (Credentials{}) {
		t.Fatal("closed viewer retained credentials")
	}
}
