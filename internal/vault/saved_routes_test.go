package vault

import (
	"bytes"
	"encoding/json"
	"path/filepath"
	"testing"

	"sshclient/internal/connectcfg"
	"sshclient/internal/snippet"
)

func TestSavedRouteReferenceUpdatesEncryptsAndSurvivesOtherWrites(t *testing.T) {
	s, err := New(testPass)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	file := filepath.Join(t.TempDir(), "vault")
	r := SavedRoute{ID: "proxy", Name: "Shared proxy", Proxy: connectcfg.Proxy{Mode: "socks5", Address: "127.0.0.1:1080", User: "fixture", Password: "synthetic shared route credential"}}
	if err = s.SaveRoute(file, r, false, false); err != nil {
		t.Fatal(err)
	}
	p := profile()
	p.Network = connectcfg.Network{RouteID: r.ID, KeepaliveSeconds: 30}
	if err = s.SaveProfile(file, p); err != nil {
		t.Fatal(err)
	}
	before, err := s.ResolveRoute(p)
	if err != nil {
		t.Fatal(err)
	}
	r.Proxy.Address = "127.0.0.1:1081"
	r.Proxy.Password = ""
	if err = s.SaveRoute(file, r, true, false); err != nil {
		t.Fatal(err)
	}
	after, err := s.ResolveRoute(p)
	if err != nil || after[0].Network.Proxy.Address != r.Proxy.Address || after[0].Network.Proxy.Password != "synthetic shared route credential" || after[0].Network.KeepaliveSeconds != 30 {
		t.Fatal("route update not resolved", err)
	}
	if before[0].Network.Proxy.Address != "127.0.0.1:1080" {
		t.Fatal("existing snapshot mutated")
	}
	if err = s.SaveSnippet(file, snippet.Command{ID: "cmd", Name: "Fixture", Body: "echo fixture"}, false); err != nil {
		t.Fatal(err)
	}
	if err = s.SaveServerNote(file, p.ID, ServerNote{Manual: "fixture note"}); err != nil {
		t.Fatal(err)
	}
	if err = s.SaveDetails(file, s.Summaries()[0]); err != nil {
		t.Fatal(err)
	}
	encoded, _ := json.Marshal(s.Routes())
	sealed, err := s.Seal()
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(encoded, []byte("synthetic shared route credential")) || bytes.Contains(sealed, []byte("synthetic shared route credential")) {
		t.Fatal("route credential leaked")
	}
	loaded, err := Read(file, testPass)
	if err != nil {
		t.Fatal(err)
	}
	defer loaded.Close()
	if len(loaded.Routes()) != 1 || !loaded.Routes()[0].HasPassword || loaded.Routes()[0].References != 1 {
		t.Fatal("routes lost after unrelated encrypted writes")
	}
	resolved, err := loaded.ResolveRoute(p)
	if err != nil || resolved[0].Network.Proxy.Password != after[0].Network.Proxy.Password {
		t.Fatal("credential did not round trip", err)
	}
	if err = loaded.SaveRoute(file, SavedRoute{ID: r.ID}, false, true); err == nil {
		t.Fatal("deleted referenced route")
	}
	if err = loaded.DeleteProfile(file, p.ID); err != nil {
		t.Fatal(err)
	}
	if err = loaded.SaveRoute(file, SavedRoute{ID: r.ID}, false, true); err != nil {
		t.Fatal(err)
	}
}

func TestSavedRoutesRejectMissingAmbiguousAndCyclicReferences(t *testing.T) {
	s, err := New(testPass)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	file := filepath.Join(t.TempDir(), "vault")
	p := profile()
	p.Network.RouteID = "missing"
	if _, err = s.ResolveRoute(p); err == nil {
		t.Fatal("missing route silently dialed direct")
	}
	if err = s.SaveProfile(file, p); err == nil {
		t.Fatal("saved missing route")
	}
	p.Network = connectcfg.Network{}
	if err = s.Put(p); err != nil {
		t.Fatal(err)
	}
	r := SavedRoute{ID: "relay-route", Name: "Shared relay", Relay: p.ID}
	if err = s.SaveRoute(file, r, false, false); err != nil {
		t.Fatal(err)
	}
	p.Network.RouteID = r.ID
	if err = s.SaveProfile(file, p); err == nil {
		t.Fatal("saved route through itself")
	}
	if err = s.DeleteProfile(file, p.ID); err == nil {
		t.Fatal("deleted route's relay")
	}
	p.ID = "other"
	p.Network.Proxy = connectcfg.Proxy{Mode: "http", Address: "127.0.0.1:8080"}
	if err = s.SaveProfile(file, p); err == nil {
		t.Fatal("saved ambiguous route and inline proxy")
	}
	p.Network.Proxy = connectcfg.Proxy{}
	if err = s.SaveProfile(file, p); err != nil {
		t.Fatal(err)
	}
	r.Relay = p.ID
	if err = s.SaveRoute(file, r, false, false); err == nil {
		t.Fatal("route edit introduced cycle")
	}
	if err = s.SaveRoute(filepath.Join(file, "unwritable"), SavedRoute{ID: "new", Name: "New", Relay: "other"}, false, false); err == nil {
		t.Fatal("expected failed write")
	}
	if len(s.Routes()) != 1 {
		t.Fatal("failed write published route")
	}
}
