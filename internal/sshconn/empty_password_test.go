package sshconn_test

import (
	"context"
	"testing"
	"time"

	"sshclient/internal/sshconn"
	"sshclient/internal/testserver"
)

func TestEmptyPasswordAuthenticatesAndStillVerifiesHost(t *testing.T) {
	fixture := testserver.NewWithPassword(t, "")
	p := fixture.Profile()
	if p.Password != "" {
		t.Fatal("fixture profile did not select the empty password")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	client, err := sshconn.Dial(ctx, p)
	if err != nil {
		t.Fatal("empty password authentication failed", err)
	}
	result, err := client.Exec(ctx, "empty-password")
	client.Close()
	if err != nil || result.ExitCode != 0 || result.Stdout != "fixture stdout: empty-password" {
		t.Fatal("empty password exec failed", err, result)
	}
	if fixture.Auths.Load() == 0 {
		t.Fatal("password authentication was not attempted")
	}

	wrong := p
	wrong.Password = "synthetic non-empty password"
	if client, err = sshconn.Dial(ctx, wrong); err == nil {
		client.Close()
		t.Fatal("server accepted a non-empty password for an empty-password account")
	}

	auths := fixture.Auths.Load()
	wrongFingerprint := p
	wrongFingerprint.Fingerprint = "SHA256:wrong"
	if client, err = sshconn.Dial(ctx, wrongFingerprint); err == nil {
		client.Close()
		t.Fatal("empty password bypassed host verification")
	}
	if fixture.Auths.Load() != auths {
		t.Fatal("credentials were sent before host verification")
	}
}
