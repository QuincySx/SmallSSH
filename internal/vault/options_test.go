package vault

import (
	"bytes"
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"

	"sshclient/internal/connectcfg"
)

func TestConnectionOptionsEncryptRedactAndPreserveIndependentCredentials(t *testing.T) {
	session, e := New(testPass)
	if e != nil {
		t.Fatal(e)
	}
	defer session.Close()
	p := profile()
	p.Terminal = connectcfg.Terminal{Encoding: "gb18030", Backspace: "bs"}
	p.Network = connectcfg.Network{Proxy: connectcfg.Proxy{Mode: "socks5", Address: "127.0.0.1:1080", User: "fixture", Password: "synthetic proxy password"}, KeepaliveSeconds: 30}
	p.Auth = "pkcs11"
	p.Password = ""
	p.Hardware = connectcfg.Hardware{Module: filepath.Join(t.TempDir(), "synthetic-driver"), ModuleSHA256: strings.Repeat("a", 64), TokenSerial: "fixture001", KeyID: "01", Fingerprint: "SHA256:fixture", PIN: "synthetic hardware PIN"}
	if e = session.Put(p); e != nil {
		t.Fatal(e)
	}
	sealed, e := session.Seal()
	if e != nil {
		t.Fatal(e)
	}
	metadata := session.Summaries()[0]
	public, e := json.Marshal(metadata)
	if e != nil {
		t.Fatal(e)
	}
	for _, secret := range []string{p.Network.Proxy.Password, p.Hardware.PIN} {
		if bytes.Contains(sealed, []byte(secret)) || bytes.Contains(public, []byte(secret)) {
			t.Fatal("connection options exposed a credential")
		}
	}
	draft := p
	draft.Name = "renamed"
	draft.Network.Proxy.Password = ""
	draft.Hardware.PIN = ""
	resolved, e := session.ResolveChange(ProfileChange{Profile: draft, KeepCredential: true, KeepProxyCredential: true})
	if e != nil {
		t.Fatal(e)
	}
	if resolved.Hardware.PIN != p.Hardware.PIN || resolved.Network.Proxy.Password != p.Network.Proxy.Password || resolved.Terminal != p.Terminal {
		t.Fatal("independent connection credentials were erased")
	}
	draft.Network.Proxy.User = "different"
	if _, e = session.ResolveChange(ProfileChange{Profile: draft, KeepCredential: true, KeepProxyCredential: true}); e == nil {
		t.Fatal("reused proxy password for a different proxy identity")
	}
}
func TestSSHRelayRoutesRejectCyclesMissingHopsAndAmbiguousProxies(t *testing.T) {
	session, e := New(testPass)
	if e != nil {
		t.Fatal(e)
	}
	defer session.Close()
	relay := profile()
	relay.ID = "relay"
	if e = session.Put(relay); e != nil {
		t.Fatal(e)
	}
	target := profile()
	target.ID = "target"
	target.Network.Relay = relay.ID
	if e = session.Put(target); e != nil {
		t.Fatal(e)
	}
	route, e := session.ResolveRoute(target)
	if e != nil || len(route) != 2 || route[1].ID != relay.ID {
		t.Fatal(route, e)
	}
	relay.Network.Relay = target.ID
	if e = session.Put(relay); e == nil {
		t.Fatal("saved a cyclic SSH route")
	}
	missing := target
	missing.Network.Relay = "missing"
	if e = session.Put(missing); e == nil {
		t.Fatal("saved a dangling relay")
	}
	ambiguous := target
	ambiguous.Network.Proxy = connectcfg.Proxy{Mode: "http", Address: "127.0.0.1:8080"}
	if e = session.Put(ambiguous); e == nil {
		t.Fatal("silently ignored a configured proxy while using a relay")
	}
}
