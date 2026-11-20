package vault

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"

	"sshclient/internal/connectcfg"
)

func TestProfileChangesPreserveDependentRelayRoutes(t *testing.T) {
	for _, save := range []bool{false, true} {
		name := "Put"
		if save {
			name = "SaveProfile"
		}
		t.Run(name, func(t *testing.T) {
			s, err := New(testPass)
			if err != nil {
				t.Fatal(err)
			}
			defer s.Close()
			for _, value := range []struct{ id, relay string }{{"e", ""}, {"d", ""}, {"c", "d"}, {"b", "c"}, {"a", "b"}} {
				p := profile()
				p.ID, p.Network.Relay = value.id, value.relay
				if err := s.Put(p); err != nil {
					t.Fatal(err)
				}
			}
			file := filepath.Join(t.TempDir(), "vault.json")
			if err := Save(file, s, true); err != nil {
				t.Fatal(err)
			}
			before, err := os.ReadFile(file)
			if err != nil {
				t.Fatal(err)
			}
			p, err := s.Profile("d")
			if err != nil {
				t.Fatal(err)
			}
			p.Network.Relay = "e"
			p.Password = "unsaved replacement"
			if save {
				err = s.SaveProfile(file, p)
			} else {
				err = s.Put(p)
			}
			if err == nil {
				t.Fatal("accepted a relay edit that exceeds the hop limit only for an indirect dependent")
			}
			stored, err := s.Profile("d")
			if err != nil || stored.Network.Relay != "" || stored.Password != profile().Password {
				t.Fatal("rejected edit changed in-memory profile", err)
			}
			a, err := s.Profile("a")
			if err != nil {
				t.Fatal(err)
			}
			if route, err := s.ResolveRoute(a); err != nil || len(route) != 4 {
				t.Fatal("rejected edit broke the existing route", err)
			}
			after, err := os.ReadFile(file)
			if err != nil || !bytes.Equal(before, after) {
				t.Fatal("rejected edit changed the encrypted file", err)
			}
			// A metadata-only update must remain possible with the same chain.
			stored.Name = "renamed relay"
			if save {
				err = s.SaveProfile(file, stored)
			} else {
				err = s.Put(stored)
			}
			if err != nil {
				t.Fatal("valid update rejected", err)
			}
		})
	}
}

func TestSavedRouteChangesValidateIndirectDependents(t *testing.T) {
	s, err := New(testPass)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	file := filepath.Join(t.TempDir(), "vault.json")
	for _, value := range []struct{ id, relay string }{{"e", ""}, {"a", "e"}} {
		p := profile()
		p.ID, p.Network.Relay = value.id, value.relay
		if err := s.Put(p); err != nil {
			t.Fatal(err)
		}
	}
	r := SavedRoute{ID: "shared", Name: "Shared relay", Relay: "e"}
	if err := s.SaveRoute(file, r, false, false); err != nil {
		t.Fatal(err)
	}
	for _, value := range []struct{ id, relay string }{{"d", ""}, {"c", "d"}, {"b", "c"}} {
		p := profile()
		p.ID, p.Network.Relay = value.id, value.relay
		if p.ID == "d" {
			p.Network = connectcfg.Network{RouteID: r.ID}
		}
		if err := s.SaveProfile(file, p); err != nil {
			t.Fatal(err)
		}
	}
	before, err := os.ReadFile(file)
	if err != nil {
		t.Fatal(err)
	}
	r.Relay = "a"
	if err := s.SaveRoute(file, r, false, false); err == nil {
		t.Fatal("accepted shared route change that breaks an indirect dependent")
	}
	if routes := s.Routes(); len(routes) != 1 || routes[0].Relay != "e" {
		t.Fatal("rejected edit changed the in-memory route")
	}
	after, err := os.ReadFile(file)
	if err != nil || !bytes.Equal(before, after) {
		t.Fatal("rejected edit changed the encrypted file", err)
	}
	b, err := s.Profile("b")
	if err != nil {
		t.Fatal(err)
	}
	if route, err := s.ResolveRoute(b); err != nil || len(route) != 4 {
		t.Fatal("rejected edit broke the existing route", err)
	}
}

func TestDeleteProfilePreservesInlineRelayReferences(t *testing.T) {
	s, err := New(testPass)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	file := filepath.Join(t.TempDir(), "vault.json")
	relay := profile()
	relay.ID = "relay"
	if err := s.Put(relay); err != nil {
		t.Fatal(err)
	}
	target := profile()
	target.ID, target.Network.Relay = "target", relay.ID
	if err := s.SaveProfile(file, target); err != nil {
		t.Fatal(err)
	}
	before, err := os.ReadFile(file)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.DeleteProfile(file, relay.ID); err == nil {
		t.Fatal("deleted a relay still used by an inline connection")
	}
	if _, err := s.Profile(relay.ID); err != nil {
		t.Fatal("rejected deletion changed memory", err)
	}
	after, err := os.ReadFile(file)
	if err != nil || !bytes.Equal(before, after) {
		t.Fatal("rejected deletion changed the encrypted file", err)
	}
	if err := s.DeleteProfile(file, target.ID); err != nil {
		t.Fatal("could not delete dependent connection", err)
	}
	if err := s.DeleteProfile(file, relay.ID); err != nil {
		t.Fatal("could not delete unreferenced relay", err)
	}
}

func TestLegacyDanglingRelaysCanBeRepairedOneAtATime(t *testing.T) {
	for _, save := range []bool{false, true} {
		name := "Put"
		if save {
			name = "SaveProfile"
		}
		t.Run(name, func(t *testing.T) {
			s, err := New(testPass)
			if err != nil {
				t.Fatal(err)
			}
			defer s.Close()
			// The former deletion bug could leave several independent dangling
			// relay references in an otherwise readable encrypted vault.
			for _, value := range []struct{ id, relay string }{{"t1", "r1"}, {"t2", "r2"}} {
				p := profile()
				p.ID, p.Network.Relay = value.id, value.relay
				s.profiles = append(s.profiles, p)
			}
			file := filepath.Join(t.TempDir(), "vault.json")
			if err := Save(file, s, true); err != nil {
				t.Fatal(err)
			}
			change := func(p Profile) error {
				if save {
					return s.SaveProfile(file, p)
				}
				return s.Put(p)
			}
			invalid, err := s.Profile("t1")
			if err != nil {
				t.Fatal(err)
			}
			invalid.Network.Relay = "another-missing-relay"
			if err := change(invalid); err == nil {
				t.Fatal("accepted an invalid edit merely because its previous route was invalid")
			}
			for i, id := range []string{"r1", "r2"} {
				p := profile()
				p.ID = id
				if err := change(p); err != nil {
					t.Fatal("unrelated dangling reference blocked incremental relay repair", err)
				}
				target, err := s.Profile([]string{"t1", "t2"}[i])
				if err != nil {
					t.Fatal(err)
				}
				if route, err := s.ResolveRoute(target); err != nil || len(route) != 2 {
					t.Fatal("repair did not restore dependent route", err)
				}
			}
		})
	}
}

func TestLegacyDanglingSavedRoutesCanBeRepairedOneAtATime(t *testing.T) {
	s, err := New(testPass)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	relay := profile()
	relay.ID = "relay"
	s.profiles = append(s.profiles, relay)
	for _, id := range []string{"one", "two"} {
		p := profile()
		p.ID, p.Network.RouteID = id, id
		s.profiles = append(s.profiles, p)
		s.routes = append(s.routes, SavedRoute{ID: id, Name: id, Relay: "missing-" + id})
	}
	file := filepath.Join(t.TempDir(), "vault.json")
	if err := Save(file, s, true); err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{"one", "two"} {
		if err := s.SaveRoute(file, SavedRoute{ID: id, Name: id, Relay: relay.ID}, false, false); err != nil {
			t.Fatal("unrelated dangling reference blocked incremental saved-route repair", err)
		}
		target, err := s.Profile(id)
		if err != nil {
			t.Fatal(err)
		}
		if route, err := s.ResolveRoute(target); err != nil || len(route) != 2 {
			t.Fatal("repair did not restore saved route", err)
		}
	}
}
