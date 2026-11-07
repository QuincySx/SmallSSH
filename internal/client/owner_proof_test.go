package client_test

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"strings"
	"testing"
	"time"

	"sshclient/internal/broker"
	"sshclient/internal/client"
	"sshclient/internal/connectcfg"
	"sshclient/internal/localipc"
	"sshclient/internal/ownercap"
	"sshclient/internal/testserver"
	"sshclient/internal/vault"
)

var (
	_ client.OwnerProof = (*vault.Session)(nil)
	_ client.OwnerProof = (*ownercap.Signer)(nil)
)

var ownerProofTestPass = []byte("b04b1 synthetic owner proof password")

type proofOnly struct {
	sign func([]byte) ([]byte, error)
}

func (p proofOnly) Proof(payload []byte) ([]byte, error) {
	return p.sign(payload)
}

func ownerProofFixture(t *testing.T) (*broker.Server, *testserver.Server, string, vault.Profile) {
	t.Helper()
	fixture := testserver.New(t)
	dir, err := os.MkdirTemp("", "b04b1-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(dir) })
	if err = os.Chmod(dir, 0700); err != nil {
		t.Fatal(err)
	}
	if err = localipc.Prepare(dir); err != nil {
		t.Fatal(err)
	}
	profile := fixture.Profile()
	profile.Name = "proof fixture"
	profile.Network.Proxy = connectcfg.Proxy{
		Mode:     "socks5",
		Address:  "127.0.0.1:1080",
		User:     "synthetic-proxy-user",
		Password: "b04b1 synthetic proxy password",
	}
	owner, err := vault.New(ownerProofTestPass)
	if err != nil {
		t.Fatal(err)
	}
	if err = owner.Put(profile); err != nil {
		owner.Close()
		t.Fatal(err)
	}
	if err = vault.Save(broker.VaultPath(dir), owner, true); err != nil {
		owner.Close()
		t.Fatal(err)
	}
	owner.Close()
	server, err := broker.Open(dir, ownerProofTestPass)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(server.Close)
	return server, fixture, dir, profile
}

func storedOwnerProofProfile(t *testing.T, dir, id string) vault.Profile {
	t.Helper()
	owner, err := vault.Read(broker.VaultPath(dir), ownerProofTestPass)
	if err != nil {
		t.Fatal(err)
	}
	defer owner.Close()
	profile, err := owner.Profile(id)
	if err != nil {
		t.Fatal(err)
	}
	return profile
}

func TestProofOnlySourceCanQueryAndSave(t *testing.T) {
	server, _, dir, profile := ownerProofFixture(t)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	proof := proofOnly{sign: server.Vault.Proof}
	pin, err := localipc.ReadBrokerTrust(dir)
	if err != nil {
		t.Fatal(err)
	}
	cli := client.Client{Dir: dir, BrokerKey: pin, Owner: proof}

	hosts, err := cli.Call(ctx, broker.Message{Action: "hosts"})
	if err != nil {
		t.Fatal(err)
	}
	if hosts.Redacted || len(hosts.Connections) != 1 || hosts.Connections[0].Name != profile.Name {
		t.Fatalf("proof-only owner query was not treated as an owner: %+v", hosts)
	}

	change := vault.ProfileChange{Profile: profile, KeepCredential: true, KeepProxyCredential: true}
	change.Profile.Name = "proof-only metadata update"
	change.Profile.Password = ""
	change.Profile.Network.Proxy.Password = ""
	if err = cli.SaveConnection(ctx, change, proof); err != nil {
		t.Fatal(err)
	}
	stored := storedOwnerProofProfile(t, dir, profile.ID)
	if stored.Name != change.Profile.Name || stored.Password != profile.Password || stored.Network.Proxy.Password != profile.Network.Proxy.Password {
		t.Fatal("proof-only save did not preserve the existing credentials")
	}
}

func TestNilOwnerProofSourcesRemainAnonymous(t *testing.T) {
	_, fixture, dir, _ := ownerProofFixture(t)
	var nilSession *vault.Session
	var nilSigner *ownercap.Signer
	tests := []struct {
		name  string
		owner client.OwnerProof
	}{
		{name: "nil interface"},
		{name: "typed nil session", owner: nilSession},
		{name: "typed nil signer", owner: nilSigner},
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if client.HasOwnerProof(tc.owner) {
				t.Fatal("nil proof source reported present")
			}
			cli := client.Client{Dir: dir, Owner: tc.owner}
			hosts, err := cli.Call(ctx, broker.Message{Action: "hosts"})
			if err != nil {
				t.Fatal(err)
			}
			if !hosts.Redacted || len(hosts.Connections) != 1 {
				t.Fatalf("nil proof source received owner data: %+v", hosts)
			}
			if hosts.Connections[0].Host != "" || hosts.Connections[0].Port != 0 || hosts.Connections[0].User != "" {
				t.Fatal("anonymous host discovery exposed reachability")
			}
			change := vault.ProfileChange{Profile: fixture.Profile()}
			if err = cli.SaveConnection(ctx, change, tc.owner); err == nil {
				t.Fatal("nil proof source authorized a save")
			}
		})
	}
}

func TestNilAndClosedOwnerProofFail(t *testing.T) {
	var session *vault.Session
	if _, err := session.Proof(nil); err == nil {
		t.Fatal("typed nil session produced a proof")
	}
	var signer *ownercap.Signer
	if _, err := signer.Proof(nil); err == nil {
		t.Fatal("typed nil signer produced a proof")
	}
	closedSigner, err := ownercap.NewSigner()
	if err != nil {
		t.Fatal(err)
	}
	if !client.HasOwnerProof(closedSigner) {
		t.Fatal("non-nil signer was rejected before close")
	}
	closedSigner.Close()
	if _, err = closedSigner.Proof(nil); err == nil {
		t.Fatal("closed signer produced a proof")
	}
	closed, err := vault.New(ownerProofTestPass)
	if err != nil {
		t.Fatal(err)
	}
	closed.Close()
	if _, err = closed.Proof(nil); err == nil {
		t.Fatal("closed session produced a proof")
	}
	if client.HasOwnerProof(session) {
		t.Fatal("typed nil session reported present")
	}
}

func TestSaveConnectionKeepsCredentialsAndRejectsInvalidInput(t *testing.T) {
	server, fixture, dir, profile := ownerProofFixture(t)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	proof := proofOnly{sign: server.Vault.Proof}
	pin, err := localipc.ReadBrokerTrust(dir)
	if err != nil {
		t.Fatal(err)
	}
	cli := client.Client{Dir: dir, BrokerKey: pin, Owner: proof}

	passwordChange := vault.ProfileChange{Profile: profile, KeepCredential: true, KeepProxyCredential: true}
	passwordChange.Profile.Name = "password metadata update"
	passwordChange.Profile.Password = ""
	passwordChange.Profile.Network.Proxy.Password = ""
	if err := cli.SaveConnection(ctx, passwordChange, proof); err != nil {
		t.Fatal(err)
	}
	stored := storedOwnerProofProfile(t, dir, profile.ID)
	if stored.Password != profile.Password || stored.Network.Proxy.Password != profile.Network.Proxy.Password {
		t.Fatal("password edit lost the stored password or proxy password")
	}

	keyChange := vault.ProfileChange{Profile: profile, KeepProxyCredential: true}
	keyChange.Profile.Name = "key metadata update"
	keyChange.Profile.Auth = "key"
	keyChange.Profile.Password = ""
	keyChange.Profile.PrivateKey = fixture.PrivateKey
	keyChange.Profile.KeyPassphrase = ""
	keyChange.Profile.Network.Proxy.Password = ""
	if err := cli.SaveConnection(ctx, keyChange, proof); err != nil {
		t.Fatal(err)
	}
	stored = storedOwnerProofProfile(t, dir, profile.ID)
	if stored.Auth != "key" || stored.PrivateKey != fixture.PrivateKey || stored.Network.Proxy.Password != profile.Network.Proxy.Password {
		t.Fatal("new private key or proxy password was not stored")
	}

	keepKey := vault.ProfileChange{Profile: stored, KeepCredential: true, KeepProxyCredential: true}
	keepKey.Profile.Name = "key retained update"
	keepKey.Profile.PrivateKey = ""
	keepKey.Profile.KeyPassphrase = ""
	keepKey.Profile.Network.Proxy.Password = ""
	if err := cli.SaveConnection(ctx, keepKey, proof); err != nil {
		t.Fatal(err)
	}
	stored = storedOwnerProofProfile(t, dir, profile.ID)
	if stored.Name != keepKey.Profile.Name || stored.PrivateKey != fixture.PrivateKey || stored.Network.Proxy.Password != profile.Network.Proxy.Password {
		t.Fatal("metadata edit lost the stored private key or proxy password")
	}

	encrypted, err := os.ReadFile(broker.VaultPath(dir))
	if err != nil {
		t.Fatal(err)
	}
	for _, secret := range []string{profile.Password, fixture.PrivateKey, profile.Network.Proxy.Password} {
		if bytes.Contains(encrypted, []byte(secret)) {
			t.Fatal("credential was written without encryption")
		}
	}
	reopened, err := vault.Read(broker.VaultPath(dir), ownerProofTestPass)
	if err != nil {
		t.Fatal(err)
	}
	reopenedProfile, err := reopened.Profile(profile.ID)
	reopened.Close()
	if err != nil || reopenedProfile.PrivateKey != fixture.PrivateKey || reopenedProfile.Network.Proxy.Password != profile.Network.Proxy.Password {
		t.Fatal("encrypted credentials did not survive reopen", err)
	}

	hosts, err := cli.Call(ctx, broker.Message{Action: "hosts"})
	if err != nil {
		t.Fatal(err)
	}
	encoded, err := json.Marshal(hosts)
	if err != nil {
		t.Fatal(err)
	}
	for _, secret := range []string{profile.Password, fixture.PrivateKey, profile.Network.Proxy.Password} {
		if bytes.Contains(encoded, []byte(secret)) {
			t.Fatal("owner query returned credential material")
		}
	}

	badTarget := vault.ProfileChange{Profile: stored, KeepCredential: true}
	badTarget.Profile.Host = "invalid host"
	if err = cli.SaveConnection(ctx, badTarget, proof); err == nil {
		t.Fatal("invalid public target was accepted")
	}
	const invalidKey = "B04B1 INVALID PRIVATE KEY MUST NOT LEAK"
	badKey := vault.ProfileChange{Profile: stored}
	badKey.Profile.Auth = "key"
	badKey.Profile.PrivateKey = invalidKey
	badKey.Profile.KeyPassphrase = "B04B1 INVALID PASSPHRASE MUST NOT LEAK"
	if err = cli.SaveConnection(ctx, badKey, proof); err == nil || strings.Contains(err.Error(), invalidKey) || strings.Contains(err.Error(), badKey.Profile.KeyPassphrase) {
		t.Fatal("invalid new private key was accepted or leaked", err)
	}
}
