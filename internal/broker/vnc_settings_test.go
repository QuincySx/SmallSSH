package broker_test

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"testing"

	"sshclient/internal/broker"
	"sshclient/internal/client"
	"sshclient/internal/protocol"
	"sshclient/internal/vault"
)

func TestVNCSettingsOwnerIsolationEncryptionAndSSHMetadataPreservation(t *testing.T) {
	cli, server, profile := newWorkspaceHarness(t)
	ctx := context.Background()
	value := vault.VNCSettings{Address: "192.0.2.10:5900", Mode: "direct", Username: "fixture", Password: "synthetic-vnc-secret"}
	if err := cli.SaveVNCSettings(ctx, profile.ID, value, cli.Owner); err != nil {
		t.Fatal(err)
	}
	got, err := cli.VNCSettings(ctx, profile.ID, cli.Owner)
	if err != nil || got != value {
		t.Fatal("VNC settings did not round trip", err)
	}
	for _, q := range []protocol.Message{
		{Action: "workspaceRead", Workspace: &protocol.WorkspaceQuery{Kind: "vnc-settings", HostID: profile.ID}},
		{Action: "workspaceWrite", WorkspaceChange: &protocol.WorkspaceChange{Kind: "vnc-settings-save", HostID: profile.ID, VNC: &value}},
	} {
		if _, err := (client.Client{Dir: cli.Dir}).Call(ctx, q); err == nil {
			t.Fatal("untrusted caller accessed VNC secret")
		}
	}
	changed := value
	changed.Password = "tampered"
	q := signedWorkspaceQuery(t, cli, cli.Owner, protocol.Message{Action: "workspaceWrite", WorkspaceChange: &protocol.WorkspaceChange{Kind: "vnc-settings-save", HostID: profile.ID, VNC: &value}})
	q.WorkspaceChange.VNC = &changed
	if _, err := cli.Call(ctx, q); err == nil {
		t.Fatal("owner proof did not bind password")
	}
	if _, err := cli.VNCSettings(ctx, "missing", cli.Owner); err == nil {
		t.Fatal("missing server accepted")
	}
	changed.Address = "bad host"
	if err := cli.SaveVNCSettings(ctx, profile.ID, changed, cli.Owner); err == nil {
		t.Fatal("invalid target accepted")
	}
	profile.Name = "Renamed"
	profile.VNC = vault.VNCSettings{}
	resolved, err := server.Vault.ResolveChange(vault.ProfileChange{Profile: profile, KeepCredential: true})
	if err != nil || resolved.VNC != value {
		t.Fatal("SSH form edit erased VNC settings", err)
	}
	data, err := os.ReadFile(broker.VaultPath(cli.Dir))
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(data, []byte(value.Password)) {
		t.Fatal("VNC password stored in plaintext")
	}
	reopened, err := vault.Open(data, pass)
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	got, err = reopened.VNCSettings(profile.ID)
	if err != nil || got != value {
		t.Fatal("VNC settings not persisted", err)
	}
	public, _ := json.Marshal(server.Vault.Summaries())
	if bytes.Contains(public, []byte(value.Password)) || bytes.Contains(public, []byte("vnc")) {
		t.Fatal("public summaries contain VNC settings")
	}
	if err := cli.SaveVNCSettings(ctx, profile.ID, vault.VNCSettings{}, cli.Owner); err != nil {
		t.Fatal(err)
	}
	got, err = cli.VNCSettings(ctx, profile.ID, cli.Owner)
	if err != nil || got != (vault.VNCSettings{}) {
		t.Fatal("forget did not clear VNC settings", err)
	}
}
