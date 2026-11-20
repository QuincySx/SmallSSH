package vault

import (
	"bytes"
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"

	"sshclient/internal/connectcfg"
	"sshclient/internal/proxyconfig"
)

func TestImportedRoutesEncryptRedactAndUpdateOnlyFutureConnections(t *testing.T) {
	s, err := New(testPass)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	file := filepath.Join(t.TempDir(), "vault")
	nodes, err := proxyconfig.Import(`anytls://synthetic-first-route-secret@proxy.invalid:443#Synthetic`)
	if err != nil {
		t.Fatal(err)
	}
	n := nodes[0]
	r := SavedRoute{ID: "imported", Name: n.Name, Proxy: connectcfg.Proxy{Mode: n.Kind, Address: n.Address, Config: n.Config, Password: n.Secret}}
	if err := s.SaveRoute(file, r, false, false); err != nil {
		t.Fatal(err)
	}
	p := profile()
	p.Network.RouteID = r.ID
	if err := s.SaveProfile(file, p); err != nil {
		t.Fatal(err)
	}
	before, err := s.ResolveRoute(p)
	if err != nil {
		t.Fatal(err)
	}
	public := s.Routes()[0]
	encoded, _ := json.Marshal(public)
	sealed, err := s.Seal()
	if err != nil || !public.HasPassword || public.Proxy.Password != "" || bytes.Contains(encoded, []byte("synthetic-first-route-secret")) || bytes.Contains(sealed, []byte("synthetic-first-route-secret")) {
		t.Fatal("imported route credential escaped encrypted storage", err)
	}
	r.Name, r.Proxy = "Renamed", public.Proxy
	if err := s.SaveRoute(file, r, true, false); err != nil {
		t.Fatal("redacted edit did not retain secret", err)
	}
	nodes, err = proxyconfig.Import(`anytls://synthetic-second-route-secret@updated.invalid:444`)
	if err != nil {
		t.Fatal(err)
	}
	n = nodes[0]
	r.Proxy = connectcfg.Proxy{Mode: n.Kind, Address: n.Address, Config: n.Config, Password: n.Secret}
	if err := s.SaveRoute(file, r, false, false); err != nil {
		t.Fatal(err)
	}
	after, err := s.ResolveRoute(p)
	if err != nil || after[0].Network.Proxy.Address != "updated.invalid:444" || !strings.Contains(after[0].Network.Proxy.Password, "synthetic-second-route-secret") || !strings.Contains(before[0].Network.Proxy.Password, "synthetic-first-route-secret") {
		t.Fatal("updated route mutated active snapshot or missed new connection", err)
	}
	loaded, err := Read(file, testPass)
	if err != nil {
		t.Fatal(err)
	}
	defer loaded.Close()
	resolved, err := loaded.ResolveRoute(p)
	if err != nil || resolved[0].Network.Proxy != after[0].Network.Proxy {
		t.Fatal("imported route lost configuration on vault round trip", err)
	}
	r.Proxy.Password = ""
	if err := loaded.SaveRoute(file, r, false, false); err == nil {
		t.Fatal("persisted redacted node without keep-credential intent")
	}
}
