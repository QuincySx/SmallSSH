package broker_test

import (
	"context"
	"io"
	"strings"
	"testing"
	"time"

	"sshclient/internal/approval"
	"sshclient/internal/broker"
	"sshclient/internal/client"
	"sshclient/internal/vault"
)

func TestConnectionCreateEditCredentialsAndOwnerBoundConnect(t *testing.T) {
	c, f := setup(t)
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()
	probe, err := c.Call(ctx, broker.Message{Action: "probeHost", Target: f.Profile().Host, Port: f.Profile().Port})
	if err != nil || probe.HostIdentity == nil || probe.HostIdentity.Fingerprint != f.Profile().Fingerprint || f.Auths.Load() != 0 {
		t.Fatal(probe, err)
	}
	save := func(change vault.ProfileChange) {
		t.Helper()
		owner, err := vault.Read(broker.VaultPath(c.Dir), pass)
		if err != nil {
			t.Fatal(err)
		}
		defer owner.Close()
		if err = c.SaveConnection(ctx, change, owner); err != nil {
			t.Fatal(err)
		}
	}
	p := f.Profile()
	p.ID = "saved"
	p.Name = "saved password server"
	save(vault.ProfileChange{Profile: p})
	owner, err := vault.Read(broker.VaultPath(c.Dir), pass)
	if err != nil {
		t.Fatal(err)
	}
	stream, err := c.ConnectWithOwner(ctx, p.ID, owner)
	owner.Close()
	if err != nil {
		t.Fatal(err)
	}
	defer stream.Close()
	if f.Auths.Load() != 1 {
		t.Fatal("connection did not use a single authentication")
	}
	// Creating another profile must not revoke an existing server's grants.
	other := f.Profile()
	other.ID = "other"
	other.Name = "Other server"
	save(vault.ProfileChange{Profile: other})
	conn := &client.TerminalConn{Stream: stream}
	marker := []byte("other-server-create-preserves-session")
	if _, err = conn.Write(marker); err != nil {
		t.Fatal(err)
	}
	reply := make([]byte, len(marker))
	if _, err = io.ReadFull(conn, reply); err != nil || string(reply) != string(marker) {
		t.Fatal("adding another connection disconnected the first", err)
	}
	p.Name = "edited password server"
	p.Password = ""
	save(vault.ProfileChange{Profile: p, KeepCredential: true})
	v, err := vault.Read(broker.VaultPath(c.Dir), pass)
	if err != nil {
		t.Fatal(err)
	}
	stored, err := v.Profile(p.ID)
	v.Close()
	if err != nil || stored.Password != f.Profile().Password || stored.Name != p.Name {
		t.Fatal("edit lost saved credential", err)
	}
	p.Auth = "key"
	p.PrivateKey = f.PrivateKey
	p.KeyPassphrase = ""
	save(vault.ProfileChange{Profile: p})
	owner, err = vault.Read(broker.VaultPath(c.Dir), pass)
	if err != nil {
		t.Fatal(err)
	}
	keyStream, err := c.ConnectWithOwner(ctx, p.ID, owner)
	owner.Close()
	if err != nil {
		t.Fatal("replacement private key did not log in", err)
	}
	keyStream.Close()
	p.PrivateKey = "not a private key"
	owner, err = vault.Read(broker.VaultPath(c.Dir), pass)
	if err != nil {
		t.Fatal(err)
	}
	if err = c.SaveConnection(ctx, vault.ProfileChange{Profile: p}, owner); err == nil {
		t.Fatal("invalid key saved")
	}
	owner.Close()
	// A pre-existing CLI/AI request remains pending after a desktop-owned connect.
	r, _ := request(t, c, approval.Operation{Host: "other", Kind: "exec", Command: "exit7", Client: "AI"})
	owner, err = vault.Read(broker.VaultPath(c.Dir), pass)
	if err != nil {
		t.Fatal(err)
	}
	s, err := c.ConnectWithOwner(ctx, "other", owner)
	owner.Close()
	if err != nil {
		t.Fatal(err)
	}
	s.Close()
	state, err := c.Call(ctx, broker.Message{Action: "status", ID: r.ID})
	if err != nil || state.Request.Status != "pending" {
		t.Fatal("desktop confirmation approved a different request", state, err)
	}
}

func TestConnectionChangeProofIncludesCredentialReplacementChoice(t *testing.T) {
	c, f := setup(t)
	ctx := context.Background()
	owner, err := vault.Read(broker.VaultPath(c.Dir), pass)
	if err != nil {
		t.Fatal(err)
	}
	defer owner.Close()
	change := vault.ProfileChange{Profile: f.Profile(), KeepCredential: true}
	challenge, err := c.Call(ctx, broker.Message{Action: "profileChallenge"})
	if err != nil {
		t.Fatal(err)
	}
	proof, err := broker.ProfileChangeProof(challenge.ID, change, owner.Proof)
	if err != nil {
		t.Fatal(err)
	}
	change.KeepCredential = false
	change.Profile.Password = "different"
	if _, err = c.Call(ctx, broker.Message{Action: "profileChange", ID: challenge.ID, ProfileChange: &change, Proof: proof}); err == nil {
		t.Fatal("credential replacement escaped owner binding")
	}
	change.Profile.PrivateKey = "SECRET MUST NOT LEAK"
	change.Profile.Auth = "key"
	err = c.SaveConnection(ctx, change, owner)
	if err == nil || strings.Contains(err.Error(), "SECRET MUST NOT LEAK") {
		t.Fatal("invalid credential validation leaked material", err)
	}
}

func TestOwnerRequestRequiresExactOneUseProofAndCannotBeSpoofed(t *testing.T) {
	c, _ := setup(t)
	ctx := context.Background()
	op := approval.Operation{Host: "fixture", Kind: "shell", ServerStatus: true, Client: "desktop spoof"}
	forged, err := c.Call(ctx, broker.Message{Action: "request", Operation: op, TTL: 60, Request: &approval.Request{OwnerIssued: true, OwnerSession: true}})
	if err != nil || forged.Request == nil || forged.Request.OwnerIssued || forged.Request.OwnerSession {
		t.Fatal("public request accepted injected scope", err)
	}
	owner, err := vault.Read(broker.VaultPath(c.Dir), pass)
	if err != nil {
		t.Fatal(err)
	}
	defer owner.Close()
	challenge, err := c.Call(ctx, broker.Message{Action: "profileChallenge"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = c.Call(ctx, broker.Message{Action: "ownerRequest", ID: challenge.ID, Operation: op, Proof: []byte("yes")}); err == nil {
		t.Fatal("owner scope accepted without signature")
	}
	challenge, err = c.Call(ctx, broker.Message{Action: "profileChallenge"})
	if err != nil {
		t.Fatal(err)
	}
	proof, err := broker.OwnerRequestProof(challenge.ID, op, owner.Proof)
	if err != nil {
		t.Fatal(err)
	}
	changed := op
	changed.Kind = "exec"
	changed.Command = "id"
	if _, err = c.Call(ctx, broker.Message{Action: "ownerRequest", ID: challenge.ID, Operation: changed, Proof: proof}); err == nil {
		t.Fatal("owner request fields were not bound")
	}
	challenge, err = c.Call(ctx, broker.Message{Action: "profileChallenge"})
	if err != nil {
		t.Fatal(err)
	}
	proof, err = broker.OwnerRequestProof(challenge.ID, op, owner.Proof)
	if err != nil {
		t.Fatal(err)
	}
	result, err := c.Call(ctx, broker.Message{Action: "ownerRequest", ID: challenge.ID, Operation: op, Proof: proof})
	if err != nil || result.Request == nil || !result.Request.OwnerIssued || !result.Request.OwnerSession {
		t.Fatal("signed owner scope missing", err)
	}
	if result.Request.Operation.Endpoint == "" || result.Request.Operation.HostFingerprint == "" {
		t.Fatal("owner endpoint not pinned by broker")
	}
	if _, err = c.Call(ctx, broker.Message{Action: "ownerRequest", ID: challenge.ID, Operation: op, Proof: proof}); err == nil {
		t.Fatal("owner challenge replay accepted")
	}
	status, err := c.Call(ctx, broker.Message{Action: "status", ID: forged.Request.ID})
	if err != nil || status.Request == nil || status.Request.Status != "pending" {
		t.Fatal("unrelated request was approved", err)
	}
}
